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
	"expvar"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
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
	"sync"
	"time"

	fuzzy2 "github.com/renstrom/fuzzysearch/fuzzy"

	"github.com/spf13/pflag"
	yaml "gopkg.in/yaml.v2"

	"github.com/dimfeld/httptreemux"
	"github.com/gorilla/csrf"
	"github.com/justinas/alice"
	"github.com/oxtoacart/bpool"
	"github.com/russross/blackfriday"
	//"github.com/microcosm-cc/bluemonday"

	"github.com/spf13/viper"
	_ "github.com/tevjef/go-runtime-metrics/expvar"

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

	timerKey       key = 0
	wikiNameKey    key = 1
	wikiExistsKey  key = 2
	wikiKey        key = 3
	yamlSeparator      = "---"
	yamlSeparator2     = "..."

	adminPermission   = "admin"
	privatePermission = "private"
	publicPermission  = "public"

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
	dataDir string
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
	errIsDir       = errors.New("file is a directory")
)

type renderer struct {
	*blackfriday.Html
}

//Base struct, page ; has to be wrapped in a data {} strut for consistency reasons
type page struct {
	SiteName  string
	Favs      []string
	UserInfo  userInfo
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
	//Public     bool     `yaml:"public,omitempty"`
	//Admin      bool     `yaml:"admin,omitempty"`
}

type wiki struct {
	Title       string
	Filename    string
	Frontmatter frontmatter
	Content     []byte
	CreateTime  int64
	ModTime     int64
}

type genPage struct {
	page
	Title string
}

type gitPage struct {
	page
	Title     string
	GitStatus string
	GitRemote string
}

type gitDirList struct {
	Type       string
	Filename   string
	CreateTime int64
	ModTime    int64
	Permission string
}

// Env wrapper to hold app-specific configs, to pass to handlers
// cache is a pointer here since it's pretty large itself, and holds a mutex
type wikiEnv struct {
	authState auth.State
	cache     *wikiCache
	templates map[string]*template.Template
	mutex     sync.Mutex
	favs
	tags
}

type wikiCache struct {
	SHA1  string
	Cache []gitDirList
	Tags  map[string][]string
	Favs  map[string]struct{}
}

type favs struct {
	sync.RWMutex
	List map[string]struct{}
}

type tags struct {
	sync.RWMutex
	List map[string][]string
}

// Sorting functions
type wikiByDate []wiki

func (a wikiByDate) Len() int           { return len(a) }
func (a wikiByDate) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a wikiByDate) Less(i, j int) bool { return a[i].CreateTime < a[j].CreateTime }

type wikiByModDate []wiki

func (a wikiByModDate) Len() int           { return len(a) }
func (a wikiByModDate) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a wikiByModDate) Less(i, j int) bool { return a[i].ModTime < a[j].ModTime }

func init() {
	var err error
	gitPath, err = exec.LookPath("git")
	if err != nil {
		log.Fatalln("git must be installed")
	}

	pflag.StringVar(&dataDir, "DataDir", "./data/", "Path to store permanent data in.")
	pflag.Bool("InitWikiRepo", false, "Initialize the wiki directory")
	pflag.Bool("Debug", false, "Turn on debug logging")
	pflag.Parse()

	viper.BindPFlags(pflag.CommandLine)

	// Viper config.
	viper.SetDefault("DataDir", "./data/")
	viper.SetDefault("Port", "5000")
	viper.SetDefault("Email", "unused@the.moment")
	viper.SetDefault("Domain", "wiki.example.com")
	viper.SetDefault("RemoteGitRepo", "")
	viper.SetDefault("AdminUser", "admin")
	viper.SetDefault("PushOnSave", false)
	viper.SetDefault("InitWikiRepo", false)
	viper.SetDefault("Dev", false)
	viper.SetDefault("Debug", false)
	viper.SetDefault("CacheEnabled", true)
	viper.SetEnvPrefix("gowiki")
	viper.AutomaticEnv()

	viper.SetConfigName("conf")
	viper.AddConfigPath(viper.GetString("DataDir"))
	err = viper.ReadInConfig() // Find and read the config file
	if err != nil {            // Handle errors reading the config file
		//panic(fmt.Errorf("Fatal error config file: %s \n", err))
		log.Println("No configuration file loaded - using defaults")
	}

	if viper.GetBool("Debug") {
		httputils.Debug = true
	}
	// Setting these last; they do not need to be set manually:

	//viper.SetDefault("WikiDir", filepath.Join(dataDir, "wikidata"))
	//viper.SetDefault("CacheLocation", filepath.Join(dataDir, "cache.gob"))
	//viper.SetDefault("AuthLocation", filepath.Join(dataDir, "auth.db"))
	//viper.SetDefault("InitWikiRepo", *initFlag)

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

// Task List support, replacing checkboxs with an SVG for more visibility
func (r *renderer) ListItem(out *bytes.Buffer, text []byte, flags int) {
	switch {
	case bytes.HasPrefix(text, []byte("[ ] ")):

		text = append(svgByte("checkbox-unchecked"), text[3:]...)
	case bytes.HasPrefix(text, []byte("[x] ")) || bytes.HasPrefix(text, []byte("[X] ")):
		text = append(svgByte("checkbox-checked"), text[3:]...)
	}
	r.Html.ListItem(out, text, flags)
}

// Inter-wiki linking, [PageName]() and [/PageName]()
func (r *renderer) NormalText(out *bytes.Buffer, text []byte) {
	linkPattern := regexp.MustCompile(`\[(?:\/|)(?P<Name>[0-9a-zA-Z-_\.\/]+)\]\(\)`)

	switch {
	case linkPattern.Match(text):
		//joinedText := path.Join(viper.GetString("Domain"), string(text))
		//domain := "//" + viper.GetString("Domain")
		link := linkPattern.ReplaceAll(text, []byte(path.Join("/", "$1")))
		title := linkPattern.ReplaceAll(text, []byte("$1"))
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

func newWikiContext(c context.Context, w *wiki) context.Context {
	return context.WithValue(c, wikiKey, w)
}

func wikiFromContext(c context.Context) *wiki {
	w, ok := c.Value(wikiKey).(*wiki)
	if !ok {
		httputils.Debugln("No wiki in context.")
		return &wiki{}
	}
	return w
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
	c.Dir = filepath.Join(dataDir, "wikidata")
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
		if viper.GetBool("PushOnSave") {
			httputils.Debugln("gitIsCleanStartup: Pushing git repo...")
			return gitPush()
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
func gitAddFilepath(filepath string) error {
	o, err := gitCommand("add", filepath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error during `git add`: %s\n%s", err.Error(), string(o))
	}
	return nil
}

// Execute `git rm {filepath}` in workingDirectory
func gitRmFilepath(filepath string) error {
	o, err := gitCommand("rm", filepath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error during `git rm`: %s\n%s", err.Error(), string(o))
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
	check(err)

	return ctime, err
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
	check(err)

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
func gitGetFileLog(filename string) ([]commitLog, error) {
	o, err := gitCommand("log", "--pretty=format:%H,%at,%s", filename).Output()
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
	check(err)

	return mtime, err
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
func gitGetTimes(filename string, ctime, mtime chan<- int64) {
	defer httputils.TimeTrack(time.Now(), "gitGetTimes")

	go func() {
		co, err := gitCommand("log", "--diff-filter=A", "--follow", "--format=%at", "-1", "--", filename).Output()
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
		check(err)
		if err != nil {
			ctime <- 0
			return
		}
		ctime <- ctimeI
	}()

	go func() {
		mo, err := gitCommand("log", "--format=%at", "-1", "--", filename).Output()
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
		check(err)
		if err != nil {
			mtime <- 0
			return
		}
		mtime <- mtimeI
	}()

}

func gitIsEmpty() bool {
	// Run git rev-parse HEAD on the repo
	// If it errors out, should mean it's empty
	err := gitCommand("rev-parse", "HEAD").Run()
	if err != nil {
		return true
	}
	return false
}

// Search results, via git
// git grep --break 'searchTerm'
func gitSearch(searchTerm, fileSpec string) []result {
	var results []result
	quotedSearchTerm := `'` + searchTerm + `'`
	cmd := exec.Command("/bin/sh", "-c", gitPath+" grep "+quotedSearchTerm+" -- "+fileSpec)
	//o := gitCommand("grep", "omg -- 'index'")
	cmd.Dir = filepath.Join(dataDir, "wikidata")
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

func (f *favs) LoadOrStore(s string) {
	f.Lock()
	if _, ok := f.List[s]; !ok {
		httputils.Debugln("favs.LoadOrStore: " + s + " is not already a favorite.")
		f.List[s] = struct{}{}
	}
	f.Unlock()
}

func (f *favs) GetAll() (sfavs []string) {
	f.RLock()
	for k := range f.List {
		sfavs = append(sfavs, k)
	}
	f.RUnlock()
	sort.Strings(sfavs)
	return sfavs
}

func newFavsMap() favs {
	return favs{
		List: make(map[string]struct{}),
	}
}

func newTagsMap() tags {
	return tags{
		List: make(map[string][]string),
	}
}

func (t *tags) LoadOrStore(tags []string, filename string) {
	t.Lock()
	for _, tag := range tags {
		t.List[tag] = append(t.List[tag], filename)
	}
	t.Unlock()
}

func (t *tags) GetOne(tag string) (pages []string) {
	t.RLock()
	pages = t.List[tag]
	t.RUnlock()
	sort.Strings(pages)
	return pages
}

func (t *tags) GetAll() (tMap map[string][]string) {
	t.RLock()
	tMap = t.List
	t.RUnlock()
	return tMap
}

func loadPage(env *wikiEnv, r *http.Request, p chan<- page) {
	defer httputils.TimeTrack(time.Now(), "loadPage")
	//timer.Step("loadpageFunc")

	// Auth lib middlewares should load the user and tokens into context for reading
	user := auth.GetUserState(r.Context())
	msg := auth.GetFlash(r.Context())
	//token := auth.GetToken(r.Context())
	token := csrf.TemplateField(r)

	var message template.HTML
	if msg != "" {
		message = template.HTML(`
			<div class="notification anim active" id="notification">
			<p>` + msg + `
			<button class="close-button" type="button" onclick="notif()">
			<div class="svg-icon"><svg version="1.1" xmlns="http://www.w3.org/2000/svg" width="32" height="32" viewBox="0 0 32 32">
			<title>cross</title>
			<path d="M31.708 25.708c-0-0-0-0-0-0l-9.708-9.708 9.708-9.708c0-0 0-0 0-0 0.105-0.105 0.18-0.227 0.229-0.357 0.133-0.356 0.057-0.771-0.229-1.057l-4.586-4.586c-0.286-0.286-0.702-0.361-1.057-0.229-0.13 0.048-0.252 0.124-0.357 0.228 0 0-0 0-0 0l-9.708 9.708-9.708-9.708c-0-0-0-0-0-0-0.105-0.104-0.227-0.18-0.357-0.228-0.356-0.133-0.771-0.057-1.057 0.229l-4.586 4.586c-0.286 0.286-0.361 0.702-0.229 1.057 0.049 0.13 0.124 0.252 0.229 0.357 0 0 0 0 0 0l9.708 9.708-9.708 9.708c-0 0-0 0-0 0-0.104 0.105-0.18 0.227-0.229 0.357-0.133 0.355-0.057 0.771 0.229 1.057l4.586 4.586c0.286 0.286 0.702 0.361 1.057 0.229 0.13-0.049 0.252-0.124 0.357-0.229 0-0 0-0 0-0l9.708-9.708 9.708 9.708c0 0 0 0 0 0 0.105 0.105 0.227 0.18 0.357 0.229 0.356 0.133 0.771 0.057 1.057-0.229l4.586-4.586c0.286-0.286 0.362-0.702 0.229-1.057-0.049-0.13-0.124-0.252-0.229-0.357z"></path>
			</svg></div>
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

	p <- page{
		SiteName: "GoWiki",
		Favs:     env.favs.GetAll(),
		UserInfo: userInfo{
			Username:   user.GetName(),
			IsAdmin:    user.IsAdmin(),
			IsLoggedIn: user.IsLoggedIn(),
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
		return template.HTML(`Git repo is clean.`)
	}
}

type historyPage struct {
	page
	Wiki        wiki
	Filename    string
	FileHistory []commitLog
}

func (env *wikiEnv) historyHandler(w http.ResponseWriter, r *http.Request) {
	name := nameFromContext(r.Context())

	if !wikiExistsFromContext(r.Context()) {
		http.Redirect(w, r, "/"+name, http.StatusFound)
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
type commitPage struct {
	page
	Wiki     wiki
	Commit   string
	Rendered string
	Diff     string
}

func (env *wikiEnv) viewCommitHandler(w http.ResponseWriter, r *http.Request, commit, name string) {
	var fm frontmatter
	var pageContent string

	//commit := vars["commit"]

	p := make(chan page, 1)
	go loadPage(env, r, p)

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
	check(err)
	if err != nil {
		panic(err)
	}

	// Render remaining content after frontmatter
	md := markdownRender(content)
	//md := commonmarkRender(content)

	pagetitle := setPageTitle(fm.Title, name)

	diffstring := string(diff)

	pageContent = md

	cp := &commitPage{
		page: <-p,
		Wiki: wiki{
			Title:       pagetitle,
			Filename:    name,
			Frontmatter: fm,
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

type recent struct {
	Date      int64
	Commit    string
	Filenames []string
}

type recentsPage struct {
	page
	Recents []recent
}

// TODO: Fix this
func (env *wikiEnv) recentHandler(w http.ResponseWriter, r *http.Request) {

	p := make(chan page, 1)
	go loadPage(env, r, p)

	gh, err := gitHistory()
	check(err)

	/*
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(200)
	*/
	var split []string
	var split2 []string
	var recents []recent

	for _, v := range gh {
		split = strings.Split(strings.TrimSpace(v), " ")
		date, err := strconv.ParseInt(split[0], 0, 64)
		if err != nil {
			panic(err)
		}

		// If there is a filename (initial one will not have it)...
		split2 = strings.Split(split[1], "\n")
		if len(split2) >= 2 {

			r := recent{
				Date:      date,
				Commit:    split2[0],
				Filenames: strings.Split(split2[1], "\n"),
			}
			//w.Write([]byte(v + "<br>"))
			recents = append(recents, r)
		}
	}

	s := recentsPage{
		page:    <-p,
		Recents: recents,
	}
	renderTemplate(r.Context(), env, w, "recents.tmpl", s)

}

type listPage struct {
	page
	Wikis []gitDirList
}

func (env *wikiEnv) listHandler(w http.ResponseWriter, r *http.Request) {

	p := make(chan page, 1)
	go loadPage(env, r, p)

	var list []gitDirList

	user := auth.GetUserState(r.Context())

	for _, v := range env.cache.Cache {
		if v.Permission == publicPermission {
			//log.Println("pubic", v.Filename)
			list = append(list, v)
		}
		if user.IsLoggedIn() {
			if v.Permission == privatePermission {
				//log.Println("priv", v.Filename)
				list = append(list, v)
			}
		}
		if user.IsAdmin() {
			if v.Permission == adminPermission {
				//log.Println("admin", v.Filename)
				list = append(list, v)
			}
		}
	}

	l := listPage{
		page:  <-p,
		Wikis: list,
	}
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
	topbuf := new(bytes.Buffer)
	bottombuf := new(bytes.Buffer)
	scanWikiPage(reader, topbuf, bottombuf)

	return marshalFrontmatter(topbuf.Bytes()), bottombuf.Bytes()
}

func readFront(reader io.Reader) frontmatter {
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
	startTokenFound := false
	endTokenFound := false
	for scanner.Scan() {

		if startTokenFound && endTokenFound {
			if grabPage {
				_, err := bufs[1].WriteString(scanner.Text() + "\n")
				if err != nil {
					log.Println("Error writing page data:", err)
				}
			} else {
				break
			}
		}
		if startTokenFound && !endTokenFound {
			// Anything after the --- tag, add to the topbuffer
			if scanner.Text() != yamlSeparator || scanner.Text() != yamlSeparator2 {
				_, err := bufs[0].WriteString(scanner.Text() + "\n")
				if err != nil {
					log.Println("Error writing page data:", err)
				}
			}
			if scanner.Text() == yamlSeparator || scanner.Text() == yamlSeparator2 {
				endTokenFound = true
				// If not given 2 buffers, end here.
				if !grabPage {
					break
				}
			}
		}

		// Hopefully catch the first --- tag
		if !startTokenFound && !endTokenFound {
			if scanner.Text() == yamlSeparator {
				startTokenFound = true
			} else {
				startTokenFound = true
				endTokenFound = true
				// If given two buffers, but we cannot find the beginning, assume the entire page is text
				if grabPage {
					_, err := bufs[1].WriteString(scanner.Text() + "\n")
					if err != nil {
						log.Println("Error writing page data:", err)
					}

				}
			}
		}
	}
}

func scanWikiPageB(reader io.Reader, bufs ...*bytes.Buffer) {
	grabPage := false
	// If we are given two buffers, do something with the bottom data
	if len(bufs) == 2 {
		grabPage = true
	}
	scanner := bufio.NewScanner(reader)
	startTokenFound := false
	endTokenFound := false
	for scanner.Scan() {

		if startTokenFound && endTokenFound {
			if grabPage {
				_, err := bufs[1].Write(scanner.Bytes())
				if err != nil {
					log.Println("Error writing page data:", err)
				}
				err = bufs[1].WriteByte('\n')
				if err != nil {
					log.Println("Error writing page data:", err)
				}
			} else {
				break
			}
		}
		if startTokenFound && !endTokenFound {
			// Anything after the --- tag, add to the topbuffer
			if !bytes.Equal(scanner.Bytes(), []byte(yamlSeparator)) || !bytes.Equal(scanner.Bytes(), []byte(yamlSeparator2)) {
				_, err := bufs[0].Write(scanner.Bytes())
				if err != nil {
					log.Println("Error writing page data:", err)
				}
				err = bufs[0].WriteByte('\n')
				if err != nil {
					log.Println("Error writing page data:", err)
				}
			}
			if bytes.Equal(scanner.Bytes(), []byte(yamlSeparator)) || bytes.Equal(scanner.Bytes(), []byte(yamlSeparator2)) {
				endTokenFound = true
				// If not given 2 buffers, end here.
				if !grabPage {
					break
				}
			}
		}

		// Hopefully catch the first --- tag
		if !startTokenFound && !endTokenFound {
			if bytes.Equal(scanner.Bytes(), []byte(yamlSeparator)) {
				startTokenFound = true
			} else {
				startTokenFound = true
				endTokenFound = true
				// If given two buffers, but we cannot find the beginning, assume the entire page is text
				if grabPage {
					_, err := bufs[1].Write(scanner.Bytes())
					if err != nil {
						log.Println("Error writing page data:", err)
					}
					err = bufs[1].WriteByte('\n')
					if err != nil {
						log.Println("Error writing page data:", err)
					}
				}
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
			// Check for deprecated individual admin/private/public tags
			private, privfound := m[privatePermission].(bool)
			if privfound && private {
				fm.Permission = privatePermission
			}
			public, pubfound := m[publicPermission].(bool)
			if pubfound && public {
				fm.Permission = publicPermission
			}
			admin, adminfound := m[adminPermission].(bool)
			if adminfound && admin {
				fm.Permission = adminPermission
			}
			permission, permfound := m["permission"].(string)
			if permfound {
				fm.Permission = permission
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
	err = tmpl.ExecuteTemplate(buf, "footer", httputils.GetRenderTime(c))
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

// doesPageExist checks if the given name exists, and is a regular file
// If there is anything wrong, it panics
func doesPageExist(name string) (bool, error) {
	defer httputils.TimeTrack(time.Now(), "doesPageExist")

	var exists bool
	var finError error

	fileInfo, err := os.Stat(name)
	if err == nil {
		fileMode := fileInfo.Mode()
		if fileMode.IsRegular() {
			exists = true
		}
		if fileMode.IsDir() {
			exists = false
			finError = errIsDir
		}
	}

	if err != nil {
		if os.IsNotExist(err) {
			exists = false
		} else {
			log.Println("doesPageExist, unhandled error:", err)
			exists = false
			finError = err
		}
	}

	httputils.Debugln("doesPageExist", name, exists)

	return exists, finError
}

// Check that the given full path is relative to the configured wikidir
func relativePathCheck(name string) error {
	defer httputils.TimeTrack(time.Now(), "relativePathCheck")
	wikiDir := filepath.Join(dataDir, "wikidata")
	fullfilename := filepath.Join(wikiDir, name)
	dir, _ := filepath.Split(name)
	if dir != "" {
		dirErr := checkDir(dir)
		if dirErr != nil {
			return dirErr
		}
	}

	_, err := filepath.Rel(wikiDir, fullfilename)
	return err
}

// This does various checks to see if an existing page exists or not
// Also checks for and returns an error on some edge cases
// So we only proceed if this returns false AND nil
// Edge cases checked for currently:
// - If name is trying to escape or otherwise a bad path
// - If name is a /directory/file combo, but /directory is actually a file
// - If name contains a .git entry, error out
func checkName(name *string) (bool, error) {
	defer httputils.TimeTrack(time.Now(), "checkName")

	separators := regexp.MustCompile(`[ &_=+:]`)
	dashes := regexp.MustCompile(`[\-]+`)

	// Rely on httptreemux's Clean function to clean up ../ and other potential path-escaping sequences;
	//  stripping off the / so we can pass it along to git
	*name = httptreemux.Clean(*name)
	if strings.HasPrefix(*name, "/") {
		*name = strings.TrimPrefix(*name, "/")
	}
	// Remove trailing spaces
	*name = strings.Trim(*name, " ")

	// Security check; ensure we are not serving any files from wikidata/.git
	// If so, toss them to the index, no hints given
	if strings.Contains(*name, ".git") {
		return false, errors.New("Unable to access given file")
	}

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

	fullfilename := filepath.Join(dataDir, "wikidata", *name)

	exists, err := doesPageExist(fullfilename)
	if err == errIsDir {
		return false, errIsDir
	}

	// If name doesn't exist, and there is no file extension given, try .page and then .md
	if !exists {
		possbileExts := []string{".md", ".page"}
		for _, ext := range possbileExts {
			if !exists && (filepath.Ext(*name) == "") {
				existsWithExt, _ := doesPageExist(fullfilename + ext)
				if existsWithExt {
					*name = *name + ext
					httputils.Debugln(*name + " found!")
					exists = true
					break
				}
			}
		}
	}

	// If original filename does not exist, normalize the filename, and check if that exists
	if !exists {
		// Normalize the name if the original name doesn't exist
		normalName := strings.ToLower(*name)
		normalName = separators.ReplaceAllString(normalName, "-")
		normalName = dashes.ReplaceAllString(normalName, "-")
		fullnewfilename := filepath.Join(dataDir, "wikidata", normalName)
		// Only check for the existence of the normalized name if anything changed
		if normalName != *name {
			exists, err = doesPageExist(fullnewfilename)
			if err == errIsDir {
				return false, errIsDir
			}
			*name = normalName
		}
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
		fullpath := filepath.Join(dataDir, "wikidata", relpath)
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
	}
	return err
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "indexHandler")

	http.Redirect(w, r, "/index", http.StatusFound)
	//viewHandler(w, r, "index")
}

func (env *wikiEnv) viewHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "viewHandler")

	name := nameFromContext(r.Context())
	/*
		nameStat, err := os.Stat(filepath.Join(dataDir, "wikidata", name))
		if err != nil {
			log.Println("viewHandler error reading", name, err)
		}
		if err == nil {
			if nameStat.IsDir() {
				// Check if name/index exists, and if it does, serve it
				_, err := os.Stat(filepath.Join(dataDir, "wikidata", name, "index"))
				if err == nil {
					http.Redirect(w, r, "/"+filepath.Join(name, "index"), http.StatusFound)
					return
				}
				if os.IsNotExist(err) {
					// TODO: List directory
					log.Println("TODO: List directory")
				}
			}
		}
	*/

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

	if !isWiki(name) {
		http.ServeFile(w, r, filepath.Join(dataDir, "wikidata", name))
		return
	}

	httputils.Debugln("Yay proper wiki page!")
	// Get Wiki
	p := loadWikiPage(env, r, name)

	// Build a list of filenames to be fed to closestmatch, for similarity matching
	var filelist []string
	user := auth.GetUserState(r.Context())
	for _, v := range env.cache.Cache {
		if v.Permission == publicPermission {
			//log.Println("pubic", v.Filename)
			filelist = append(filelist, v.Filename)
		}
		if user.IsLoggedIn() {
			if v.Permission == privatePermission {
				//log.Println("priv", v.Filename)
				filelist = append(filelist, v.Filename)
			}
		}
		if user.IsAdmin() {
			if v.Permission == adminPermission {
				//log.Println("admin", v.Filename)
				filelist = append(filelist, v.Filename)
			}
		}
	}
	// Check for similar filenames
	/*
		var similarPages []string
		for _, match := range fuzzy.Find(name, filelist) {
			similarPages = append(similarPages, match.Str)
		}
	*/

	similarPages := fuzzy2.FindFold(name, filelist)
	p.SimilarPages = similarPages

	renderTemplate(r.Context(), env, w, "wiki_view.tmpl", p)
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

func isWiki(filename string) bool {
	var isWiki bool
	file, err := os.Open(filepath.Join(dataDir, "wikidata", filename))
	if err != nil {
		log.Println(err)
		isWiki = false
	}

	defer file.Close()
	buff := make([]byte, 512)
	_, err = file.Read(buff)
	if err != nil {
		log.Println(err)
		isWiki = false
	}
	filetype := http.DetectContentType(buff)
	if filetype == "application/octet-stream" {
		// Definitely wiki page...but others probably
		if string(buff[:3]) == "---" {
			isWiki = true
		}

		// Account for .page files from gitit
		if filepath.Ext(filename) == ".page" || filepath.Ext(filename) == ".md" {
			isWiki = true
		}
		// TODO Fixes gitit-created files, until I can figure out a better way
		//realFileType = "wiki"
	} else if filetype == "text/plain; charset=utf-8" {
		isWiki = true
	} else {
		isWiki = false
	}
	return isWiki
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

	err := r.ParseForm()
	if err != nil {
		log.Println("Error parsing form ", err)
	}
	content := r.FormValue("editor")

	// Strip out CRLF here,
	// as I cannot figure out if it's the browser or what inserting them...
	if strings.Contains(content, "\r\n") {
		log.Println("crlf detected in saveHandler; replacing with just newlines.")
		content = strings.Replace(content, "\r\n", "\n", -1)
		//log.Println(strings.Contains(content, "\r\n"))
	}

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

	fm := frontmatter{
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

	err = thewiki.save(&env.mutex)
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

	env.authState.SetFlash("Wiki page successfully saved.", w)
	http.Redirect(w, r, "/"+name, http.StatusFound)
	log.Println(name + " page saved!")
}

func urlFromPath(path string) string {
	wikiDir := filepath.Join(dataDir, "wikidata")
	url := filepath.Clean(wikiDir) + "/"
	return strings.TrimPrefix(path, url)
}

func loadWiki(name string, w chan<- wiki) {
	defer httputils.TimeTrack(time.Now(), "loadWiki")

	var fm frontmatter

	wikiDir := filepath.Join(dataDir, "wikidata")

	ctime := make(chan int64, 1)
	mtime := make(chan int64, 1)
	go gitGetTimes(name, ctime, mtime)

	fm, content := readFileAndFront(filepath.Join(wikiDir, name))

	pagetitle := setPageTitle(fm.Title, name)

	/*
		ctime, err := gitGetCtime(name)
		checkErr("loadWiki()/gitGetCtime", err)

		mtime, err := gitGetMtime(name)
		checkErr("loadWiki()/gitGetMtime", err)
	*/

	w <- wiki{
		Title:       pagetitle,
		Filename:    name,
		Frontmatter: fm,
		Content:     content,
		CreateTime:  <-ctime,
		ModTime:     <-mtime,
	}

}

type wikiPage struct {
	page
	Wiki         wiki
	Rendered     string
	SimilarPages []string
}

func loadWikiPage(env *wikiEnv, r *http.Request, name string) wikiPage {
	defer httputils.TimeTrack(time.Now(), "loadWikiPage")

	var theWiki wiki
	var md string

	p := make(chan page, 1)
	go loadPage(env, r, p)

	wikiExists := wikiExistsFromContext(r.Context())
	if !wikiExists {
		theWiki = wiki{
			Title:    name,
			Filename: name,
			Frontmatter: frontmatter{
				Title: name,
			},
			CreateTime: 0,
			ModTime:    0,
		}
	}
	if wikiExists {
		wc := make(chan wiki, 1)
		go loadWiki(name, wc)
		theWiki = <-wc
		md = markdownRender(theWiki.Content)
	}

	//md := commonmarkRender(wikip.Content)
	//markdownRender2(wikip.Content)

	wp := wikiPage{
		page:     <-p,
		Wiki:     theWiki,
		Rendered: md,
	}
	return wp
}

func (wiki *wiki) save(mutex *sync.Mutex) error {
	mutex.Lock()
	defer httputils.TimeTrack(time.Now(), "wiki.save()")

	dir, filename := filepath.Split(wiki.Filename)
	wikiDir := filepath.Join(dataDir, "wikidata")
	fullfilename := filepath.Join(wikiDir, dir, filename)

	// If directory doesn't exist, create it
	// - Check if dir is null first
	if dir != "" {
		dirpath := filepath.Join(wikiDir, dir)
		if _, err := os.Stat(dirpath); os.IsNotExist(err) {
			err := os.MkdirAll(dirpath, 0755)
			if err != nil {
				mutex.Unlock()
				return err
			}
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
	if err != nil {
		mutex.Unlock()
		return err
	}

	//buffer := new(bytes.Buffer)
	wb := bufio.NewWriter(f)

	_, err = wb.WriteString("---\n")
	if err != nil {
		mutex.Unlock()
		return err
	}

	yamlBuffer, err := yaml.Marshal(wiki.Frontmatter)
	if err != nil {
		mutex.Unlock()
		return err
	}

	_, err = wb.Write(yamlBuffer)
	if err != nil {
		mutex.Unlock()
		return err
	}

	_, err = wb.WriteString("---\n")
	if err != nil {
		mutex.Unlock()
		return err
	}

	_, err = wb.Write(wiki.Content)
	if err != nil {
		mutex.Unlock()
		return err
	}

	err = wb.Flush()
	if err != nil {
		mutex.Unlock()
		return err
	}

	err = f.Close()
	if err != nil {
		mutex.Unlock()
		return err
	}

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
	if err != nil {
		mutex.Unlock()
		return err
	}

	// FIXME: add a message box to edit page, check for it here
	err = gitCommitEmpty()
	if err != nil {
		mutex.Unlock()
		return err
	}

	log.Println(fullfilename + " has been saved.")
	mutex.Unlock()
	return err

}

func (env *wikiEnv) loginPageHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "loginPageHandler")

	title := "login"
	p := make(chan page, 1)
	go loadPage(env, r, p)

	gp := &genPage{
		<-p,
		title,
	}
	renderTemplate(r.Context(), env, w, "login.tmpl", gp)
}

func (env *wikiEnv) signupPageHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "signupPageHandler")

	title := "signup"
	p := make(chan page, 1)
	go loadPage(env, r, p)

	gp := &genPage{
		<-p,
		title,
	}
	renderTemplate(r.Context(), env, w, "signup.tmpl", gp)
}

func (env *wikiEnv) adminUsersHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "adminUsersHandler")

	title := "admin-users"
	p := make(chan page, 1)
	go loadPage(env, r, p)

	userlist, err := env.authState.Userlist()
	if err != nil {
		panic(err)
	}

	data := struct {
		page
		Title string
		Users []string
	}{
		<-p,
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
	p := make(chan page, 1)
	go loadPage(env, r, p)

	userlist, err := env.authState.Userlist()
	if err != nil {
		panic(err)
	}

	//ctx := r.Context()
	params := getParams(r.Context())
	selectedUser := params["username"]

	data := struct {
		page
		Title string
		Users []string
		User  string
	}{
		<-p,
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
	p := make(chan page, 1)
	go loadPage(env, r, p)

	gp := &genPage{
		<-p,
		title,
	}
	renderTemplate(r.Context(), env, w, "admin_main.tmpl", gp)
}

func (env *wikiEnv) adminConfigHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "adminConfigHandler")

	// To save config to toml:
	viperMap := viper.AllSettings()

	title := "admin-config"
	p := make(chan page, 1)
	go loadPage(env, r, p)

	data := struct {
		page
		Title  string
		Config map[string]interface{}
	}{
		<-p,
		title,
		viperMap,
	}
	renderTemplate(r.Context(), env, w, "admin_config.tmpl", data)
}

func (env *wikiEnv) gitCheckinHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "gitCheckinHandler")

	title := "Git Checkin"
	p := make(chan page, 1)
	go loadPage(env, r, p)

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
		<-p,
		title,
		s,
		viper.GetString("RemoteGitRepo"),
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
	p := make(chan page, 1)
	go loadPage(env, r, p)

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
		<-p,
		title,
		err.Error(),
		viper.GetString("RemoteGitRepo"),
	}
	renderTemplate(r.Context(), env, w, "admin_git.tmpl", gp)
}

type tagMapPage struct {
	page
	TagKeys map[string][]string
}

func (env *wikiEnv) tagMapHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "tagMapHandler")

	p := make(chan page, 1)
	go loadPage(env, r, p)

	list := env.tags.GetAll()

	tagpage := &tagMapPage{
		page:    <-p,
		TagKeys: list,
	}

	renderTemplate(r.Context(), env, w, "tag_list.tmpl", tagpage)
}

type tagPage struct {
	page
	TagName string
	Results []string
}

func (env *wikiEnv) tagHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "tagHandler")

	params := getParams(r.Context())
	name := params["name"]

	p := make(chan page, 1)
	go loadPage(env, r, p)

	results := env.tags.GetOne(name)

	tagpage := &tagPage{
		page:    <-p,
		TagName: name,
		Results: results,
	}
	renderTemplate(r.Context(), env, w, "tag_view.tmpl", tagpage)
}

func (env *wikiEnv) createWiki(w http.ResponseWriter, r *http.Request, name string) {

	w.WriteHeader(404)
	//title := "Create " + name + "?"
	p := make(chan page, 1)
	go loadPage(env, r, p)

	wp := &wikiPage{
		page: <-p,
		Wiki: wiki{
			Title:    name,
			Filename: name,
			Frontmatter: frontmatter{
				Title: name,
			},
		},
	}
	renderTemplate(r.Context(), env, w, "wiki_create.tmpl", wp)
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
		html = svg("file-text")
	}
	if gitType == "tree" {
		html = svg("folder-open")
	}
	return html
}

func svg(iconName string) template.HTML {
	// MAJOR TODO:
	// Check for file existence before trying to read the file; if non-existent return ""
	iconFile, err := ioutil.ReadFile("assets/icons/" + iconName + ".svg")
	if err != nil {
		log.Println("Error loading assets/icons/", iconName, err)
	}
	return template.HTML(`<div class="svg-icon">` + string(iconFile) + `</div>`)
}

func svgByte(iconName string) []byte {
	// MAJOR TODO:
	// Check for file existence before trying to read the file; if non-existent return ""
	iconFile, err := ioutil.ReadFile("assets/icons/" + iconName + ".svg")
	if err != nil {
		log.Println("Error loading assets/icons/", iconName, err)
	}
	return []byte(`<div class="svg-icon">` + string(iconFile) + `</div>`)
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

	funcMap := template.FuncMap{"svg": svg, "typeIcon": typeIcon, "prettyDate": httputils.PrettyDate, "safeHTML": httputils.SafeHTML, "imgClass": httputils.ImgClass, "isLoggedIn": isLoggedIn, "jsTags": jsTags}

	for _, layout := range layouts {
		files := append(includes, layout)
		//DEBUG TEMPLATE LOADING
		//httputils.Debugln(files)
		env.templates[filepath.Base(layout)] = template.Must(template.New("templates").Funcs(funcMap).ParseFiles(files...))
	}
	return nil
}

func initWikiDir() {
	// Check for root DataDir existence first
	dir, err := os.Stat(viper.GetString("DataDir"))
	if err != nil {
		if os.IsNotExist(err) {
			log.Println(viper.GetString("DataDir"), "does not exist; creating it.")
			err = os.Mkdir(viper.GetString("DataDir"), 0755)
			if err != nil {
				log.Fatalln(err)
			}
		} else {
			log.Fatalln(err)
		}
	} else if !dir.IsDir() {
		log.Fatalln(viper.GetString("DataDir"), "is not a directory. This is where wiki data is stored.")
	}

	//Check for wikiDir directory + git repo existence
	wikiDir := filepath.Join(dataDir, "wikidata")
	_, err = os.Stat(wikiDir)
	if err != nil {
		log.Println(wikiDir + " does not exist, creating it.")
		os.Mkdir(wikiDir, 0755)
	}
	_, err = os.Stat(filepath.Join(wikiDir, ".git"))
	if err != nil {
		log.Println(wikiDir + " is not a git repo!")
		if viper.GetBool("InitWikiRepo") {
			if viper.GetString("RemoteGitRepo") != "" {
				log.Println("--InitWikiRepo flag is given. Cloning " + viper.GetString("RemoteGitRepo") + " into " + wikiDir + "...")
				err = gitClone(viper.GetString("RemoteGitRepo"))
				check(err)
			} else {
				log.Println("No RemoteGitRepo defined. Creating a new git repo at", wikiDir)
				err = gitInit()
				check(err)
			}
		} else {
			repoNotExistErr := errors.New("clone/move your existing repo here, change the config, or run with --InitWikiRepo to clone a specified remote repo")
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

type result struct {
	Name   string
	Result string
}

type searchPage struct {
	page
	Results []result
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

	p := make(chan page, 1)
	go loadPage(env, r, p)

	user := auth.GetUserState(r.Context())

	var fileList string

	for _, v := range env.cache.Cache {
		if user.IsLoggedIn() {
			if v.Permission == privatePermission {
				//log.Println("priv", v.Filename)
				fileList = fileList + " " + `"` + v.Filename + `"`
			}
			if user.IsAdmin() {
				if v.Permission == adminPermission {
					//log.Println("admin", v.Filename)
					fileList = fileList + " " + `"` + v.Filename + `"`
				}
			}
		}

		if v.Permission == publicPermission {
			//log.Println("pubic", v.Filename)
			fileList = fileList + " " + `"` + v.Filename + `"`
		}
	}

	//log.Println(fileList)

	results := gitSearch(name, strings.TrimSpace(fileList))

	s := &searchPage{
		page:    <-p,
		Results: results,
	}
	renderTemplate(r.Context(), env, w, "search_results.tmpl", s)
}

func buildCache() *wikiCache {
	defer httputils.TimeTrack(time.Now(), "buildCache")
	cache := new(wikiCache)
	cache.Tags = make(map[string][]string)
	cache.Favs = make(map[string]struct{})
	var wps []gitDirList

	if !gitIsEmpty() {

		fileList, err := gitLsTree()
		check(err)

		for _, file := range fileList {

			wikiDir := filepath.Join(dataDir, "wikidata")
			// If using Git, build the full path:
			fullname := filepath.Join(wikiDir, file.Filename)

			// If this is a directory, add it to the list for listing
			//   but just assume it is private
			if file.Type == "tree" {
				var wp gitDirList
				wp = gitDirList{
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

				ctime := make(chan int64, 1)
				mtime := make(chan int64, 1)
				go gitGetTimes(file.Filename, ctime, mtime)

				var wp gitDirList

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
					//cache.Favs.LoadOrStore(file.Filename)
					if _, ok := cache.Favs[file.Filename]; !ok {
						httputils.Debugln("buildCache.favs: " + file.Filename + " is not already a favorite.")
						cache.Favs[file.Filename] = struct{}{}
					}
				}
				if fm.Tags != nil {
					//cache.Tags.LoadOrStore(fm.Tags, file.Filename)
					for _, tag := range fm.Tags {
						cache.Tags[tag] = append(cache.Tags[tag], file.Filename)
					}
				}
				/*
					ctime, err := gitGetCtime(file.Filename)
					check(err)
					mtime, err := gitGetMtime(file.Filename)
					check(err)
				*/

				wp = gitDirList{
					Type:       "blob",
					Filename:   file.Filename,
					CreateTime: <-ctime,
					ModTime:    <-mtime,
					Permission: fm.Permission,
				}
				wps = append(wps, wp)
			}
		}
		cache.SHA1 = headHash()
	}

	cache.Cache = wps

	if viper.GetBool("CacheEnabled") {
		cacheFile, err := os.Create(filepath.Join(dataDir, "cache.gob"))
		check(err)
		cacheEncoder := gob.NewEncoder(cacheFile)
		err = cacheEncoder.Encode(cache)
		check(err)
		err = cacheFile.Close()
		check(err)
	}

	return cache
}

func loadCache() *wikiCache {
	cache := new(wikiCache)
	cache.Tags = make(map[string][]string)
	cache.Favs = make(map[string]struct{})
	if viper.GetBool("CacheEnabled") {
		cacheFile, err := os.Open(filepath.Join(dataDir, "cache.gob"))
		defer cacheFile.Close()
		if err == nil {
			log.Println("Loading cache from gob.")
			cacheDecoder := gob.NewDecoder(cacheFile)
			err = cacheDecoder.Decode(&cache)
			//check(err)
			if err != nil {
				log.Println("Error loading cache. Rebuilding it.", err)
				cache = buildCache()
			}
			// Check the cached sha1 versus HEAD sha1, rebuild if they differ
			if !gitIsEmpty() && cache.SHA1 != headHash() {
				log.Println("Cache SHA1s do not match. Rebuilding cache.")
				cache = buildCache()
			}
		}
		// If cache does not exist, build it
		if os.IsNotExist(err) {
			log.Println("Cache does not exist, building it...")
			cache = buildCache()
		}
	} else {
		log.Println("Building cache...")
		cache = buildCache()
	}

	return cache
}

func headHash() string {
	output, err := gitCommand("rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		log.Println("Error retrieving SHA1 of wikidata:", err)
		return ""
	}
	return string(output)
}

// This should be all the stuff we need to be refreshed on startup and when pages are saved
func (env *wikiEnv) refreshStuff() {
	env.cache = loadCache()
	env.favs.List = env.cache.Favs
	env.tags.List = env.cache.Tags
}

func markdownPreview(w http.ResponseWriter, r *http.Request) {

	w.Write([]byte(markdownRender([]byte(r.PostFormValue("md")))))
}

// return false if request should be allowed
// return true if request should be rejected
func wikiRejected(wikiName string, wikiExists, isAdmin, isLoggedIn bool) bool {

	httputils.Debugln("wikiRejected name", wikiName)

	// if wikiExists, read the frontmatter and reject/accept based on frontmatter.Permission
	if wikiExists {
		wikiDir := filepath.Join(dataDir, "wikidata")
		fullfilename := filepath.Join(wikiDir, wikiName)

		// If err, reject, and log that error
		f, err := os.Open(fullfilename)
		if err != nil {
			log.Println("wikiRejected: Error reading", fullfilename, err)
			return true
		}
		fm := readFront(f)
		f.Close()
		switch fm.Permission {
		case adminPermission:
			if isAdmin {
				return false
			}
		case publicPermission:
			return false
		case privatePermission:
			if isLoggedIn {
				return false
			}
		default:
			if isAdmin {
				return false
			}
		}
		if isAdmin {
			return false
		}
	}

	if isLoggedIn {
		return false
	}

	return true

}

func (env *wikiEnv) wikiMiddle(next http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params := getParams(r.Context())
		name := params["name"]
		user := auth.GetUserState(r.Context())
		pageExists, relErr := checkName(&name)
		//wikiDir := filepath.Join(dataDir, "wikidata")
		//fullfilename := filepath.Join(wikiDir, name)

		if relErr != nil {
			if relErr == errBaseNotDir {
				http.Error(w, "Cannot create subdir of a file.", 500)
				return
			}

			// If the given name is a directory, and URL is just /name/, check for /name/index
			//    If name/index exists, redirect to it
			if relErr == errIsDir && r.URL.Path[:len("/"+name)] == "/"+name {
				// Check if name/index exists, and if it does, serve it
				_, err := os.Stat(filepath.Join(dataDir, "wikidata", name, "index"))
				if err == nil {
					http.Redirect(w, r, "/"+path.Join(name, "index"), http.StatusFound)
					return
				}
			}
			httpErrorHandler(w, r, relErr)
			return
		}

		nameCtx := newNameContext(r.Context(), name)
		ctx := newWikiExistsContext(nameCtx, pageExists)
		r = r.WithContext(ctx)

		if wikiRejected(name, pageExists, user.IsAdmin(), user.IsLoggedIn()) {
			mitigateWiki(true, env, r, w)
		} else {
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

	})
}

// mitigateWiki is a general redirect handler; redirect should be set to true for login mitigations
func mitigateWiki(redirect bool, env *wikiEnv, r *http.Request, w http.ResponseWriter) {
	httputils.Debugln("mitigateWiki: " + r.Host + r.URL.Path)
	env.authState.SetFlash("Unable to view that.", w)
	if redirect {
		// Use auth.Redirect to redirect while storing the current URL for future use
		auth.Redirect(&env.authState, w, r)
	} else {
		http.Redirect(w, r, "/index", http.StatusFound)
	}

}

func (env *wikiEnv) setFavoriteHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "setFavoriteHandler")
	name := nameFromContext(r.Context())
	if !wikiExistsFromContext(r.Context()) {
		http.Redirect(w, r, "/"+name, http.StatusFound)
		return
	}
	p := loadWikiPage(env, r, name)
	if p.Wiki.Frontmatter.Favorite {
		p.Wiki.Frontmatter.Favorite = false
		env.authState.SetFlash(name+" has been un-favorited.", w)
		log.Println(name + " page un-favorited!")
	} else {
		p.Wiki.Frontmatter.Favorite = true
		env.authState.SetFlash(name+" has been favorited.", w)
		log.Println(name + " page favorited!")
	}

	err := p.Wiki.save(&env.mutex)
	if err != nil {
		log.Println(err)
	}

	http.Redirect(w, r, "/"+name, http.StatusSeeOther)

}

func (env *wikiEnv) deleteHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "deleteHandler")
	name := nameFromContext(r.Context())
	if !wikiExistsFromContext(r.Context()) {
		http.Redirect(w, r, "/"+name, http.StatusFound)
		return
	}
	err := gitRmFilepath(name)
	if err != nil {
		log.Println("Error deleting file from git repo,", name, err)
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}

	err = gitCommitWithMessage(name + " has been removed from git repo.")
	if err != nil {
		log.Println("Error commiting to git repo,", name, err)
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}

	env.authState.SetFlash(name+" page successfully deleted.", w)
	http.Redirect(w, r, "/", http.StatusSeeOther)

}

func dataDirCheck() {
	dir, err := os.Stat(viper.GetString("DataDir"))
	if err != nil {
		if os.IsNotExist(err) {
			log.Println(viper.GetString("DataDir"), "does not exist; creating it.")
			err = os.Mkdir(viper.GetString("DataDir"), 0755)
			if err != nil {
				log.Fatalln(err)
			}
		} else {
			log.Fatalln(err)
		}
	} else if !dir.IsDir() {
		log.Fatalln(viper.GetString("DataDir"), "is not a directory. This is where wiki data is stored.")
	}
}

func router(env *wikiEnv) http.Handler {

	csrfSecure := true
	if viper.GetBool("Dev") {
		csrfSecure = false
	}

	// HTTP stuff from here on out
	s := alice.New(httputils.Logger, env.authState.UserEnvMiddle, csrf.Protect([]byte("c379bf3ac76ee306cf72270cf6c5a612e8351dcb"), csrf.Secure(csrfSecure)))

	r := httptreemux.NewContextMux()

	r.PanicHandler = errorHandler

	r.GET("/", indexHandler)

	r.GET("/tags", env.authState.AuthMiddle(env.tagMapHandler))
	r.GET("/tag/*name", env.authState.AuthMiddle(env.tagHandler))

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

	r.POST("/md_render", markdownPreview)

	r.GET("/uploads/*", treeMuxWrapper(http.StripPrefix("/uploads/", http.FileServer(http.Dir("uploads")))))

	// Wiki page handlers
	r.GET(`/fav/*name`, env.authState.AuthMiddle(env.wikiMiddle(env.setFavoriteHandler)))
	r.GET(`/edit/*name`, env.authState.AuthMiddle(env.wikiMiddle(env.editHandler)))
	r.POST(`/save/*name`, env.authState.AuthMiddle(env.wikiMiddle(env.saveHandler)))
	r.GET(`/history/*name`, env.authState.AuthMiddle(env.wikiMiddle(env.historyHandler)))
	r.POST(`/delete/*name`, env.authState.AuthMiddle(env.wikiMiddle(env.deleteHandler)))
	r.GET(`/*name`, env.wikiMiddle(env.viewHandler))

	return s.Then(r)
}

func main() {

	// subscribe to SIGINT signals
	stopChan := make(chan os.Signal)
	signal.Notify(stopChan, os.Interrupt)

	initWikiDir()
	dataDirCheck()

	// Bring up authState
	anAuthState, err := auth.NewAuthState(filepath.Join(dataDir, "auth.db"))
	check(err)

	theCache := loadCache()

	env := &wikiEnv{
		authState: *anAuthState,
		cache:     theCache,
		templates: make(map[string]*template.Template),
		mutex:     sync.Mutex{},
	}
	env.favs.List = env.cache.Favs
	env.tags.List = env.cache.Tags

	err = tmplInit(env)
	if err != nil {
		log.Fatalln(err)
	}

	// Check for unclean Git dir on startup
	if !gitIsEmpty() {
		err = gitIsCleanStartup()
		if err != nil {
			log.Fatalln("There was an issue with the git repo:", err)
		}
	}

	mux := http.NewServeMux()
	mux.Handle("/debug/vars", expvar.Handler())
	mux.Handle("/debug/pprof/", env.authState.AuthAdminMiddle(http.HandlerFunc(pprof.Index)))
	mux.Handle("/debug/pprof/cmdline", env.authState.AuthAdminMiddle(http.HandlerFunc(pprof.Cmdline)))
	mux.Handle("/debug/pprof/profile", env.authState.AuthAdminMiddle(http.HandlerFunc(pprof.Profile)))
	mux.Handle("/debug/pprof/symbol", env.authState.AuthAdminMiddle(http.HandlerFunc(pprof.Symbol)))
	mux.Handle("/debug/pprof/trace", env.authState.AuthAdminMiddle(http.HandlerFunc(pprof.Trace)))
	mux.HandleFunc("/robots.txt", httputils.Robots)
	mux.HandleFunc("/favicon.ico", httputils.FaviconICO)
	mux.HandleFunc("/favicon.png", httputils.FaviconPNG)
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir("./assets"))))
	mux.Handle("/", router(env))

	httputils.Logfile = filepath.Join(dataDir, "http.log")

	log.Println("Listening on 127.0.0.1:" + viper.GetString("Port"))

	srv := &http.Server{
		Addr:    "127.0.0.1:" + viper.GetString("Port"),
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
