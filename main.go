package main

// Credits:
// - jQuery-Tags-Input: https://github.com/xoxco/jQuery-Tags-Input
//     - Used for elegant tags UI on editing page
// - YAML frontmatter based on http://godoc.org/j4k.co/fmatter
//     - Used for YAML frontmatter parsing to/from wiki pages
// - bpool-powered template rendering based on https://elithrar.github.io/article/approximating-html-template-inheritance/
//     - Used to catch rendering errors, so there's no half-rendered pages
// - Using a map[string]struct{} for cache.Favs to easily check for uniqueness: http://stackoverflow.com/a/9251352
// - go1.8+ http.Server.Shutdown() support: https://tylerchr.blog/golang-18-whats-coming/

//TODO:
// - wikidata should be periodically pushed to git@jba.io:conf/gowiki-data.git
//    - Unsure how/when to do this, possibly in a go-routine after every commit?
// - WRITE SOME TESTS!!
//   - Mainly testing Admin, Public, and Private/default pages
// - Move some of the wikiHandler logic to viewHandler

// x GUI for Tags - taggle.js should do this for me
// x LDAP integration
// - Buttons
// x Private pages
// - Tests
// x cache gitLs output, based on latest sha1

// YAML frontmatter based on http://godoc.org/j4k.co/fmatter

// Markdown stuff from https://raw.githubusercontent.com/gogits/gogs/master/modules/markdown/markdown.go

import (
	"bufio"
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/http/pprof"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dimfeld/httptreemux"
	"github.com/getsentry/raven-go"
	"github.com/gorilla/csrf"
	"github.com/justinas/alice"
	"github.com/oxtoacart/bpool"
	"github.com/russross/blackfriday"
	//"github.com/microcosm-cc/bluemonday"
	"github.com/spf13/viper"
	gogit "gopkg.in/src-d/go-git.v4"
	"gopkg.in/yaml.v2"
	"jba.io/go/auth"
	"jba.io/go/httputils"
)

type key int

const (
	commonHTMLFlags = 0 |
		blackfriday.HTML_FOOTNOTE_RETURN_LINKS |
		blackfriday.HTML_TOC |
		blackfriday.HTML_NOFOLLOW_LINKS

	commonExtensions = 0 |
		blackfriday.EXTENSION_NO_INTRA_EMPHASIS |
		blackfriday.EXTENSION_TABLES |
		blackfriday.EXTENSION_FENCED_CODE |
		blackfriday.EXTENSION_AUTOLINK |
		blackfriday.EXTENSION_STRIKETHROUGH |
		blackfriday.EXTENSION_AUTO_HEADER_IDS |
		blackfriday.EXTENSION_BACKSLASH_LINE_BREAK |
		blackfriday.EXTENSION_DEFINITION_LISTS |
		blackfriday.EXTENSION_NO_EMPTY_LINE_BEFORE_BLOCK |
		blackfriday.EXTENSION_FOOTNOTES |
		blackfriday.EXTENSION_TITLEBLOCK

	timerKey      key = 0
	wikiNameKey   key = 1
	wikiExistsKey key = 2
	wikiKey       key = 3
	yamlsep           = "---"
	yamlsep2          = "..."

	/*
		commonHTMLFlags2 = 0 |
			bf.FootnoteReturnLinks |
			bf.NofollowLinks

		commonExtensions2 = 0 |
			bf.NoIntraEmphasis |
			bf.Tables |
			bf.FencedCode |
			bf.Autolink |
			bf.Strikethrough |
			bf.AutoHeaderIDs |
			bf.BackslashLineBreak |
			bf.DefinitionLists |
			bf.NoEmptyLineBeforeBlock |
			bf.Footnotes |
			bf.Titleblock |
			bf.TOC
	*/
)

var (
	//authState      *auth.AuthState
	//linkPattern = regexp.MustCompile(`\[\/(?P<Name>[0-9a-zA-Z-_\.\/]+)\]\(\)`)
	//bufpool        *bpool.BufferPool
	//templates      map[string]*template.Template
	gitPath string
	//cache          *wikiCache
	errNotInGit    = errors.New("given file not in Git repo")
	errNoFile      = errors.New("no such file")
	errNoDirIndex  = errors.New("no such directory index")
	errBaseNotDir  = errors.New("cannot create subdirectory of a file")
	errGitDirty    = errors.New("directory is dirty")
	errBadPath     = errors.New("given path is invalid")
	errGitAhead    = errors.New("wiki git repo is ahead; Need to push")
	errGitBehind   = errors.New("wiki git repo is behind; Need to pull")
	errGitDiverged = errors.New("wiki git repo has diverged; Need to intervene manually")
)

type renderer struct {
	*blackfriday.Html
}

//Base struct, page ; has to be wrapped in a data {} strut for consistency reasons
type page struct {
	SiteName  string
	Favs      map[string]struct{}
	UserInfo  *userInfo
	Token     template.HTML
	FlashMsg  template.HTML
	GitStatus template.HTML
}

type userInfo struct {
	Username   string
	IsAdmin    bool
	IsLoggedIn bool
}

type frontmatter struct {
	Title      string   `yaml:"title"`
	Tags       []string `yaml:"tags,omitempty"`
	Favorite   bool     `yaml:"favorite,omitempty"`
	Permission string   `yaml:"permission,omitempty"`
	Public     bool     `yaml:"public,omitempty"`
	Admin      bool     `yaml:"admin,omitempty"`
}

type badFrontmatter struct {
	Title      string `yaml:"title"`
	Tags       string `yaml:"tags,omitempty"`
	Favorite   bool   `yaml:"favorite,omitempty"`
	Permission string `yaml:"permission,omitempty"`
	Public     bool   `yaml:"public,omitempty"`
	Admin      bool   `yaml:"admin,omitempty"`
}

type wiki struct {
	Title       string
	Filename    string
	Frontmatter *frontmatter
	Content     []byte
	CreateTime  int64
	ModTime     int64
}

type wikiCache struct {
	SHA1  string
	Cache []*gitDirList
	Tags  map[string][]string
	Favs  map[string]struct{}
}

type wikiPage struct {
	*page
	Wiki     *wiki
	Rendered string
}

type commitPage struct {
	*page
	Wiki     *wiki
	Commit   string
	Rendered string
	Diff     string
}

type listPage struct {
	*page
	Wikis []*gitDirList
}

type genPage struct {
	*page
	Title string
}

type gitPage struct {
	*page
	Title     string
	GitStatus string
	GitRemote string
}

type historyPage struct {
	*page
	Wiki        *wiki
	Filename    string
	FileHistory []*commitLog
}

type tagMapPage struct {
	*page
	TagKeys map[string][]string
}

type searchPage struct {
	*page
	Results []*result
}

type recentsPage struct {
	*page
	Recents []*recent
}

type recent struct {
	Date      int64
	Commit    string
	Filenames []string
}

type result struct {
	Name   string
	Result string
}

type commitLog struct {
	Filename string
	Commit   string
	Date     int64
	Message  string
}

type gitDirList struct {
	Type       string
	Filename   string
	CreateTime int64
	ModTime    int64
	Permission string
}

type wikiEnv struct {
	authState *auth.State
	cache     *wikiCache
	templates map[string]*template.Template
}

// Sorting functions
type wikiByDate []*wiki

func (a wikiByDate) Len() int           { return len(a) }
func (a wikiByDate) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a wikiByDate) Less(i, j int) bool { return a[i].CreateTime < a[j].CreateTime }

type wikiByModDate []*wiki

func (a wikiByModDate) Len() int           { return len(a) }
func (a wikiByModDate) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a wikiByModDate) Less(i, j int) bool { return a[i].ModTime < a[j].ModTime }

func init() {
	raven.SetDSN("https://5ab2f68b0f524799b1d0b324350cc2ae:e01dbad12f8e4fd0bce97681a772a072@app.getsentry.com/94753")

	// Viper config.
	viper.SetDefault("Port", "5000")
	viper.SetDefault("Email", "unused@the.moment")
	viper.SetDefault("WikiDir", "./data/wikidata/")
	viper.SetDefault("Domain", "wiki.example.com")
	viper.SetDefault("GitRepo", "git@example.com:user/wikidata.git")
	viper.SetDefault("AdminUser", "admin")
	viper.SetDefault("PushOnSave", false)
	viper.SetDefault("InitWikiRepo", false)
	viper.SetDefault("Dev", false)
	viper.SetDefault("Debug", false)
	viper.SetDefault("CacheLocation", "./data/cache.gob")

	viper.SetEnvPrefix("gowiki")
	viper.AutomaticEnv()

	viper.SetConfigName("conf")
	viper.AddConfigPath("./data/")
	err := viper.ReadInConfig() // Find and read the config file
	if err != nil {             // Handle errors reading the config file
		//panic(fmt.Errorf("Fatal error config file: %s \n", err))
		log.Println("No configuration file loaded - using defaults")
	}

	if viper.GetBool("Debug") {
		httputils.Debug = true
	}

	//viper.SetConfigType("toml")

	/*
			Port     string
			Email    string
			WikiDir  string
			Domain  string
			GitRepo  string
		    AuthConf struct {
		        LdapEnabled bool
		        LdapConf struct {
		            LdapPort uint16 `json:",omitempty"`
		            LdapUrl  string `json:",omitempty"`
		            LdapDn   string `json:",omitempty"`
		            LdapUn   string `json:",omitempty"`
		            LdapOu   string `json:",omitempty"`
		        }
		    }
	*/

	//viper.Unmarshal(&cfg)
	//viper.UnmarshalKey("AuthConf", &auth.Authcfg)

	/*
		bufpool = bpool.NewBufferPool(64)
		if templates == nil {
			templates = make(map[string]*template.Template)
		}
	*/

	gitPath, err = exec.LookPath("git")
	if err != nil {
		log.Fatalln("git must be installed")
	}
	/*
		if cache == nil {
			cache = new(wikiCache)
		}
	*/
	/*
		templatesDir := "./templates/"
		layouts, err := filepath.Glob(templatesDir + "layouts/*.tmpl")
		if err != nil {
			panic(err)
		}
		includes, err := filepath.Glob(templatesDir + "includes/*.tmpl")
		if err != nil {
			panic(err)
		}

		funcMap := template.FuncMap{"prettyDate": utils.PrettyDate, "safeHTML": utils.SafeHTML, "imgClass": utils.ImgClass, "isAdmin": isAdmin, "isLoggedIn": isLoggedIn, "jsTags": jsTags}

		for _, layout := range layouts {
			files := append(includes, layout)
			//DEBUG TEMPLATE LOADING
			utils.Debugln(files)
			templates[filepath.Base(layout)] = template.Must(template.New("templates").Funcs(funcMap).ParseFiles(files...))
		}
	*/

	//var err error

}

func markdownRender(input []byte) string {
	defer httputils.TimeTrack(time.Now(), "markdownRender")
	renderer := &renderer{Html: blackfriday.HtmlRenderer(commonHTMLFlags, "", "").(*blackfriday.Html)}

	unsanitized := blackfriday.MarkdownOptions(input, renderer, blackfriday.Options{
		Extensions: commonExtensions})
	//p := bluemonday.UGCPolicy()
	//p.AllowElements("nav", "input", "li")
	//return string(p.SanitizeBytes(unsanitized))

	return string(unsanitized)
}

// Task List support.
func (r *renderer) ListItem(out *bytes.Buffer, text []byte, flags int) {
	switch {
	case bytes.HasPrefix(text, []byte("[ ] ")):
		text = append([]byte(`<i class="fa fa-square" aria-hidden="true"></i>`), text[3:]...)
	case bytes.HasPrefix(text, []byte("[x] ")) || bytes.HasPrefix(text, []byte("[X] ")):
		text = append([]byte(`<i class="fa fa-check-square" aria-hidden="true"></i>`), text[3:]...)
	}
	r.Html.ListItem(out, text, flags)
}

func (r *renderer) NormalText(out *bytes.Buffer, text []byte) {
	linkPattern := regexp.MustCompile(`\[\/(?P<Name>[0-9a-zA-Z-_\.\/]+)\]\(\)`)

	switch {
	case linkPattern.Match(text):
		domain := "//" + viper.GetString("Domain")
		link := linkPattern.ReplaceAll(text, []byte(domain+"/$1"))
		title := linkPattern.ReplaceAll(text, []byte("/$1"))
		r.Html.Link(out, link, []byte(""), title)
		return
	}
	r.Html.NormalText(out, text)

}

func check(err error) {
	if err != nil {
		pc, fn, line, ok := runtime.Caller(1)
		details := runtime.FuncForPC(pc)
		if ok && details != nil {
			//log.Fatalln(line, " Func: ", details.Name(), " Err: ", err)
			log.Printf("[error] in %s[%s:%d] %v", details.Name(), fn, line, err)
		}
	}
}

func checkErr(name string, err error) {
	if err != nil {
		log.Println("Function: " + name)
		log.Println(err)
		panic(err)
	}
}

func checkErrReturn(name string, err error) error {
	if err != nil {
		log.Println("Function: " + name)
		log.Println(err)
		return err
	}
	return nil
}

func appendIfMissing(slice []string, s string) []string {
	for _, ele := range slice {
		if ele == s {
			return slice
		}
	}
	return append(slice, s)
}

func httpErrorHandler(w http.ResponseWriter, r *http.Request, err interface{}) {
	data := struct {
		Error interface{}
	}{
		err,
	}

	errorPageTpl := `
	<html>
		<head>
			<title>Error</title>
			<meta http-equiv="Content-Type" content="text/html; charset=utf-8">
		</head>	
		<body> 
			<p>{{ .Error }}</p>
		</body>
	</html>`

	w.WriteHeader(http.StatusInternalServerError)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	tpl := template.Must(template.New("ErrorPage").Parse(errorPageTpl))
	tpl.Execute(w, data)
}

func errorHandler(w http.ResponseWriter, r *http.Request, err interface{}) {
	_, filePath, line, _ := runtime.Caller(4)
	data := struct {
		Filepath string
		Line     int
		Error    interface{}
	}{
		filePath,
		line,
		err,
	}
	//w.Write([]byte(function))

	panicPageTpl := `
	<html>
		<head>
			<title>Error</title>
			<meta http-equiv="Content-Type" content="text/html; charset=utf-8">
		</head>	
		<body>
			<p>File: {{ .Filepath }} {{ .Line }}</p> 
			<p>{{ .Error }}</p>
		</body>
	</html>`

	defer func() {
		if rval := recover(); rval != nil {
			debug.PrintStack()
			rvalStr := fmt.Sprint(rval)
			packet := raven.NewPacket(rvalStr, raven.NewException(errors.New(rvalStr), raven.NewStacktrace(2, 3, nil)), raven.NewHttp(r))
			raven.Capture(packet, nil)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}()

	tpl := template.Must(template.New("ErrorPage").Parse(panicPageTpl))
	tpl.Execute(w, data)

}

func timeNewContext(c context.Context, t time.Time) context.Context {
	return context.WithValue(c, timerKey, t)
}

func timeFromContext(c context.Context) time.Time {
	t, ok := c.Value(timerKey).(time.Time)
	if !ok {
		httputils.Debugln("No startTime in context.")
		t = time.Now()
	}
	return t
}

func newNameContext(c context.Context, t string) context.Context {
	return context.WithValue(c, wikiNameKey, t)
}

func nameFromContext(c context.Context) string {
	t, ok := c.Value(wikiNameKey).(string)
	if !ok {
		httputils.Debugln("No wikiName in context.")
		return ""
	}
	return t
}

func newWikiExistsContext(c context.Context, t bool) context.Context {
	return context.WithValue(c, wikiExistsKey, t)
}

func wikiExistsFromContext(c context.Context) bool {
	t, ok := c.Value(wikiExistsKey).(bool)
	if !ok {
		httputils.Debugln("No wikiExists in context.")
		return false
	}
	return t
}

func isAdmin(s string) bool {
	if s == "User" {
		return false
	} else if s == "Admin" {
		return true
	}
	return false
}

func isLoggedIn(s string) bool {
	if s == "" {
		return false
	}
	return true
}

func jsTags(tagS []string) string {
	var tags string
	for _, v := range tagS {
		tags = tags + ", " + v
	}
	tags = strings.TrimPrefix(tags, ", ")
	tags = strings.TrimSuffix(tags, ", ")
	return tags
}

// CUSTOM GIT WRAPPERS
// Construct an *exec.Cmd for `git {args}` with a workingDirectory
func gitCommand(args ...string) *exec.Cmd {
	c := exec.Command(gitPath, args...)
	c.Env = os.Environ()
	c.Env = append(c.Env, `GIT_COMMITTER_NAME="Golang Wiki"`, `GIT_COMMITTER_EMAIL="golangwiki@jba.io"`)
	c.Dir = viper.GetString("WikiDir")
	return c
}

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

// Execute `git status -s` in directory
// If there is output, the directory has is dirty
func gitIsClean() error {
	gitBehind := []byte("Your branch is behind")
	gitAhead := []byte("Your branch is ahead")
	gitDiverged := []byte("have diverged")

	// Check for untracked files first
	u := gitCommand("ls-files", "--exclude-standard", "--others")
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
	c := gitCommand("status", "-uno")

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

	/*
		if len(o) != 0 {
			return o, ErrGitDirty
		}
	*/

	return nil
}

func gitIsCleanStartup() error {
	gitBehind := []byte("Your branch is behind")
	gitAhead := []byte("Your branch is ahead")
	gitDiverged := []byte("have diverged")

	// Check for untracked files first
	u := gitCommand("ls-files", "--exclude-standard", "--others")
	uo, err := u.Output()
	if len(uo) != 0 {
		return errors.New("Untracked files: " + string(uo))
	}

	// Fetch changes from remote
	err = gitCommand("fetch").Run()
	if err != nil {
		return err
	}

	// Now check the status, minus untracked files
	c := gitCommand("status", "-uno")

	o, err := c.Output()
	if err != nil {
		return err
	}

	if bytes.Contains(o, gitBehind) {
		httputils.Debugln("gitIsCleanStartup: Pulling git repo...")
		return gitPull()
		//return errors.New(string(o))
		//return ErrGitBehind
	}

	if bytes.Contains(o, gitAhead) {
		httputils.Debugln("gitIsCleanStartup: Pushing git repo...")
		return gitPush()
		//return errors.New(string(o))
		//return ErrGitAhead
	}

	if bytes.Contains(o, gitDiverged) {
		return errors.New(string(o))
		//return ErrGitDiverged
	}

	/*
		if len(o) != 0 {
			return o, ErrGitDirty
		}
	*/

	return nil
}

// Execute `git add {filepath}` in workingDirectory
func gitAddFilepath(filepath string) error {
	o, err := gitCommand("add", filepath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error during `git add`: %s\n%s", err.Error(), string(o))
	}
	return nil
}

// Execute `git commit -m {msg}` in workingDirectory
func gitCommitWithMessage(msg string) error {
	o, err := gitCommand("commit", "--author", "'Golang Wiki <golangwiki@jba.io>'", "-m", msg).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error during `git commit`: %s\n%s", err.Error(), string(o))
	}

	return nil
}

// Execute `git commit -m "commit from GoWiki"` in workingDirectory
func gitCommitEmpty() error {
	o, err := gitCommand("commit", "--author", "'Golang Wiki <golangwiki@jba.io>'", "-m", "commit from GoWiki").CombinedOutput()
	if err != nil {
		return fmt.Errorf("error during `git commit`: %s\n%s", err.Error(), string(o))
	}

	return nil
}

// Execute `git push` in workingDirectory
func gitPush() error {
	o, err := gitCommand("push", "-u", "origin", "master").CombinedOutput()
	if err != nil {
		return fmt.Errorf("error during `git push`: %s\n%s", err.Error(), string(o))
	}

	return nil
}

// Execute `git push` in workingDirectory
func gitPull() error {
	o, err := gitCommand("pull").CombinedOutput()
	if err != nil {
		return fmt.Errorf("error during `git pull`: %s\n%s", err.Error(), string(o))
	}

	return nil
}

// File creation time, output to UNIX time
// git log --diff-filter=A --follow --format=%at -1 -- [filename]
func gitGetCtime(filename string) (int64, error) {
	defer httputils.TimeTrack(time.Now(), "gitGetCtime")
	//var ctime int64
	o, err := gitCommand("log", "--diff-filter=A", "--follow", "--format=%at", "-1", "--", filename).Output()
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
	checkErr("gitGetCtime()/ParseInt", err)

	return ctime, nil
}

// File modification time, output to UNIX time
// git log -1 --format=%at -- [filename]
func gitGetMtime(filename string) (int64, error) {
	defer httputils.TimeTrack(time.Now(), "gitGetMtime")
	//var mtime int64
	o, err := gitCommand("log", "--format=%at", "-1", "--", filename).Output()
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
	checkErr("gitGetMtime()/ParseInt", err)

	return mtime, nil
}

// File history
// git log --pretty=format:"commit:%H date:%at message:%s" [filename]
// git log --pretty=format:"%H,%at,%s" [filename]
func gitGetFileLog(filename string) ([]*commitLog, error) {
	o, err := gitCommand("log", "--pretty=format:%H,%at,%s", filename).Output()
	if err != nil {
		return nil, fmt.Errorf("error during `git log`: %s\n%s", err.Error(), string(o))
	}
	// split each commit onto it's own line
	logsplit := strings.Split(string(o), "\n")
	// now split each commit-line into it's slice
	// format should be: [sha1],[date],[message]
	var commits []*commitLog
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
		theCommit := &commitLog{
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
func gitGetFileCommit(filename, commit string) ([]byte, error) {
	// Combine these into one
	fullcommit := commit + ":" + filename
	o, err := gitCommand("show", fullcommit).CombinedOutput()
	if err != nil {
		return []byte{}, fmt.Errorf("error during `git show`: %s\n%s", err.Error(), string(o))
	}
	return o, nil
}

// Get diff for entire commit
// git show [commit sha1]
func gitGetFileCommitDiff(filename, commit string) ([]byte, error) {
	o, err := gitCommand("show", commit).CombinedOutput()
	if err != nil {
		return []byte{}, fmt.Errorf("error during `git show`: %s\n%s", err.Error(), string(o))
	}
	return o, nil
}

// File modification time for specific commit, output to UNIX time
// git log -1 --format=%at [commit sha1]
func gitGetFileCommitMtime(commit string) (int64, error) {
	//var mtime int64
	o, err := gitCommand("log", "--format=%at", "-1", commit).Output()
	if err != nil {
		return 0, fmt.Errorf("error during `git log -1 --format=at --`: %s\n%s", err.Error(), string(o))
	}
	ostring := strings.TrimSpace(string(o))
	mtime, err := strconv.ParseInt(ostring, 10, 64)
	checkErr("gitGetFileCommitMtime()/ParseInt", err)

	return mtime, nil
}

// git ls-files
func gitLs() ([]string, error) {
	o, err := gitCommand("ls-files", "-z").Output()
	if err != nil {
		return nil, fmt.Errorf("error during `git ls-files`: %s\n%s", err.Error(), string(o))
	}
	nul := bytes.Replace(o, []byte("\x00"), []byte("\n"), -1)
	// split each commit onto it's own line
	lssplit := strings.Split(string(nul), "\n")
	return lssplit, nil
}

// git ls-tree -r -t HEAD
func gitLsTree() ([]*gitDirList, error) {
	o, err := gitCommand("ls-tree", "-r", "-t", "-z", "HEAD").Output()
	if err != nil {
		return nil, fmt.Errorf("error during `git ls-files`: %s\n%s", err.Error(), string(o))
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
func gitLsTreeDir(dir string) ([]*gitDirList, error) {
	o, err := gitCommand("ls-tree", "-z", "HEAD:"+dir).Output()
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
func gitHistory() ([]string, error) {
	o, err := gitCommand("log", "--name-only", "--pretty=format:_END %at %H", "-z", "HEAD").Output()
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
func gitGetTimes(filename string) (ctime int64, mtime int64) {
	defer httputils.TimeTrack(time.Now(), "gitGetTimes")

	ctimeChan := make(chan int64)
	//var ctime int64
	go func() {
		co, err := gitCommand("log", "--diff-filter=A", "--follow", "--format=%at", "-1", "--", filename).Output()
		if err != nil {
			log.Println(err)
			ctimeChan <- 0
			return
		}
		costring := strings.TrimSpace(string(co))
		// If output is blank, no point in wasting CPU doing the rest
		if costring == "" {
			log.Println(filename + " is not checked into Git")
			ctimeChan <- 0
			return
		}
		ctime, err = strconv.ParseInt(costring, 10, 64)
		checkErr("gitGetCtime()/ParseInt", err)
		ctimeChan <- ctime
	}()

	mtimeChan := make(chan int64)
	//var mtime int64
	go func() {
		mo, err := gitCommand("log", "--format=%at", "-1", "--", filename).Output()
		if err != nil {
			log.Println(err)
			mtimeChan <- 0
			return
		}
		mostring := strings.TrimSpace(string(mo))
		// If output is blank, no point in wasting CPU doing the rest
		if mostring == "" {
			log.Println(filename + " is not checked into Git")
			mtimeChan <- 0
			return
		}
		mtime, err = strconv.ParseInt(mostring, 10, 64)
		checkErr("gitGetMtime()/ParseInt", err)
		mtimeChan <- mtime
	}()

	for i := 0; i < 2; i++ {
		select {
		case ctime = <-ctimeChan:
		case mtime = <-mtimeChan:
		}
	}
	//ctime = <-ctimeChan
	//mtime = <-mtimeChan

	return ctime, mtime
}

// Search results, via git
// git grep --break 'searchTerm'
func gitSearch(searchTerm, fileSpec string) []*result {
	var results []*result
	cmd := exec.Command("/bin/sh", "-c", gitPath+" grep "+searchTerm+" -- "+fileSpec)
	//o := gitCommand("grep", "omg -- 'index'")
	cmd.Dir = viper.GetString("WikiDir")
	o, err := cmd.CombinedOutput()
	if err != nil {
		log.Println("ERROR gitSearch:", err)
		log.Println(string(o))
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
			theResult := &result{
				Name:   vs[0],
				Result: vs[1],
			}
			results = append(results, theResult)
		}
	}
	return results
}

func loadPage(env *wikiEnv, r *http.Request) *page {
	defer httputils.TimeTrack(time.Now(), "loadPage")
	//timer.Step("loadpageFunc")

	// Auth lib middlewares should load the user and tokens into context for reading
	user, isAdmin := auth.GetUsername(r.Context())
	msg := auth.GetFlash(r.Context())
	//token := auth.GetToken(r.Context())
	token := csrf.TemplateField(r)

	var message template.HTML
	if msg != "" {
		message = template.HTML(`
			<div class="notification anim" id="notification">
			<p>` + msg + `
			<button class="close-button" aria-label="Dismiss alert" type="button">
			<span aria-hidden="true">&times;</span>
			</button></p>
			</div>
		`)
	} else {
		message = template.HTML("")
	}

	/*
		// Grab list of favs from channel
		favs := make(chan []string)
		go favsHandler(favs)
		gofavs := <-favs
	*/

	// Grab git status
	//gitStatusErr := gitIsClean()
	gitHTML := gitIsCleanURLs(token)
	/*
		if gitStatusErr == nil {
			gitStatusErr = errors.New("Git repo is clean")
		}
	*/

	favs := env.cache.Favs
	if env.cache.Favs == nil {
		favs = make(map[string]struct{})
	}

	return &page{
		SiteName: "GoWiki",
		Favs:     favs,
		UserInfo: &userInfo{
			Username:   user,
			IsAdmin:    isAdmin,
			IsLoggedIn: auth.IsLoggedIn(r.Context()),
		},
		Token:     token,
		FlashMsg:  message,
		GitStatus: gitHTML,
	}
}

func gitIsCleanURLs(token template.HTML) template.HTML {
	switch gitIsClean() {
	case errGitAhead:
		return template.HTML(`<form method="post" action="/admin/git/push" id="git_push">` + token + `<i class="fa fa-cloud-upload" aria-hidden="true"></i><button type="submit" class="button">Push git</button></form>`)
	case errGitBehind:
		return template.HTML(`<form method="post" action="/admin/git/pull" id="git_pull">` + token + `<i class="fa fa-cloud-download" aria-hidden="true"></i><button type="submit" class="button">Pull git</button></form>`)
	case errGitDiverged:
		return template.HTML(`<a href="/admin/git"><i class="fa fa-exclamation-triangle" aria-hidden="true"></i>Issue with git:wiki!</a>`)
	default:
		return template.HTML(`<p>Git repo is clean.</p>`)
	}
}

func (env *wikiEnv) historyHandler(w http.ResponseWriter, r *http.Request) {
	name := nameFromContext(r.Context())
	wikiExists := wikiExistsFromContext(r.Context())
	if !wikiExists {
		httputils.Debugln("wikiExists false: No such file...creating one.")
		//http.Redirect(w, r, "/edit/"+name, http.StatusTemporaryRedirect)
		env.createWiki(w, r, name)
		return
	}

	wikip := loadWikiPage(env, r, name)

	history, err := gitGetFileLog(name)
	if err != nil {
		panic(err)
	}
	hp := &historyPage{
		wikip.page,
		wikip.Wiki,
		name,
		history,
	}
	renderTemplate(r.Context(), env, w, "wiki_history.tmpl", hp)
}

// Need to get content of the file at specified commit
// > git show [commit sha1]:[filename]
// As well as the date
// > git log -1 --format=%at [commit sha1]
// TODO: need to find a way to detect sha1s
func (env *wikiEnv) viewCommitHandler(w http.ResponseWriter, r *http.Request, commit, name string) {
	var fm frontmatter
	var pageContent string

	//commit := vars["commit"]

	p := loadPage(env, r)

	body, err := gitGetFileCommit(name, commit)
	if err != nil {
		panic(err)
	}
	ctime, err := gitGetCtime(name)
	if err != nil && err != errNotInGit {
		panic(err)
	}
	mtime, err := gitGetFileCommitMtime(commit)
	if err != nil {
		panic(err)
	}
	diff, err := gitGetFileCommitDiff(name, commit)
	if err != nil {
		panic(err)
	}

	// Read YAML frontmatter into fm
	reader := bytes.NewReader(body)
	fm, content := readWikiPage(reader)
	checkErr("viewCommitHandler()/readWikiPage", err)

	// Render remaining content after frontmatter
	md := markdownRender(content)
	//md := commonmarkRender(content)

	pagetitle := setPageTitle(fm.Title, name)

	diffstring := string(diff)

	pageContent = md

	cp := &commitPage{
		page: p,
		Wiki: &wiki{
			Title:       pagetitle,
			Filename:    name,
			Frontmatter: &fm,
			Content:     content,
			CreateTime:  ctime,
			ModTime:     mtime,
		},
		Commit:   commit,
		Rendered: pageContent,
		Diff:     diffstring,
	}

	renderTemplate(r.Context(), env, w, "wiki_commit.tmpl", cp)

}

// TODO: Fix this
func (env *wikiEnv) recentHandler(w http.ResponseWriter, r *http.Request) {

	p := loadPage(env, r)

	gh, err := gitHistory()
	checkErr("recentHandler()/gitHistory", err)

	/*
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(200)
	*/
	var split []string
	var split2 []string
	var recents []*recent

	for _, v := range gh {
		split = strings.Split(strings.TrimSpace(v), " ")
		date, err := strconv.ParseInt(split[0], 0, 64)
		if err != nil {
			panic(err)
		}

		// If there is a filename (initial one will not have it)...
		split2 = strings.Split(split[1], "\n")
		if len(split2) >= 2 {

			r := &recent{
				Date:      date,
				Commit:    split2[0],
				Filenames: strings.Split(split2[1], "\n"),
			}
			//w.Write([]byte(v + "<br>"))
			recents = append(recents, r)
		}
	}

	s := &recentsPage{p, recents}
	renderTemplate(r.Context(), env, w, "recents.tmpl", s)

}

func (env *wikiEnv) listHandler(w http.ResponseWriter, r *http.Request) {

	p := loadPage(env, r)

	var list []*gitDirList

	userLoggedIn := auth.IsLoggedIn(r.Context())
	_, isAdmin := auth.GetUsername(r.Context())

	for _, v := range env.cache.Cache {
		if v.Permission == "public" {
			//log.Println("pubic", v.Filename)
			list = append(list, v)
		}
		if userLoggedIn {
			if v.Permission == "private" {
				//log.Println("priv", v.Filename)
				list = append(list, v)
			}
		}
		if isAdmin {
			if v.Permission == "admin" {
				//log.Println("admin", v.Filename)
				list = append(list, v)
			}
		}
	}

	l := &listPage{p, list}
	renderTemplate(r.Context(), env, w, "list.tmpl", l)
}

func readFileAndFront(filename string) (frontmatter, []byte) {
	//defer httputils.TimeTrack(time.Now(), "readFileAndFront")

	f, err := os.Open(filename)
	//checkErr("readFileAndFront()/Open", err)
	if err != nil {
		httputils.Debugln("Error in readFileAndFront:", err)
		f.Close()
		return frontmatter{}, []byte("")
	}

	fm, content := readWikiPage(f)
	f.Close()
	return fm, content
}

func readWikiPage(reader io.Reader) (frontmatter, []byte) {
	//defer httputils.TimeTrack(time.Now(), "readWikiPage")
	/*
		s := bufio.NewScanner(reader)

		topbuf := new(bytes.Buffer)
		bottombuf := new(bytes.Buffer)
		start := false
		end := false
		for s.Scan() {

			if start && end {
				bottombuf.Write(s.Bytes())
				bottombuf.WriteString("\n")
			}
			if start && !end {
				// Anything after the --- tag, add to the topbuffer
				if s.Text() != yamlsep || s.Text() != yamlsep2 {
					topbuf.Write(s.Bytes())
					topbuf.WriteString("\n")
				}
				if s.Text() == yamlsep || s.Text() == yamlsep2 {
					end = true
				}
			}

			// Hopefully catch the first --- tag
			if !start && !end {
				if s.Text() == yamlsep {
					start = true
				} else {
					start = true
					end = true
				}

			}
		}
	*/
	topbuf := new(bytes.Buffer)
	bottombuf := new(bytes.Buffer)
	scanWikiPage(reader, topbuf, bottombuf)

	return marshalFrontmatter(topbuf.Bytes()), bottombuf.Bytes()
}

func readFront(reader io.Reader) frontmatter {
	//defer httputils.TimeTrack(time.Now(), "readFront")
	/*
		s := bufio.NewScanner(reader)

		topbuf := new(bytes.Buffer)
		start := false
		end := false
		for s.Scan() {

			if start && end {
				break
			}
			if start && !end {
				// Anything after the --- tag, add to the topbuffer
				if s.Text() != yamlsep || s.Text() != yamlsep2 {
					topbuf.Write(s.Bytes())
					topbuf.WriteString("\n")
				}
				// This should be the end separator
				if s.Text() == yamlsep || s.Text() == yamlsep2 {
					end = true
					break
				}
			}

			// Hopefully catch the first --- tag
			if !start && !end {
				if s.Text() == yamlsep {
					start = true
				} else {
					start = true
					end = true
				}

			}
		}
	*/
	topbuf := new(bytes.Buffer)
	scanWikiPage(reader, topbuf)

	return marshalFrontmatter(topbuf.Bytes())

}

func scanWikiPage(reader io.Reader, bufs ...*bytes.Buffer) {
	grabPage := false
	// If we are given two buffers, do something with the bottom data
	if len(bufs) == 2 {
		grabPage = true
	}
	scanner := bufio.NewScanner(reader)
	start := false
	end := false
	for scanner.Scan() {

		if start && end {
			if grabPage {
				bufs[1].Write(scanner.Bytes())
				bufs[1].WriteString("\n")
			} else {
				break
			}
		}
		if start && !end {
			// Anything after the --- tag, add to the topbuffer
			if scanner.Text() != yamlsep || scanner.Text() != yamlsep2 {
				bufs[0].Write(scanner.Bytes())
				bufs[0].WriteString("\n")
			}
			if scanner.Text() == yamlsep || scanner.Text() == yamlsep2 {
				end = true
				// If not given 2 buffers, end here.
				if !grabPage {
					break
				}
			}
		}

		// Hopefully catch the first --- tag
		if !start && !end {
			if scanner.Text() == yamlsep {
				start = true
			} else {
				start = true
				end = true
			}
		}
	}
}

func marshalFrontmatter(fmdata []byte) (fm frontmatter) {
	//defer httputils.TimeTrack(time.Now(), "marshalFrontmatter")

	if fmdata != nil {
		err := yaml.Unmarshal(fmdata, &fm)
		// Try and handle malformed YAML here
		if err != nil {
			m := map[string]interface{}{}
			err2 := yaml.Unmarshal(fmdata, &m)
			if err2 != nil {
				return frontmatter{}
			}

			title, titlefound := m["title"].(string)
			if titlefound {
				fm.Title = title
			}
			switch v := m["tags"].(type) {
			case string:
				fm.Tags = strings.Split(v, ",")
			case []string:
				fm.Tags = v
			default:
				fm.Tags = []string{}
			}
			favorite, favfound := m["favorite"].(bool)
			if favfound {
				fm.Favorite = favorite
			}
			public, pubfound := m["public"].(bool)
			if pubfound && public {
				fm.Permission = "public"
			}
			admin, adminfound := m["admin"].(bool)
			if adminfound && admin {
				fm.Permission = "admin"
			}
			permission, permfound := m["permission"].(string)
			if permfound {
				fm.Permission = permission
			}
		}
		// Deal with old Public and Admin tags
		if fm.Permission == "" {
			if fm.Public {
				fm.Permission = "public"
			}
			if !fm.Public {
				fm.Permission = "private"
			}
			if fm.Admin {
				fm.Permission = "admin"
			}
		}
	}
	return fm
}

func renderTemplate(c context.Context, env *wikiEnv, w http.ResponseWriter, name string, data interface{}) {
	tmpl, ok := env.templates[name]
	if !ok {
		log.Println(fmt.Errorf("The template %s does not exist", name))
		panic(fmt.Errorf("The template %s does not exist", name))
	}

	// Create buffer to write to and check for errors
	bufpool := bpool.NewBufferPool(64)
	buf := bufpool.Get()
	err := tmpl.ExecuteTemplate(buf, "base", data)
	if err != nil {
		log.Println("renderTemplate error:")
		log.Println(err)
		bufpool.Put(buf)
		panic(err)
	}

	// Set the header and write the buffer to w
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Squeeze in our response time here
	// Real hacky solution, but better than modifying the struct
	start := timeFromContext(c)
	elapsed := time.Since(start)
	tmpl.Execute(buf, elapsed.String())
	err = tmpl.ExecuteTemplate(buf, "footer", elapsed.String())
	if err != nil {
		log.Println("renderTemplate error:")
		log.Println(err)
		bufpool.Put(buf)
		panic(err)
	}
	err = tmpl.ExecuteTemplate(buf, "bottom", data)
	if err != nil {
		log.Println("renderTemplate error:")
		log.Println(err)
		bufpool.Put(buf)
		panic(err)
	}
	buf.WriteTo(w)
	bufpool.Put(buf)
}

func parseBool(value string) bool {
	boolValue, err := strconv.ParseBool(value)
	if err != nil {
		return false
	}
	return boolValue
}

// doesPageExist checks if the given name exists, and is a  regular file
// If there is anything wrong, it panics
func doesPageExist(name string) bool {
	defer httputils.TimeTrack(time.Now(), "doesPageExist")

	exists := false

	fileInfo, err := os.Stat(name)
	if err == nil {
		fileMode := fileInfo.Mode()
		if fileMode.IsRegular() {
			exists = true
		}
	}

	if err != nil {
		if os.IsNotExist(err) {
			exists = false
			//} else if !fileInfo. {
			//	log.Println("not a dir?")
			//	exists = false
		} else {
			log.Println("doesPageExist, unhandled error: ")
			log.Println(err)
			//panic(err)
			exists = false
		}
	}

	return exists
}

// Check that the given full path is relative to the configured wikidir
func relativePathCheck(name string) error {
	defer httputils.TimeTrack(time.Now(), "relativePathCheck")
	fullfilename := filepath.Join(viper.GetString("WikiDir"), name)
	dir, _ := filepath.Split(name)
	if dir != "" {
		dirErr := checkDir(dir)
		if dirErr != nil {
			return dirErr
		}
	}

	_, err := filepath.Rel(viper.GetString("WikiDir"), fullfilename)
	return err
}

// This does various checks to see if an existing page exists or not
// Also checks for and returns an error on some edge cases
// So we only proceed if this returns false AND nil
// Edge cases checked for currently:
// - If name is trying to escape or otherwise a bad path
// - If name is a /directory/file combo, but /directory is actually a file
func checkName(name *string) (bool, error) {
	defer httputils.TimeTrack(time.Now(), "checkName")

	separators := regexp.MustCompile(`[ &_=+:]`)
	dashes := regexp.MustCompile(`[\-]+`)

	// First 'sanitize' the name
	//log.Println(name)
	*name = strings.Replace(*name, "..", "", -1)
	//log.Println(name)
	*name = path.Clean(*name)
	//log.Println(name)
	// Remove trailing spaces
	*name = strings.Trim(*name, " ")
	//log.Println(name)

	/*
		// Directory without specified index
		if strings.HasSuffix(*name, "/") {
			*name = filepath.Join(*name, "index")
		}
	*/

	// Check that no one is trying to escape out of wikiDir, etc
	// Very important to check it here, before trying to check if it exists
	relErr := relativePathCheck(*name)
	if relErr != nil {
		return false, relErr
	}

	// Build the full path
	fullfilename := filepath.Join(viper.GetString("WikiDir"), *name)

	exists := doesPageExist(fullfilename)

	// If name doesn't exist, and there is no file extension given, try .page and then .md
	if !exists {
		possbileExts := []string{".md", ".page"}
		for _, ext := range possbileExts {
			if !exists && (filepath.Ext(*name) == "") {
				if doesPageExist(fullfilename + ext) {
					*name = *name + ext
					httputils.Debugln(*name + " found!")
					exists = true
					break
				}
			}
		}
	}
	/*
		if !exists && (filepath.Ext(*name) == "") {

			if doesPageExist(fullfilename + ".md") {
				*name = *name + ".md"
				log.Println(*name + " found!")
				exists = true
			}
			if doesPageExist(fullfilename + ".page") {
				*name = *name + ".page"
				log.Println(*name + " found!")
				exists = true
			}
		}
	*/

	// If original filename does not exist, normalize the filename, and check if that exists
	if !exists {
		// Normalize the name if the original name doesn't exist
		*name = strings.ToLower(*name)
		*name = separators.ReplaceAllString(*name, "-")
		*name = dashes.ReplaceAllString(*name, "-")
		fullnewfilename := filepath.Join(viper.GetString("WikiDir"), *name)
		exists = doesPageExist(fullnewfilename)
	}
	return exists, nil

}

// checkDir should perform a recursive check over all directory elements of a given path,
//  and check that we're not trying to overwrite a file with a directory
func checkDir(dir string) error {
	defer httputils.TimeTrack(time.Now(), "checkDir")

	dirs := strings.Split(dir, "/")
	var relpath string
	var err error
	for _, v := range dirs {
		// relpath progressively builds up the /path/to/file, element by element
		relpath = filepath.Join(relpath, v)
		// We combine that with the configured WikiDir to get the fullpath
		fullpath := filepath.Join(viper.GetString("WikiDir"), relpath)
		// Then try and open the fullpath to the element in question
		file, fileOpenErr := os.Open(fullpath)
		// If it doesn't exist, move on
		if os.IsNotExist(fileOpenErr) {
			err = nil
		} else if fileOpenErr != nil && !os.IsNotExist(fileOpenErr) {
			err = fileOpenErr
			break
		} else if fileOpenErr == nil {
			fileInfo, fileInfoErr := file.Stat()
			// If there is an error, and it's not just a non-existent file, panic
			if fileInfoErr != nil && !os.IsNotExist(fileInfoErr) {
				log.Println("Unhandled checkDir/fileInfo error: ")
				err = fileInfoErr
				break
			}
			if fileInfoErr == nil {
				// If the 'file' can be opened, now determine if it's a file or a directory
				fileMode := fileInfo.Mode()
				// I believe this should be the only path to success...
				if fileMode.IsDir() {
					err = nil
				} else {
					err = errBaseNotDir
					break
				}
			}
		}

		//log.Println(dirs[:k])
	}
	//log.Println(relpath)
	return err
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "indexHandler")

	http.Redirect(w, r, "/index", http.StatusSeeOther)
	//viewHandler(w, r, "index")
}

func (env *wikiEnv) viewHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "viewHandler")

	name := nameFromContext(r.Context())

	nameStat, err := os.Stat(filepath.Join(viper.GetString("WikiDir"), name))
	if err != nil {
		log.Println(err)
	}
	if err == nil {
		if nameStat.IsDir() {
			// Check if name/index exists, and if it does, serve it
			_, err := os.Stat(filepath.Join(viper.GetString("WikiDir"), name, "index"))
			if err == nil {
				/*
					name = filepath.Join(name, "index")
					ctx := newWikiExistsContext(r.Context(), true)
					p := loadWikiPage(env, r.WithContext(ctx), name)
					renderTemplate(ctx, env, w, "wiki_view.tmpl", p)
				*/
				http.Redirect(w, r, filepath.Join(name, "index"), http.StatusFound)
				return
			}
			if os.IsNotExist(err) {
				// TODO: List directory
				log.Println("TODO: List directory")
			}
		}
	}

	wikiExists := wikiExistsFromContext(r.Context())
	if !wikiExists {
		httputils.Debugln("wikiExists false: No such file...creating one.")
		//http.Redirect(w, r, "/edit/"+name, http.StatusTemporaryRedirect)
		env.createWiki(w, r, name)
		return
	}

	// If this is a commit, pass along the SHA1 to that function
	if r.URL.Query().Get("commit") != "" {
		commit := r.URL.Query().Get("commit")
		//utils.Debugln(r.URL.Query().Get("commit"))
		env.viewCommitHandler(w, r, commit, name)
		return
	}

	fileType := getFileType(name)
	if fileType == "wiki" {
		httputils.Debugln("Yay proper wiki page!")
		// Get Wiki
		p := loadWikiPage(env, r, name)
		renderTemplate(r.Context(), env, w, "wiki_view.tmpl", p)
		return
	} else {
		http.ServeFile(w, r, filepath.Join(viper.GetString("WikiDir"), name))
		return
		/*
			var html template.HTML
			if strings.Contains(fileType, "image") {
				html = template.HTML(`<img src="/` + name + `">`)
			}
			p := loadPage(env, r)
			data := struct {
				*page
				Title   string
				TheHTML template.HTML
			}{
				p,
				name,
				html,
			}
			renderTemplate(r.Context(), env, w, "file_view.tmpl", data)
		*/
	}

}

func getFileType(filename string) string {
	var realFileType string
	file, err := os.Open(filepath.Join(viper.GetString("WikiDir"), filename))
	if err != nil {
		log.Println(err)
	}
	defer file.Close()
	buff := make([]byte, 512)
	_, err = file.Read(buff)
	if err != nil {
		log.Println(err)
	}
	filetype := http.DetectContentType(buff)
	//log.Println(filetype)
	if filetype == "application/octet-stream" {
		// Definitely wiki page...but others probably
		if string(buff[:3]) == "---" {
			realFileType = "wiki"
		}
	} else if filetype == "text/plain; charset=utf-8" {
		realFileType = "wiki"
	} else {
		realFileType = filetype
	}
	return realFileType
}

func (env *wikiEnv) editHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "editHandler")
	name := nameFromContext(r.Context())
	p := loadWikiPage(env, r, name)
	renderTemplate(r.Context(), env, w, "wiki_edit.tmpl", p)
}

func (env *wikiEnv) saveHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "saveHandler")

	name := nameFromContext(r.Context())

	r.ParseForm()
	//txt := r.Body
	content := r.FormValue("editor")
	//bwiki := txt

	// Check for and install required YAML frontmatter
	title := r.FormValue("title")
	// This is the separate input that tagdog.js throws new tags into
	tags := r.FormValue("tags_all")
	favorite := r.FormValue("favorite")
	permission := r.FormValue("permission")

	favoritebool := false
	if favorite == "on" {
		favoritebool = true
	}

	if title == "" {
		title = name
	}

	var tagsA []string
	if tags != "" {
		tagsA = strings.Split(tags, ",")
	}

	fm := &frontmatter{
		Title:      title,
		Tags:       tagsA,
		Favorite:   favoritebool,
		Permission: permission,
	}

	thewiki := &wiki{
		Title:       title,
		Filename:    name,
		Frontmatter: fm,
		Content:     []byte(content),
	}

	err := thewiki.save()
	if err != nil {
		panic(err)
	}

	// If PushOnSave is enabled, push to remote repo after save
	if viper.GetBool("PushOnSave") {
		err := gitPush()
		if err != nil {
			panic(err)
		}
	}

	go env.refreshStuff()

	env.authState.SetFlash("Wiki page successfully saved.", w, r)
	http.Redirect(w, r, "/"+name, http.StatusSeeOther)
	log.Println(name + " page saved!")
}

func newHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "newHandler")

	pagetitle := r.FormValue("newwiki")

	relErr := relativePathCheck(pagetitle)
	if relErr != nil {
		if relErr == errBaseNotDir {
			log.Println("ERROR: Cannot create subdir of a file:", pagetitle)
			http.Error(w, "Cannot create subdir of a file.", 500)
			return
		}
		httpErrorHandler(w, r, relErr)
		return
	}

	http.Redirect(w, r, "/"+pagetitle, http.StatusSeeOther)

	/* Off-loading most of this, by just redirecting to the pagetitle
	//fullfilename := cfg.WikiDir + pagetitle
		fullfilename := filepath.Join(viper.GetString("WikiDir"), pagetitle)
		rel, err := filepath.Rel(viper.GetString("WikiDir"), fullfilename)
		if err != nil {
			panic(err)
		}
		if strings.HasPrefix(rel, "../") {
			panic(err)
		}

		_, fierr := os.Stat(pagetitle)
		if os.IsNotExist(fierr) {

	checkName(&pagetitle)
	fullfilename := filepath.Join(viper.GetString("WikiDir"), pagetitle)
	feErr := relativePathCheck(pagetitle)
	if feErr != nil {
		log.Println(feErr)
		httpErrorHandler(w, r, feErr)
		return
	}

	// If page does not exist, ask to create it
	if !doesPageExist(fullfilename) {
		createWiki(w, r, pagetitle)
		//http.Redirect(w, r, "/edit/"+pagetitle, http.StatusTemporaryRedirect)
		return
	}

	// If pagetitle2 isn't blank, and there is no error returned by checkName,
	//   that should mean the page exists, so redirect to it.
	if pagetitle != "" && feErr == nil {
		http.Redirect(w, r, pagetitle, http.StatusTemporaryRedirect)
	}

	return
	*/

}

func urlFromPath(path string) string {
	url := filepath.Clean(viper.GetString("WikiDir")) + "/"
	return strings.TrimPrefix(path, url)
}

func favsHandler(env *wikiEnv, favs chan []string) {
	defer httputils.TimeTrack(time.Now(), "favsHandler")

	//favss := favbuf.String()
	var sfavs []string
	for fav := range env.cache.Favs {
		sfavs = append(sfavs, fav)
	}

	//httputils.Debugln("Favorites: " + favss)
	//sfavs := strings.Fields(favss)

	sort.Strings(sfavs)

	favs <- sfavs
}

func loadWiki(name string) *wiki {
	defer httputils.TimeTrack(time.Now(), "loadWiki")

	var fm frontmatter

	fm, content := readFileAndFront(filepath.Join(viper.GetString("WikiDir"), name))

	pagetitle := setPageTitle(fm.Title, name)

	/*
		ctime, err := gitGetCtime(name)
		checkErr("loadWiki()/gitGetCtime", err)

		mtime, err := gitGetMtime(name)
		checkErr("loadWiki()/gitGetMtime", err)
	*/
	ctime, mtime := gitGetTimes(name)

	return &wiki{
		Title:       pagetitle,
		Filename:    name,
		Frontmatter: &fm,
		Content:     content,
		CreateTime:  ctime,
		ModTime:     mtime,
	}

}

//////////////////////////////
/* Get type WikiPage struct {
	PageTitle    string
	Filename     string
	*Frontmatter
	*Wiki
}
type Wiki struct {
	Rendered     string
    Content      string
}*/
/////////////////////////////
func loadWikiPage(env *wikiEnv, r *http.Request, name string) *wikiPage {
	defer httputils.TimeTrack(time.Now(), "loadWikiPage")

	var thePage *page
	thePageChan := make(chan *page)
	var theWiki *wiki
	theWikiChan := make(chan *wiki)
	var theMarkdown string
	theMarkdownChan := make(chan string)

	go func() {
		p := loadPage(env, r)
		thePageChan <- p
	}()

	wikiExists := wikiExistsFromContext(r.Context())
	if !wikiExists {
		go func() {
			theWiki = &wiki{
				Title:    name,
				Filename: name,
				Frontmatter: &frontmatter{
					Title: name,
				},
				CreateTime: 0,
				ModTime:    0,
			}
			theWikiChan <- theWiki
			theMarkdownChan <- theMarkdown
		}()
	}
	if wikiExists {
		go func() {
			wikip := loadWiki(name)
			// Render remaining content after frontmatter
			md := markdownRender(wikip.Content)
			theWikiChan <- wikip
			theMarkdownChan <- md
		}()
	}

	for i := 0; i < 3; i++ {
		select {
		case theWiki = <-theWikiChan:
		case thePage = <-thePageChan:
		case theMarkdown = <-theMarkdownChan:
		}
	}

	//md := commonmarkRender(wikip.Content)
	//markdownRender2(wikip.Content)

	wp := &wikiPage{
		page:     thePage,
		Wiki:     theWiki,
		Rendered: theMarkdown,
	}
	return wp
}

func (wiki *wiki) save() error {
	defer httputils.TimeTrack(time.Now(), "wiki.save()")

	dir, filename := filepath.Split(wiki.Filename)
	fullfilename := filepath.Join(viper.GetString("WikiDir"), dir, filename)

	// If directory doesn't exist, create it
	// - Check if dir is null first
	if dir != "" {
		//dirpath := cfg.WikiDir + dir
		dirpath := filepath.Join(viper.GetString("WikiDir"), dir)
		if _, err := os.Stat(dirpath); os.IsNotExist(err) {
			err := os.MkdirAll(dirpath, 0755)
			checkErrReturn("save()/MkdirAll", err)
		}
	}
	/*
		originalFile, err := ioutil.ReadFile(fullfilename)
		checkErr("wiki.save()/ReadFile", err)
	*/

	// Create a buffer where we build the content of the file
	var f *os.File
	var err error
	f, err = os.OpenFile(fullfilename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
	checkErrReturn("save()/OpenFile", err)

	//buffer := new(bytes.Buffer)
	wb := bufio.NewWriter(f)

	_, err = wb.WriteString("---\n")
	checkErrReturn("save()/WriteString1", err)

	yamlBuffer, err := yaml.Marshal(wiki.Frontmatter)
	checkErrReturn("save()/yaml.Marshal", err)

	_, err = wb.Write(yamlBuffer)
	checkErrReturn("save()/Write yamlBuffer", err)

	_, err = wb.WriteString("---\n")
	checkErrReturn("save()/WriteString2", err)

	_, err = wb.Write(wiki.Content)
	checkErrReturn("save()/wb.Write wiki.Content", err)

	err = wb.Flush()
	checkErrReturn("save()/wb.Flush", err)

	err = f.Close()
	checkErrReturn("save()/f.Close", err)

	// Test equality of the original file, plus the buffer we just built
	//log.Println(bytes.Equal(originalFile, buffer.Bytes()))
	/*
		if bytes.Equal(originalFile, wb.Bytes()) {
			log.Println("No changes detected.")
			return nil
		}
	*/

	// Write contents of above buffer, which should be Frontmatter+WikiContent
	//ioutil.WriteFile(fullfilename, buffer.Bytes(), 0755)
	/*
		_, err = w.Write(buffer.Bytes())
		if err != nil {
			checkErr("wiki.save()/Write", err)
			return err
		}
	*/

	gitfilename := dir + filename

	err = gitAddFilepath(gitfilename)
	checkErrReturn("save()/gitAddFilepath", err)

	// FIXME: add a message box to edit page, check for it here
	err = gitCommitEmpty()
	checkErrReturn("save()/gitCommitEmpty", err)

	log.Println(fullfilename + " has been saved.")
	return nil

}

func (env *wikiEnv) loginPageHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "loginPageHandler")

	title := "login"
	p := loadPage(env, r)

	gp := &genPage{
		p,
		title,
	}
	renderTemplate(r.Context(), env, w, "login.tmpl", gp)
}

func (env *wikiEnv) signupPageHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "signupPageHandler")

	title := "signup"
	p := loadPage(env, r)

	gp := &genPage{
		p,
		title,
	}
	renderTemplate(r.Context(), env, w, "signup.tmpl", gp)
}

func (env *wikiEnv) adminUsersHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "adminUsersHandler")

	title := "admin-users"
	p := loadPage(env, r)

	userlist, err := env.authState.Userlist()
	if err != nil {
		panic(err)
	}

	data := struct {
		*page
		Title string
		Users []string
	}{
		p,
		title,
		userlist,
	}
	/*gp := &genPage{
		p,
		title,
	}*/
	renderTemplate(r.Context(), env, w, "admin_users.tmpl", data)

}

func (env *wikiEnv) adminUserHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "adminUserHandler")

	title := "admin-user"
	p := loadPage(env, r)

	userlist, err := env.authState.Userlist()
	if err != nil {
		panic(err)
	}

	//ctx := r.Context()
	params := getParams(r.Context())
	selectedUser := params["username"]

	data := struct {
		*page
		Title string
		Users []string
		User  string
	}{
		p,
		title,
		userlist,
		selectedUser,
	}
	/*gp := &genPage{
		p,
		title,
	}*/
	renderTemplate(r.Context(), env, w, "admin_user.tmpl", data)
}

// Function to take a <select><option> value and redirect to a URL based on it
func adminUserPostHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	selectedUser := r.FormValue("user")
	http.Redirect(w, r, "/admin/user/"+selectedUser, http.StatusSeeOther)
}

func (env *wikiEnv) adminMainHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "adminMainHandler")

	title := "admin-main"
	p := loadPage(env, r)

	gp := &genPage{
		p,
		title,
	}
	renderTemplate(r.Context(), env, w, "admin_main.tmpl", gp)
}

func (env *wikiEnv) adminConfigHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "adminConfigHandler")

	// To save config to toml:
	viperMap := viper.AllSettings()

	title := "admin-config"
	p := loadPage(env, r)

	data := struct {
		*page
		Title  string
		Config map[string]interface{}
	}{
		p,
		title,
		viperMap,
	}
	renderTemplate(r.Context(), env, w, "admin_config.tmpl", data)
}

func (env *wikiEnv) gitCheckinHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "gitCheckinHandler")

	title := "Git Checkin"
	p := loadPage(env, r)

	var s string

	if r.URL.Query().Get("file") != "" {
		file := r.URL.Query().Get("file")
		s = file
	} else {
		err := gitIsClean()
		s = err.Error()
		/*
			if err != nil && err != ErrGitDirty {
				panic(err)
			}
		*/
		//owithnewlines = bytes.Replace(o, []byte{0}, []byte(" <br>"), -1)
	}

	gp := &gitPage{
		p,
		title,
		s,
		viper.GetString("GitRepo"),
	}
	renderTemplate(r.Context(), env, w, "git_checkin.tmpl", gp)
}

func gitCheckinPostHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "gitCheckinPostHandler")

	var path string

	if r.URL.Query().Get("file") != "" {
		//file := r.URL.Query().Get("file")
		//log.Println(action)
		path = r.URL.Query().Get("file")
	} else {
		path = "."
	}

	err := gitAddFilepath(path)
	if err != nil {
		panic(err)
	}
	err = gitCommitEmpty()
	if err != nil {
		panic(err)
	}
	if path != "." {
		http.Redirect(w, r, "/"+path, http.StatusSeeOther)
	} else {
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}

}

func gitPushPostHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "gitPushPostHandler")

	err := gitPush()
	if err != nil {
		panic(err)
	}

	http.Redirect(w, r, r.Referer(), http.StatusSeeOther)

}

func gitPullPostHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "gitPullPostHandler")

	err := gitPull()
	if err != nil {
		panic(err)
	}

	http.Redirect(w, r, r.Referer(), http.StatusSeeOther)

}

func (env *wikiEnv) adminGitHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "adminGitHandler")

	title := "Git Management"
	p := loadPage(env, r)

	//var owithnewlines []byte

	err := gitIsClean()
	if err == nil {
		err = errors.New("Git repo is clean")
	}

	/*
		if err != nil && err != ErrGitDirty {
			panic(err)
		}

		owithnewlines = bytes.Replace(o, []byte{0}, []byte(" <br>"), -1)
	*/

	gp := &gitPage{
		p,
		title,
		err.Error(),
		viper.GetString("GitRepo"),
	}
	renderTemplate(r.Context(), env, w, "admin_git.tmpl", gp)
}

func (env *wikiEnv) tagMapHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "tagMapHandler")

	p := loadPage(env, r)

	tagpage := &tagMapPage{
		page:    p,
		TagKeys: env.cache.Tags,
	}
	renderTemplate(r.Context(), env, w, "tag_list.tmpl", tagpage)
}

func (env *wikiEnv) createWiki(w http.ResponseWriter, r *http.Request, name string) {
	//username, _ := auth.GetUsername(r.Context())
	//if username != "" {
	if auth.IsLoggedIn(r.Context()) {
		w.WriteHeader(404)
		//title := "Create " + name + "?"
		p := loadPage(env, r)

		/*gp := &genPage{
			p,
			name,
		}*/
		wp := &wikiPage{
			page: p,
			Wiki: &wiki{
				Title:    name,
				Filename: name,
				Frontmatter: &frontmatter{
					Title: name,
				},
			},
		}
		renderTemplate(r.Context(), env, w, "wiki_create.tmpl", wp)
		return
	}

	env.authState.SetFlash("Please login to view that page.", w, r)
	//h := viper.GetString("Domain")
	http.Redirect(w, r, "/login"+"?url="+r.URL.String(), http.StatusSeeOther)
	return

}

func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	// A very simple health check.
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")

	// In the future we could report back on the status of our DB, or our cache
	// (e.g. Redis) by performing a simple PING, and include them in the response.
	io.WriteString(w, `{"alive": true}`)
}

func isWikiPage(fullname string) bool {
	isIt := false
	// Detect filetype first
	file, err := os.Open(fullname)
	if err != nil {
		log.Println(err)
		isIt = false
	}
	buff := make([]byte, 512)
	_, err = file.Read(buff)
	if err != nil {
		log.Println(err)
		isIt = false
	}

	filetype := http.DetectContentType(buff)
	// Try and detect mis-detected wiki pages
	if filetype == "application/octet-stream" {
		/*
			if bytes.Equal(buff[:3], []byte("---")) {
				log.Println(fullname + " is a wiki page.")
				return true
			}
		*/
		isIt = true
	}
	if filetype == "text/plain; charset=utf-8" {
		isIt = true
	}

	log.Println(fullname + " is " + filetype)

	return isIt
}

/*
// wikiHandler wraps around all wiki page handlers
// Currently it retrieves the page name from params, checks for file existence, and checks for private pages
func wikiHandler(fn wHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Here we will extract the page title from the Request,
		// and call the provided handler 'fn'

		params := getParams(r.Context())
		name := params["name"]
		username, isAdmin := auth.GetUsername(r.Context())

		// Check if file exists before doing anything else
		name, feErr := checkName(name)
		fullname := filepath.Join(viper.GetString("WikiDir"), name)

		isWikiPage(fullname)

		if name != "" && feErr == errNoFile {
			//log.Println(r.URL.RequestURI())

			// If editing or saving, bypass create page
			if r.URL.RequestURI() == "/edit/"+name {
				fn(w, r, name)
				return
			}
			if r.URL.RequestURI() == "/save/"+name {
				fn(w, r, name)
				return
			}
			createWiki(w, r, name)
			return
		} else if feErr != nil {
			httpErrorHandler(w, r, feErr)
			return
		}

		// Detect filetypes

		//	filetype := checkFiletype(fullname)
		//	if filetype != "text/plain; charset=utf-8" {

		//		http.ServeFile(w, r, fullname)
		//	}


		// Read YAML frontmatter into fm
		// If err, just return, as file should not contain frontmatter
		f, err := os.Open(fullname)
		checkErr("wikiHandler()/Open", err)
		defer f.Close()

		fm, fmberr := readFront(f)
		checkErr("wikiHandler()/readFront", fmberr)

		// If user is logged in, check if wiki git repo is clean, then continue
		//if username != "" {
		if auth.IsLoggedIn(r.Context()) {
			err := gitIsClean()
			if err != nil {
				log.Println(err)
				auth.SetFlash(err.Error(), w, r)
				http.Redirect(w, r, "/admin/git", http.StatusSeeOther)
			}
			fn(w, r, name)
			return
		}

		// If this is a public page, just serve it
		if fm.Public {
			fn(w, r, name)
			return
		}
		// If this is an admin page, check if user is admin before serving
		if fm.Admin && !isAdmin {
			log.Println(username + " attempting to access restricted URL.")
			auth.SetFlash("Sorry, you are not allowed to see that.", w, r)
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		// If not logged in, mitigate, as the page is presumed private
		if !auth.IsLoggedIn(r.Context()) {
			rurl := r.URL.String()
			httputils.Debugln("wikiHandler mitigating: " + r.Host + rurl)
			//w.Write([]byte("OMG"))

			// Detect if we're in an endless loop, if so, just panic
			if strings.HasPrefix(rurl, "login?url=/login") {
				panic("AuthMiddle is in an endless redirect loop")
			}
			auth.SetFlash("Please login to view that page.", w, r)
			http.Redirect(w, r, "http://"+r.Host+"/login"+"?url="+rurl, http.StatusSeeOther)
			return
		}

	}
}
*/

func treeMuxWrapper(next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	}
}

// In combination with a footer.tmpl and associated code in renderTemplate(),
//  this middleware gives us a response time in the request.Context
func timer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		newTime := timeNewContext(r.Context(), time.Now())
		next.ServeHTTP(w, r.WithContext(newTime))
	})
}

func typeIcon(gitType string) template.HTML {
	var html template.HTML
	if gitType == "blob" {
		html = template.HTML(`<i class="fa fa-file-text-o" aria-hidden="true"></i>`)
	}
	if gitType == "tree" {
		html = template.HTML(`<i class="fa fa-folder-o" aria-hidden="true"></i>`)
	}
	return html
}

func tmplInit(env *wikiEnv) error {
	templatesDir := "./templates/"
	layouts, err := filepath.Glob(templatesDir + "layouts/*.tmpl")
	if err != nil {
		panic(err)
	}
	includes, err := filepath.Glob(templatesDir + "includes/*.tmpl")
	if err != nil {
		panic(err)
	}

	funcMap := template.FuncMap{"typeIcon": typeIcon, "prettyDate": httputils.PrettyDate, "safeHTML": httputils.SafeHTML, "imgClass": httputils.ImgClass, "isLoggedIn": isLoggedIn, "jsTags": jsTags}

	for _, layout := range layouts {
		files := append(includes, layout)
		//DEBUG TEMPLATE LOADING
		//httputils.Debugln(files)
		env.templates[filepath.Base(layout)] = template.Must(template.New("templates").Funcs(funcMap).ParseFiles(files...))
	}
	return nil
}

func initWikiDir() {
	//Check for wikiDir directory + git repo existence
	wikidir := viper.GetString("WikiDir")
	_, err := os.Stat(wikidir)
	if err != nil {
		log.Println(wikidir + " does not exist, creating it.")
		os.Mkdir(wikidir, 0755)
	}
	_, err = os.Stat(wikidir + ".git")
	if err != nil {
		log.Println(wikidir + " is not a git repo!")
		if viper.GetBool("InitWikiRepo") {
			log.Println("-init flag is given. Cloning " + viper.GetString("GitRepo") + " into " + wikidir + "...")
			err = gitClone(viper.GetString("GitRepo"))
			check(err)
		} else {
			repoNotExistErr := errors.New("clone/move your existing repo here, change the config, or run with -init to clone a specified remote repo")
			//log.Fatalln("Clone/move your existing repo here, change the config, or run with -init to clone a specified remote repo.")
			panic(repoNotExistErr)
		}
	}
}

// Simple function to get the httptreemux params, setting it blank if there aren't any
func getParams(c context.Context) map[string]string {
	/*
		params, ok := c.Value(httptreemux.ParamsContextKey).(map[string]string)
		if !ok {
			params = make(map[string]string)
		}
	*/

	return httptreemux.ContextParams(c)
}

func setPageTitle(frontmatterTitle, filename string) string {
	var name string
	if frontmatterTitle != "" {
		name = frontmatterTitle
	} else {
		name = filename
	}
	return name
}

func (env *wikiEnv) search(w http.ResponseWriter, r *http.Request) {
	params := getParams(r.Context())
	name := params["name"]

	// If this is a POST request, and searchwiki form is not blank,
	//  redirect to /search/$(searchform)
	if r.Method == "POST" {
		r.ParseForm()
		if r.PostFormValue("searchwiki") != "" {
			http.Redirect(w, r, "/search/"+r.PostFormValue("searchwiki"), http.StatusSeeOther)
			return
			//name = r.PostFormValue("searchwiki")
		}
	}

	p := loadPage(env, r)

	userLoggedIn := auth.IsLoggedIn(r.Context())
	_, isAdmin := auth.GetUsername(r.Context())

	var fileList string

	for _, v := range env.cache.Cache {
		if userLoggedIn {
			if v.Permission == "private" {
				//log.Println("priv", v.Filename)
				fileList = fileList + " " + `"` + v.Filename + `"`
			}
		}
		if isAdmin {
			if v.Permission == "private" {
				//log.Println("admin", v.Filename)
				fileList = fileList + " " + `"` + v.Filename + `"`
			}
		}
		if v.Permission == "public" {
			//log.Println("pubic", v.Filename)
			fileList = fileList + " " + `"` + v.Filename + `"`
		}
	}

	//log.Println(fileList)

	results := gitSearch(`'`+name+`'`, strings.TrimSpace(fileList))

	s := &searchPage{p, results}
	renderTemplate(r.Context(), env, w, "search_results.tmpl", s)
}

func buildCache() *wikiCache {
	defer httputils.TimeTrack(time.Now(), "buildCache")
	cache := new(wikiCache)
	if cache.Tags == nil {
		cache.Tags = make(map[string][]string)
	}
	if cache.Favs == nil {
		cache.Favs = make(map[string]struct{})
	}

	var wps []*gitDirList

	fileList, err := gitLsTree()
	check(err)
	for _, file := range fileList {

		// If using Git, build the full path:
		fullname := filepath.Join(viper.GetString("WikiDir"), file.Filename)

		// If this is a directory, add it to the list for listing
		//   but just assume it is private
		if file.Type == "tree" {
			var wp *gitDirList
			wp = &gitDirList{
				Type:       "tree",
				Filename:   file.Filename,
				CreateTime: 0,
				ModTime:    0,
				Permission: "private",
			}
			wps = append(wps, wp)
		}

		// If not a directory, get frontmatter from file and add to list
		if file.Type == "blob" {

			// If this is an absolute path, including the cfg.WikiDir, trim it
			//withoutdotslash := strings.TrimPrefix(viper.GetString("WikiDir"), "./")
			//fileURL := strings.TrimPrefix(file, withoutdotslash)

			var wp *gitDirList

			// Read YAML frontmatter into fm
			f, err := os.Open(fullname)
			check(err)

			fm := readFront(f)
			//fm, content := readWikiPage(f)
			f.Close()
			//checkErr("crawlWiki()/readFront", err)

			if fm.Title == "" {
				fm.Title = file.Filename
			}
			if fm.Permission == "" {
				fm.Permission = "private"
			}
			if fm.Favorite != true {
				fm.Favorite = false
			}
			if fm.Tags == nil {
				fm.Tags = []string{}
			}

			// Tags and Favorites building
			// Replacing readFavs and readTags
			if fm.Favorite {
				//log.Println(file + " is a favorite.")
				if _, ok := cache.Favs[file.Filename]; !ok {
					httputils.Debugln("crawlWiki: " + file.Filename + " is not already a favorite.")
					cache.Favs[file.Filename] = struct{}{}
				}
				//favbuf.WriteString(file + " ")
			}
			if fm.Tags != nil {
				for _, tag := range fm.Tags {
					if _, ok := cache.Tags[tag]; !ok {
						cache.Tags[tag] = append(cache.Tags[tag], file.Filename)
					}
				}
			}

			ctime, err := gitGetCtime(file.Filename)
			checkErr("crawlWiki()/gitGetCtime", err)
			mtime, err := gitGetMtime(file.Filename)
			checkErr("crawlWiki()/gitGetMtime", err)

			wp = &gitDirList{
				Type:       "blob",
				Filename:   file.Filename,
				CreateTime: ctime,
				ModTime:    mtime,
				Permission: fm.Permission,
			}
			wps = append(wps, wp)
		}
	}

	cache.Cache = wps

	cache.SHA1 = headHash()

	cacheFile, err := os.Create(viper.GetString("CacheLocation"))
	check(err)
	cacheEncoder := gob.NewEncoder(cacheFile)
	cacheEncoder.Encode(cache)
	cacheFile.Close()
	return cache
}

func loadCache() *wikiCache {
	cache := new(wikiCache)
	cacheFile, err := os.Open("./data/cache.gob")
	defer cacheFile.Close()
	if err == nil {
		log.Println("Loading cache from gob.")
		cacheDecoder := gob.NewDecoder(cacheFile)
		err = cacheDecoder.Decode(cache)
		//check(err)
		if err != nil {
			log.Println("Error loading cache. Rebuilding it.")
			cache = buildCache()
		}
		// Check the cached sha1 versus HEAD sha1, rebuild if they differ
		if cache.SHA1 != headHash() {
			log.Println("Cache SHA1s do not match. Rebuilding cache.")
			cache = buildCache()
		}
	}
	// If cache does not exist, build it
	if os.IsNotExist(err) {
		log.Println("Cache does not exist, building it.")
		cache = buildCache()
	}
	return cache
}

func headHash() string {
	repo, err := gogit.PlainOpen(viper.GetString("WikiDir"))
	check(err)
	head, err := repo.Head()
	check(err)
	return head.Hash().String()
}

// This should be all the stuff we need to be refreshed on startup and when pages are saved
func (env *wikiEnv) refreshStuff() {
	env.cache = loadCache()
}

func markdownPreview(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

	w.Write([]byte(markdownRender([]byte(r.FormValue("md")))))
}

func (env *wikiEnv) wikiMiddle(next http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params := getParams(r.Context())
		name := params["name"]
		_, isAdmin := auth.GetUsername(r.Context())
		userLoggedIn := auth.IsLoggedIn(r.Context())
		pageExists, relErr := checkName(&name)
		fullfilename := filepath.Join(viper.GetString("WikiDir"), name)
		//relErr := relativePathCheck(name)
		if relErr != nil {
			if relErr == errBaseNotDir {
				http.Error(w, "Cannot create subdir of a file.", 500)
				return
			}
			httpErrorHandler(w, r, relErr)
			return
		}
		nameCtx := newNameContext(r.Context(), name)
		//pageExists := doesPageExist(fullfilename)
		ctx := newWikiExistsContext(nameCtx, pageExists)

		// if pageExists, read its frontmatter
		if pageExists {
			// Read YAML frontmatter into fm
			// If err, just return, as file should not contain frontmatter
			f, err := os.Open(fullfilename)
			check(err)
			fm := readFront(f)
			f.Close()
			switch fm.Permission {
			case "admin":
				if userLoggedIn && isAdmin {
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
				if !isAdmin {
					rurl := r.URL.String()
					httputils.Debugln("wikiMiddle mitigating: " + r.Host + rurl)

					// Detect if we're in an endless loop, if so, just panic
					if strings.HasPrefix(rurl, "login?url=/login") {
						panic("AuthMiddle is in an endless redirect loop")
					}
					env.authState.SetFlash("Please login to see that.", w, r)
					http.Redirect(w, r.WithContext(ctx), "/login"+"?url="+rurl, http.StatusSeeOther)
					return
				}
			case "public":
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			case "private":
				if !userLoggedIn {
					rurl := r.URL.String()
					httputils.Debugln("wikiMiddle mitigating: " + r.Host + rurl)

					// Detect if we're in an endless loop, if so, just panic
					if strings.HasPrefix(rurl, "login?url=/login") {
						panic("AuthMiddle is in an endless redirect loop")
					}
					env.authState.SetFlash("Please login to see that.", w, r)
					http.Redirect(w, r.WithContext(ctx), "/login"+"?url="+rurl, http.StatusSeeOther)
					return
				}
				if userLoggedIn {
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}
		}

		if userLoggedIn {
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// If not logged in, mitigate, as the page is presumed private
		if !userLoggedIn {
			rurl := r.URL.String()
			httputils.Debugln("wikiMiddle mitigating: " + r.Host + rurl)

			// Detect if we're in an endless loop, if so, just panic
			if strings.HasPrefix(rurl, "login?url=/login") {
				panic("AuthMiddle is in an endless redirect loop")
			}
			env.authState.SetFlash("Please login to view that page.", w, r)
			http.Redirect(w, r.WithContext(ctx), "/login"+"?url="+rurl, http.StatusSeeOther)
			return
		}

		//next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (env *wikiEnv) setFavoriteHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "setFavoriteHandler")
	name := nameFromContext(r.Context())
	p := loadWikiPage(env, r, name)
	p.Wiki.Frontmatter.Favorite = true
	err := p.Wiki.save()
	if err != nil {
		log.Println(err)
	}
	env.authState.SetFlash(name+" has been favorited.", w, r)
	http.Redirect(w, r, "/"+name, http.StatusSeeOther)
	log.Println(name + " page favorited!")
}

func main() {

	// subscribe to SIGINT signals
	stopChan := make(chan os.Signal)
	signal.Notify(stopChan, os.Interrupt)
	mux := http.NewServeMux()

	/*
		f, err := os.Create("trace.out")
		if err != nil {
			panic(err)
		}
		defer f.Close()

		err = trace.Start(f)
		if err != nil {
			panic(err)
		}
		defer trace.Stop()
	*/

	dataDir, err := os.Stat("./data/")
	if err != nil {
		if os.IsNotExist(err) {
			err = os.Mkdir("data", 0755)
			if err != nil {
				log.Fatalln(err)
			}
		}
	}

	if !dataDir.IsDir() {
		log.Fatalln("./data/ is not a directory. This is where wiki data is stored.")
	}

	initWikiDir()

	// Bring up authState
	//var err error
	anAuthState, err := auth.NewAuthState("./data/auth.db", viper.GetString("AdminUser"))
	check(err)

	theCache := loadCache()

	env := &wikiEnv{
		authState: anAuthState,
		cache:     theCache,
		templates: make(map[string]*template.Template),
	}

	err = tmplInit(env)
	if err != nil {
		log.Fatalln(err)
	}

	// Check for unclean Git dir on startup
	err = gitIsCleanStartup()
	if err != nil {
		log.Fatalln("There was an issue with the git repo:", err)
	}

	csrfSecure := true
	if viper.GetBool("Dev") {
		csrfSecure = false
	}

	// HTTP stuff from here on out
	s := alice.New(timer, httputils.Logger, env.authState.UserEnvMiddle, csrf.Protect([]byte("c379bf3ac76ee306cf72270cf6c5a612e8351dcb"), csrf.Secure(csrfSecure)))

	//h := httptreemux.New()
	//h.PanicHandler = httptreemux.ShowErrorsPanicHandler
	r := httptreemux.NewContextMux()

	r.PanicHandler = errorHandler

	r.GET("/", indexHandler)

	r.GET("/tags", env.authState.AuthMiddle(env.tagMapHandler))

	r.GET("/new", env.authState.AuthMiddle(newHandler))
	r.GET("/login", env.loginPageHandler)
	r.GET("/logout", env.authState.LogoutHandler)
	//r.GET("/signup", signupPageHandler)
	r.GET("/list", env.listHandler)
	r.GET("/search/*name", env.search)
	r.POST("/search", env.search)
	r.GET("/recent", env.authState.AuthMiddle(env.recentHandler))
	r.GET("/health", healthCheckHandler)

	admin := r.NewContextGroup("/admin")
	admin.GET("/", env.authState.AuthAdminMiddle(env.adminMainHandler))
	admin.GET("/config", env.authState.AuthAdminMiddle(env.adminConfigHandler))
	//admin.POST("/config", env.authState.AuthAdminMiddle(adminConfigPostHandler))
	admin.GET("/git", env.authState.AuthAdminMiddle(env.adminGitHandler))
	admin.POST("/git/push", env.authState.AuthAdminMiddle(gitPushPostHandler))
	admin.POST("/git/checkin", env.authState.AuthAdminMiddle(gitCheckinPostHandler))
	admin.POST("/git/pull", env.authState.AuthAdminMiddle(gitPullPostHandler))
	admin.GET("/users", env.authState.AuthAdminMiddle(env.adminUsersHandler))
	admin.POST("/users", env.authState.AuthAdminMiddle(env.authState.UserSignupPostHandler))
	admin.POST("/user", env.authState.AuthAdminMiddle(adminUserPostHandler))
	admin.GET("/user/:username", env.authState.AuthAdminMiddle(env.adminUserHandler))
	admin.POST("/user/:username", env.authState.AuthAdminMiddle(env.adminUserHandler))
	admin.POST("/user/password_change", env.authState.AuthAdminMiddle(env.authState.AdminUserPassChangePostHandler))
	admin.POST("/user/delete", env.authState.AuthAdminMiddle(env.authState.AdminUserDeletePostHandler))

	a := r.NewContextGroup("/auth")
	a.POST("/login", env.authState.LoginPostHandler)
	a.POST("/logout", env.authState.LogoutHandler)
	a.GET("/logout", env.authState.LogoutHandler)
	//a.POST("/signup", auth.SignupPostHandler)

	r.POST("/gitadd", env.authState.AuthMiddle(gitCheckinPostHandler))
	r.GET("/gitadd", env.authState.AuthMiddle(env.gitCheckinHandler))

	r.GET("/md_render", markdownPreview)

	r.GET("/uploads/*", treeMuxWrapper(http.StripPrefix("/uploads/", http.FileServer(http.Dir("uploads")))))

	r.GET(`/fav/*name`, env.authState.AuthMiddle(env.wikiMiddle(env.setFavoriteHandler)))

	r.GET(`/edit/*name`, env.authState.AuthMiddle(env.wikiMiddle(env.editHandler)))
	r.POST(`/save/*name`, env.authState.AuthMiddle(env.wikiMiddle(env.saveHandler)))
	r.GET(`/history/*name`, env.authState.AuthMiddle(env.wikiMiddle(env.historyHandler)))
	//r.GET(`/new/*name`, auth.AuthMiddle(newHandler))
	r.GET(`/*name`, env.wikiMiddle(env.viewHandler))

	mux.Handle("/debug/pprof/", env.authState.AuthAdminMiddle(http.HandlerFunc(pprof.Index)))
	mux.Handle("/debug/pprof/cmdline", env.authState.AuthAdminMiddle(http.HandlerFunc(pprof.Cmdline)))
	mux.Handle("/debug/pprof/profile", env.authState.AuthAdminMiddle(http.HandlerFunc(pprof.Profile)))
	mux.Handle("/debug/pprof/symbol", env.authState.AuthAdminMiddle(http.HandlerFunc(pprof.Symbol)))
	mux.Handle("/debug/pprof/trace", env.authState.AuthAdminMiddle(http.HandlerFunc(pprof.Trace)))

	mux.HandleFunc("/robots.txt", httputils.Robots)
	mux.HandleFunc("/favicon.ico", httputils.FaviconICO)
	mux.HandleFunc("/favicon.png", httputils.FaviconPNG)
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir("./assets"))))

	mux.Handle("/", s.Then(r))

	log.Println("Listening on port " + viper.GetString("Port"))

	srv := &http.Server{
		Addr:    "0.0.0.0:" + viper.GetString("Port"),
		Handler: mux,
	}

	go func() {
		// service connections
		if err := srv.ListenAndServe(); err != nil {
			log.Printf("listen: %s\n", err)
		}
	}()

	<-stopChan // wait for SIGINT
	log.Println("Shutting down server...")

	// shut down gracefully, but wait no longer than 5 seconds before halting
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)

	log.Println("Server gracefully stopped")

}
