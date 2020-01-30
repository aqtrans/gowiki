package main

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"git.jba.io/go/httputils"
)

// CUSTOM GIT WRAPPERS
// Construct an *exec.Cmd for `git {args}` with a workingDirectory
func (env *wikiEnv) gitCommand(args ...string) *exec.Cmd {
	c := exec.Command(env.cfg.GitPath, args...)
	c.Env = os.Environ()
	c.Env = append(c.Env, `GIT_COMMITTER_NAME="`+env.cfg.GitCommitName+`"`, `GIT_COMMITTER_EMAIL="`+env.cfg.GitCommitEmail+`"`)
	c.Dir = env.cfg.WikiDir
	return c
}

/*
// Execute `git init {directory}` in the current workingDirectory
func gitInit() error {
	//wd, err := os.Getwd()
	//if err != nil {
	//	return err
	//}
	return gitCommand("init").Run()
}

// Execute `git clone [repo]` in the current workingDirectory
func gitClone(repo string) error {
	//wd, err := os.Getwd()
	//if err != nil {
	//	return err
	//}
	o, err := gitCommand("clone", repo, ".").CombinedOutput()
	if err != nil {
		return fmt.Errorf("error during `git clone`: %s\n%s", err.Error(), string(o))
	}
	return nil
}
*/

// Execute `git status -s` in directory
// If there is output, the directory has is dirty
func (env *wikiEnv) gitIsClean() error {
	gitBehind := []byte("Your branch is behind")
	gitAhead := []byte("Your branch is ahead")
	gitDiverged := []byte("have diverged")

	// Check for untracked files first
	u := env.gitCommand("ls-files", "--exclude-standard", "--others")
	uo, err := u.Output()
	if len(uo) != 0 {
		return errors.New(string(uo))
	}

	// Fetch changes from remote
	// Commented out for now; adds a solid second to load times!
	/*
		err = gitCommand("fetch").Run()
		if err != nil {
			return err
		}
	*/

	// Now check the status, minus untracked files
	c := env.gitCommand("status", "-uno")

	o, err := c.Output()
	if err != nil {
		return err
	}

	if bytes.Contains(o, gitBehind) {
		//return gitPull()
		//return errors.New(string(o))
		return errGitBehind
	}

	if bytes.Contains(o, gitAhead) {
		//return gitPush()
		//return errors.New(string(o))
		return errGitAhead
	}

	if bytes.Contains(o, gitDiverged) {
		//return errors.New(string(o))
		return errGitDiverged
	}

	return nil
}

func (env *wikiEnv) gitIsCleanStartup() error {
	gitBehind := []byte("Your branch is behind")
	gitAhead := []byte("Your branch is ahead")
	gitDiverged := []byte("have diverged")

	// Check for untracked files first
	u := env.gitCommand("ls-files", "--exclude-standard", "--others")
	uo, err := u.Output()
	if len(uo) != 0 {
		return errors.New("Untracked files: " + string(uo))
	}

	if env.cfg.RemoteGitRepo != "" {
		// Fetch changes from remote
		err = env.gitCommand("fetch").Run()
		if err != nil {
			return err
		}
	}

	// Now check the status, minus untracked files
	c := env.gitCommand("status", "-uno")

	o, err := c.Output()
	if err != nil {
		return err
	}

	if bytes.Contains(o, gitBehind) {
		httputils.Debugln("gitIsCleanStartup: Pulling git repo...")
		return env.gitPull()
		//return errors.New(string(o))
		//return ErrGitBehind
	}

	if bytes.Contains(o, gitAhead) {
		if env.cfg.PushOnSave {
			httputils.Debugln("gitIsCleanStartup: Pushing git repo...")
			return env.gitPush()
		}
		return nil
		//return errors.New(string(o))
		//return ErrGitAhead
	}

	if bytes.Contains(o, gitDiverged) {
		return errors.New(string(o))
		//return ErrGitDiverged
	}

	return nil
}

// Execute `git add {filepath}` in workingDirectory
func (env *wikiEnv) gitAddFilepath(filepath string) error {
	o, err := env.gitCommand("add", filepath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error during `git add`: %s\n%s", err.Error(), string(o))
	}
	return nil
}

// Execute `git rm {filepath}` in workingDirectory
func (env *wikiEnv) gitRmFilepath(filepath string) error {
	o, err := env.gitCommand("rm", filepath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error during `git rm`: %s\n%s", err.Error(), string(o))
	}
	return nil
}

// Execute `git commit -m {msg}` in workingDirectory
func (env *wikiEnv) gitCommitWithMessage(msg string) error {
	o, err := env.gitCommand("commit", "--author", "'Golang Wiki <golangwiki@jba.io>'", "-m", msg).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error during `git commit`: %s\n%s", err.Error(), string(o))
	}

	return nil
}

// Execute `git commit -m "commit from GoWiki"` in workingDirectory
func (env *wikiEnv) gitCommitEmpty() error {
	o, err := env.gitCommand("commit", "--author", "'Golang Wiki <golangwiki@jba.io>'", "-m", "commit from GoWiki").CombinedOutput()
	if err != nil {
		return fmt.Errorf("error during `git commit`: %s\n%s", err.Error(), string(o))
	}

	return nil
}

// Execute `git push` in workingDirectory
func (env *wikiEnv) gitPush() error {
	o, err := env.gitCommand("push", "-u", "origin", "master").CombinedOutput()
	if err != nil {
		return fmt.Errorf("error during `git push`: %s\n%s", err.Error(), string(o))
	}

	return nil
}

// Execute `git push` in workingDirectory
func (env *wikiEnv) gitPull() error {
	o, err := env.gitCommand("pull").CombinedOutput()
	if err != nil {
		return fmt.Errorf("error during `git pull`: %s\n%s", err.Error(), string(o))
	}

	return nil
}

// File creation time, output to UNIX time
// git log --diff-filter=A --follow --format=%at -1 -- [filename]
func (env *wikiEnv) gitGetCtime(filename string) (int64, error) {
	defer httputils.TimeTrack(time.Now(), "gitGetCtime")
	//var ctime int64
	o, err := env.gitCommand("log", "--diff-filter=A", "--follow", "--format=%at", "-1", "--", filename).Output()
	if err != nil {
		return 0, fmt.Errorf("error during `git log --diff-filter=A --follow --format=at -1 --`: %s\n%s", err.Error(), string(o))
	}
	ostring := strings.TrimSpace(string(o))
	// If output is blank, no point in wasting CPU doing the rest
	if ostring == "" {
		log.Println(filename + " is not checked into Git")
		return 0, errNotInGit
	}
	ctime, err := strconv.ParseInt(ostring, 10, 64)
	if err != nil {
		panic(err)
	}

	return ctime, err
}

// File modification time, output to UNIX time
// git log -1 --format=%at -- [filename]
func (env *wikiEnv) gitGetMtime(filename string) (int64, error) {
	defer httputils.TimeTrack(time.Now(), "gitGetMtime")
	//var mtime int64
	o, err := env.gitCommand("log", "--format=%at", "-1", "--", filename).Output()
	if err != nil {
		return 0, fmt.Errorf("error during `git log -1 --format=at --`: %s\n%s", err.Error(), string(o))
	}
	ostring := strings.TrimSpace(string(o))
	// If output is blank, no point in wasting CPU doing the rest
	if ostring == "" {
		log.Println(filename + " is not checked into Git")
		return 0, nil
	}
	mtime, err := strconv.ParseInt(ostring, 10, 64)
	if err != nil {
		panic(err)
	}

	return mtime, err
}

type commitLog struct {
	Filename string
	Commit   string
	Date     int64
	Message  string
}

// File history
// git log --pretty=format:"commit:%H date:%at message:%s" [filename]
// git log --pretty=format:"%H,%at,%s" [filename]
func (env *wikiEnv) gitGetFileLog(filename string) ([]commitLog, error) {
	o, err := env.gitCommand("log", "--pretty=format:%H,%at,%s", filename).Output()
	if err != nil {
		return nil, fmt.Errorf("error during `git log`: %s\n%s", err.Error(), string(o))
	}
	// split each commit onto it's own line
	logsplit := strings.Split(string(o), "\n")
	// now split each commit-line into it's slice
	// format should be: [sha1],[date],[message]
	var commits []commitLog
	for _, v := range logsplit {
		//var theCommit *commitLog
		var vs = strings.SplitN(v, ",", 3)
		// Convert date to int64
		var mtime, err = strconv.ParseInt(vs[1], 10, 64)
		if err != nil {
			panic(err)
		}
		// Now shortening the SHA1 to 7 digits, supposed to be the default git short sha output
		shortsha := vs[0][0:7]

		// vs[0] = commit, vs[1] = date, vs[2] = message
		theCommit := commitLog{
			Filename: filename,
			Commit:   shortsha,
			Date:     mtime,
			Message:  vs[2],
		}
		commits = append(commits, theCommit)
	}
	return commits, nil
}

// Get file as it existed at specific commit
// git show [commit sha1]:[filename]
func (env *wikiEnv) gitGetFileCommit(filename, commit string) ([]byte, error) {
	// Combine these into one
	fullcommit := commit + ":" + filename
	o, err := env.gitCommand("show", fullcommit).CombinedOutput()
	if err != nil {
		return []byte{}, fmt.Errorf("error during `git show`: %s\n%s", err.Error(), string(o))
	}
	return o, nil
}

// Get diff for entire commit
// git show [commit sha1]
func (env *wikiEnv) gitGetFileCommitDiff(filename, commit string) ([]byte, error) {
	o, err := env.gitCommand("show", commit).CombinedOutput()
	if err != nil {
		return []byte{}, fmt.Errorf("error during `git show`: %s\n%s", err.Error(), string(o))
	}
	return o, nil
}

// File modification time for specific commit, output to UNIX time
// git log -1 --format=%at [commit sha1]
func (env *wikiEnv) gitGetFileCommitMtime(commit string) (int64, error) {
	//var mtime int64
	o, err := env.gitCommand("log", "--format=%at", "-1", commit).Output()
	if err != nil {
		return 0, fmt.Errorf("error during `git log -1 --format=at --`: %s\n%s", err.Error(), string(o))
	}
	ostring := strings.TrimSpace(string(o))
	mtime, err := strconv.ParseInt(ostring, 10, 64)
	if err != nil {
		panic(err)
	}

	return mtime, err
}

// git ls-files
func (env *wikiEnv) gitLs() ([]string, error) {
	o, err := env.gitCommand("ls-files", "-z").Output()
	if err != nil {
		return nil, fmt.Errorf("error during `git ls-files`: %s\n%s", err.Error(), string(o))
	}
	nul := bytes.Replace(o, []byte("\x00"), []byte("\n"), -1)
	// split each commit onto it's own line
	lssplit := strings.Split(string(nul), "\n")
	return lssplit, nil
}

// git ls-tree -r -t HEAD
func (env *wikiEnv) gitLsTree() ([]*gitDirList, error) {
	o, err := env.gitCommand("ls-tree", "-r", "-t", "-z", "HEAD").Output()
	if err != nil {
		return nil, fmt.Errorf("error during `git ls-files`: %s\n%s", err.Error(), string(o))
	}
	if len(o) == 0 {
		return []*gitDirList{}, nil
	}
	nul := bytes.Replace(o, []byte("\x00"), []byte("\n"), -1)
	// split each commit onto it's own line
	ostring := strings.TrimSpace(string(nul))
	lssplit := strings.Split(ostring, "\n")
	var dirList []*gitDirList
	for _, v := range lssplit {
		var vs = strings.SplitN(v, " ", 3)
		var vs2 = strings.FieldsFunc(vs[2], func(c rune) bool { return c == '\t' })
		theGitDirListing := &gitDirList{
			Type:     vs[1],
			Filename: vs2[1],
		}
		dirList = append(dirList, theGitDirListing)
	}
	return dirList, nil
}

// git ls-tree HEAD:[dirname]
func (env *wikiEnv) gitLsTreeDir(dir string) ([]*gitDirList, error) {
	o, err := env.gitCommand("ls-tree", "-z", "HEAD:"+dir).Output()
	if err != nil {
		return nil, fmt.Errorf("error during `git ls-files`: %s\n%s", err.Error(), string(o))
	}
	nul := bytes.Replace(o, []byte("\x00"), []byte("\n"), -1)
	// split each commit onto it's own line
	ostring := strings.TrimSpace(string(nul))
	lssplit := strings.Split(ostring, "\n")
	var dirList []*gitDirList
	for _, v := range lssplit {
		var vs = strings.Fields(v)
		theGitDirListing := &gitDirList{
			Type:     vs[1],
			Filename: vs[3],
		}
		dirList = append(dirList, theGitDirListing)
	}
	return dirList, nil
}

// git log --name-only --pretty=format:"%at %H" -z HEAD
func (env *wikiEnv) gitHistory() ([]string, error) {
	o, err := env.gitCommand("log", "--name-only", "--pretty=format:_END %at %H", "-z", "HEAD").Output()
	if err != nil {
		return nil, fmt.Errorf("error during `git history`: %s\n%s", err.Error(), string(o))
	}

	// Get rid of first _END
	o = bytes.Replace(o, []byte("_END"), []byte(""), 1)
	o = bytes.Replace(o, []byte("\x00"), []byte("\n"), -1)

	// Now remove all _END tags
	b := bytes.SplitAfter(o, []byte("_END"))
	var s []string
	//var s *[]recent
	for _, v := range b {
		v = bytes.Replace(v, []byte("_END"), []byte(""), -1)
		s = append(s, string(v))
	}

	return s, nil
}

// File creation time, output to UNIX time
// git log --diff-filter=A --follow --format=%at -1 -- [filename]
// File modification time, output to UNIX time
// git log -1 --format=%at -- [filename]
func (env *wikiEnv) gitGetTimes(filename string, ctime, mtime chan<- int64) {
	defer httputils.TimeTrack(time.Now(), "gitGetTimes")

	go func() {
		co, err := env.gitCommand("log", "--diff-filter=A", "--follow", "--format=%at", "-1", "--", filename).Output()
		if err != nil {
			log.Println(filename, err)
			ctime <- 0
			return
		}
		costring := strings.TrimSpace(string(co))
		// If output is blank, no point in wasting CPU doing the rest
		if costring == "" {
			log.Println(filename + " is not checked into Git")
			ctime <- 0
			return
		}
		ctimeI, err := strconv.ParseInt(costring, 10, 64)
		if err != nil {
			ctime <- 0
			return
		}
		ctime <- ctimeI
	}()

	go func() {
		mo, err := env.gitCommand("log", "--format=%at", "-1", "--", filename).Output()
		if err != nil {
			log.Println(filename, err)
			mtime <- 0
			return
		}
		mostring := strings.TrimSpace(string(mo))
		// If output is blank, no point in wasting CPU doing the rest
		if mostring == "" {
			log.Println(filename + " is not checked into Git")
			mtime <- 0
			return
		}
		mtimeI, err := strconv.ParseInt(mostring, 10, 64)
		if err != nil {
			mtime <- 0
			return
		}
		mtime <- mtimeI
	}()

}

func (env *wikiEnv) gitIsEmpty() bool {
	// Run git rev-parse HEAD on the repo
	// If it errors out, should mean it's empty
	err := env.gitCommand("rev-parse", "HEAD").Run()
	if err != nil {
		return true
	}
	return false
}

// Search results, via git
// git grep --break 'searchTerm'
func (env *wikiEnv) gitSearch(searchTerm, fileSpec string) []result {
	var results []result
	quotedSearchTerm := `'` + searchTerm + `'`
	cmd := exec.Command("/bin/sh", "-c", env.cfg.GitPath+" grep -i "+quotedSearchTerm+" -- "+fileSpec)
	//o := gitCommand("grep", "omg -- 'index'")
	cmd.Dir = env.cfg.WikiDir
	o, err := cmd.CombinedOutput()
	if err != nil {
		return nil
	}

	// split each result onto it's own line
	logsplit := strings.Split(string(o), "\n")
	// now split each search result line into it's slice
	// format should be: filename:searchresult

	for _, v := range logsplit {
		//var theCommit *commitLog
		var vs = strings.SplitN(v, ":", 2)
		//log.Println(len(vs))

		// vs[0] = commit, vs[1] = date, vs[2] = message
		if len(vs) == 2 {
			theResult := result{
				Name:   vs[0],
				Result: vs[1],
			}
			results = append(results, theResult)
		}
	}
	// Check for matching filenames
	listOfFiles := strings.Fields(fileSpec)
	for _, v := range listOfFiles {
		if strings.Contains(v, searchTerm) {
			cleanV := strings.TrimPrefix(v, "\"")
			cleanV = strings.TrimSuffix(cleanV, "\"")
			theResult := result{
				Name: cleanV,
			}
			results = append(results, theResult)
		}
	}

	return results
}

func (env *wikiEnv) gitIsCleanURLs(token template.HTML) template.HTML {
	switch env.gitIsClean() {
	case errGitAhead:
		return template.HTML(`<form method="post" action="/admin/git/push" id="git_push">` + token + `<i class="fa fa-cloud-upload" aria-hidden="true"></i><button type="submit" class="button">Push git</button></form>`)
	case errGitBehind:
		return template.HTML(`<form method="post" action="/admin/git/pull" id="git_pull">` + token + `<i class="fa fa-cloud-download" aria-hidden="true"></i><button type="submit" class="button">Pull git</button></form>`)
	case errGitDiverged:
		return template.HTML(`<a href="/admin/git"><i class="fa fa-exclamation-triangle" aria-hidden="true"></i>Issue with git:wiki!</a>`)
	default:
		return template.HTML(`Git repo is clean.`)
	}
}
