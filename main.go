package main

// Credits:
// - jQuery-Tags-Input: https://github.com/xoxco/jQuery-Tags-Input
//     - Used for elegant tags UI on editing page
// - YAML frontmatter based on http://godoc.org/j4k.co/fmatter
//     - Used for YAML frontmatter parsing to/from wiki pages
// - bpool-powered template rendering based on https://elithrar.github.io/article/approximating-html-template-inheritance/
//     - Used to catch rendering errors, so there's no half-rendered pages

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
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/justinas/alice"
	"github.com/oxtoacart/bpool"
	"github.com/russross/blackfriday"
	//"github.com/rhinoman/go-commonmark"
	//bf "gopkg.in/russross/blackfriday.v2"
	"context"
	"regexp"
	"runtime"

	"github.com/BurntSushi/toml"
	"github.com/GeertJohan/go.rice"
	"github.com/aqtrans/ctx-csrf"
	"github.com/blevesearch/bleve"
	"github.com/blevesearch/bleve/analysis/analyzers/keyword_analyzer"
	"github.com/blevesearch/bleve/analysis/language/en"
	"github.com/dimfeld/httptreemux"
	"github.com/getsentry/raven-go"
	"github.com/microcosm-cc/bluemonday"
	"github.com/spf13/viper"
	"github.com/thoas/stats"
	"gopkg.in/yaml.v2"
	"jba.io/go/auth"
	"jba.io/go/httputils"
)

type key int

const TimerKey key = 0

const yamlsep = "---"

const (
	commonHtmlFlags = 0 |
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

type renderer struct {
	*blackfriday.Html
}

func markdownRender(input []byte) string {
	renderer := &renderer{Html: blackfriday.HtmlRenderer(commonHtmlFlags, "", "").(*blackfriday.Html)}

	unsanitized := blackfriday.MarkdownOptions(input, renderer, blackfriday.Options{
		Extensions: commonExtensions})
	p := bluemonday.UGCPolicy()
	p.AllowElements("nav")

	return string(p.SanitizeBytes(unsanitized))
}

// Task List support.
func (r *renderer) ListItem(out *bytes.Buffer, text []byte, flags int) {
	switch {
	case bytes.HasPrefix(text, []byte("[ ] ")):
		text = append([]byte(`<input type="checkbox" disabled="">`), text[3:]...)
	case bytes.HasPrefix(text, []byte("[x] ")) || bytes.HasPrefix(text, []byte("[X] ")):
		text = append([]byte(`<input type="checkbox" checked="" disabled="">`), text[3:]...)
	}
	r.Html.ListItem(out, text, flags)
}

func (r *renderer) NormalText(out *bytes.Buffer, text []byte) {

	switch {
	case linkPattern.Match(text):
		//log.Println("text " + string(text))
		domain := "//" + viper.GetString("Domain")
		//log.Println(string(linkPattern.ReplaceAll(text, []byte(domain+"/$1"))))
		link := linkPattern.ReplaceAll(text, []byte(domain+"/$1"))
		title := linkPattern.ReplaceAll(text, []byte("/$1"))
		r.Html.Link(out, link, []byte(""), title)
		return
	}
	r.Html.NormalText(out, text)
	//log.Println("title " + string(title))
	//log.Println("content " + string(content))
}

type configuration struct {
	Domain     string
	Port       string
	Email      string
	WikiDir    string
	GitRepo    string
	AdminUser  string
	PushOnSave bool
}

type Renderer struct {
	blackfriday.Renderer
	urlPrefix string
}

var (
	linkPattern   = regexp.MustCompile(`\[\/(?P<Name>[0-9a-zA-Z-_\.\/]+)\]\(\)`)
	bufpool       *bpool.BufferPool
	templates     map[string]*template.Template
	_24K          int64 = (1 << 20) * 24
	fLocal        bool
	debug         = httputils.Debug
	fInit         bool
	gitPath       string
	favbuf        bytes.Buffer
	tagMap        map[string][]string
	tagsBuf       bytes.Buffer
	index         bleve.Index
	wikiList      map[string][]*wiki
	ErrNotInGit   = errors.New("given file not in Git repo")
	ErrNoFile     = errors.New("no such file")
	ErrNoDirIndex = errors.New("no such directory index")
	ErrBaseNotDir = errors.New("cannot create subdirectory of a file")
	ErrGitDirty   = errors.New("directory is dirty")
	ErrBadPath    = errors.New("given path is invalid")
)

//Base struct, page ; has to be wrapped in a data {} strut for consistency reasons
type page struct {
	SiteName string
	Favs     []string
	UN       string
	IsAdmin  bool
	Token    template.HTML
	FlashMsg string
}

type frontmatter struct {
	Title    string   `yaml:"title"`
	Tags     []string `yaml:"tags,omitempty"`
	Favorite bool     `yaml:"favorite,omitempty"`
	Public   bool     `yaml:"public,omitempty"`
	Admin    bool     `yaml:"admin,omitempty"`
}

type badFrontmatter struct {
	Title    string `yaml:"title"`
	Tags     string `yaml:"tags,omitempty"`
	Favorite bool   `yaml:"favorite,omitempty"`
	Public   bool   `yaml:"public,omitempty"`
	Admin    bool   `yaml:"admin,omitempty"`
}

type wiki struct {
	Title       string
	Filename    string
	Frontmatter *frontmatter
	Content     []byte
	CreateTime  int64
	ModTime     int64
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
	Wikis       []*wiki
	PublicWikis []*wiki
	AdminWikis  []*wiki
}

type genPage struct {
	*page
	Title string
}

type gitPage struct {
	*page
	Title     string
	GitFiles  string
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

type jsonfresponse struct {
	Href string `json:"href,omitempty"`
	Name string `json:"name,omitempty"`
}

type wHandler func(http.ResponseWriter, *http.Request, string)

// Sorting functions
type wikiByDate []*wiki

func (a wikiByDate) Len() int           { return len(a) }
func (a wikiByDate) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a wikiByDate) Less(i, j int) bool { return a[i].CreateTime < a[j].CreateTime }

type wikiByModDate []*wiki

func (a wikiByModDate) Len() int           { return len(a) }
func (a wikiByModDate) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a wikiByModDate) Less(i, j int) bool { return a[i].ModTime < a[j].ModTime }

var conf configuration

func init() {
	//toml.DecodeFile("./data/conf.toml", &conf);

	raven.SetDSN("https://5ab2f68b0f524799b1d0b324350cc2ae:e01dbad12f8e4fd0bce97681a772a072@app.getsentry.com/94753")

	// Viper config.
	viper.SetDefault("Port", "3000")
	viper.SetDefault("Email", "unused@the.moment")
	viper.SetDefault("WikiDir", "./data/wikidata/")
	viper.SetDefault("Domain", "wiki.example.com")
	viper.SetDefault("GitRepo", "git@example.com:user/wikidata.git")
	viper.SetDefault("AdminUser", "admin")
	viper.SetDefault("PushOnSave", false)

	viper.SetConfigName("conf")
	viper.AddConfigPath("./data/")
	err := viper.ReadInConfig() // Find and read the config file
	if err != nil {             // Handle errors reading the config file
		//panic(fmt.Errorf("Fatal error config file: %s \n", err))
		fmt.Println("No configuration file loaded - using defaults")
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

	//Flag '-l' enables go.dev and *.dev domain resolution
	flag.BoolVar(&fLocal, "l", false, "Turn on localhost resolving for Handlers")
	//Flag '-d' enabled debug logging
	flag.BoolVar(&httputils.Debug, "d", false, "Enabled debug logging")
	//Flag '-init' enables pulling of remote git repo into wikiDir
	flag.BoolVar(&fInit, "init", false, "Enable auto-cloning of remote wikiDir")

	bufpool = bpool.NewBufferPool(64)
	if templates == nil {
		templates = make(map[string]*template.Template)
	}

	gitPath, err = exec.LookPath("git")
	if err != nil {
		log.Fatal("git must be installed")
	}
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

	tpl := template.Must(template.New("ErrorPage").Parse(panicPageTpl))
	tpl.Execute(w, data)

}

func (conf *configuration) save() bool {
	buf := new(bytes.Buffer)
	err := toml.NewEncoder(buf).Encode(conf)
	if err != nil {
		log.Println(err)
		return false
	}
	//log.Println(buf.String())

	err = ioutil.WriteFile("./data/conf.toml", buf.Bytes(), 0644)
	if err != nil {
		log.Println(err)
		return false
	}
	return true
}

func timeNewContext(c context.Context, t time.Time) context.Context {
	return context.WithValue(c, TimerKey, t)
}

func timeFromContext(c context.Context) time.Time {
	t, ok := c.Value(TimerKey).(time.Time)
	if !ok {
		httputils.Debugln("No startTime in context.")
		t = time.Now()
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
	//log.Println(tags)
	return tags
}

/*
// Special Markdown render helper to convert [/empty/wiki/links]() to a full <a href> link
// Borrowed most of this from https://raw.githubusercontent.com/gogits/gogs/master/modules/markdown/markdown.go
func replaceInterwikiLinks(rawBytes []byte, urlPrefix string) []byte {
	return linkPattern.ReplaceAll(rawBytes, []byte(fmt.Sprintf(`<a href="%s/$1">/$1</a>`, urlPrefix)))
	/*
		ms := linkPattern.FindAll(rawBytes, -1)
		for _, m := range ms {
			m2 := bytes.TrimPrefix(m, []byte("["))
			m2 = bytes.TrimSuffix(m2, []byte("]()"))
			//log.Println(string(m2))
			rawBytes = []byte(fmt.Sprintf(`<a href="%s%s">%s</a>`, urlPrefix, m2, m2, ))
			//rawBytes = link
		}

	//return rawBytes
}
*/

// CUSTOM GIT WRAPPERS
// Construct an *exec.Cmd for `git {args}` with a workingDirectory
func gitCommand(args ...string) *exec.Cmd {
	c := exec.Command(gitPath, args...)
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
		return errors.New(fmt.Sprintf("error during `git clone`: %s\n%s", err.Error(), string(o)))
	}
	return nil
}

// Execute `git status -s` in directory
// If there is output, the directory has is dirty
func gitIsClean() ([]byte, error) {
	c := gitCommand("status", "-z")

	o, err := c.Output()
	if err != nil {
		return nil, err
	}

	if len(o) != 0 {
		return o, ErrGitDirty
	}

	return nil, nil
}

// Execute `git add {filepath}` in workingDirectory
func gitAddFilepath(filepath string) error {
	o, err := gitCommand("add", filepath).CombinedOutput()
	if err != nil {
		return errors.New(fmt.Sprintf("error during `git add`: %s\n%s", err.Error(), string(o)))
	}
	return nil
}

// Execute `git commit -m {msg}` in workingDirectory
func gitCommitWithMessage(msg string) error {
	o, err := gitCommand("commit", "-m", msg).CombinedOutput()
	if err != nil {
		return errors.New(fmt.Sprintf("error during `git commit`: %s\n%s", err.Error(), string(o)))
	}

	return nil
}

// Execute `git commit -m "commit from GoWiki"` in workingDirectory
func gitCommitEmpty() error {
	o, err := gitCommand("commit", "-m", "commit from GoWiki").CombinedOutput()
	if err != nil {
		return errors.New(fmt.Sprintf("error during `git commit`: %s\n%s", err.Error(), string(o)))
	}

	return nil
}

// Execute `git push` in workingDirectory
func gitPush() error {
	o, err := gitCommand("push", "-u", "origin", "master").CombinedOutput()
	if err != nil {
		return errors.New(fmt.Sprintf("error during `git push`: %s\n%s", err.Error(), string(o)))
	}

	return nil
}

// Execute `git push` in workingDirectory
func gitPull() error {
	o, err := gitCommand("pull").CombinedOutput()
	if err != nil {
		return errors.New(fmt.Sprintf("error during `git pull`: %s\n%s", err.Error(), string(o)))
	}

	return nil
}

// File creation time, output to UNIX time
// git log --diff-filter=A --follow --format=%at -1 -- [filename]
func gitGetCtime(filename string) (int64, error) {
	//var ctime int64
	o, err := gitCommand("log", "--diff-filter=A", "--follow", "--format=%at", "-1", "--", filename).Output()
	if err != nil {
		return 0, errors.New(fmt.Sprintf("error during `git log --diff-filter=A --follow --format=%aD -1 --`: %s\n%s", err.Error(), string(o)))
	}
	ostring := strings.TrimSpace(string(o))
	// If output is blank, no point in wasting CPU doing the rest
	if ostring == "" {
		log.Println(filename + " is not checked into Git")
		return 0, ErrNotInGit
	}
	ctime, err := strconv.ParseInt(ostring, 10, 64)
	if err != nil {
		log.Println("gitGetCtime error:")
		log.Println(err)
	}
	return ctime, nil
}

// File modification time, output to UNIX time
// git log -1 --format=%at -- [filename]
func gitGetMtime(filename string) (int64, error) {
	//var mtime int64
	o, err := gitCommand("log", "--format=%at", "-1", "--", filename).Output()
	if err != nil {
		return 0, errors.New(fmt.Sprintf("error during `git log -1 --format=%aD --`: %s\n%s", err.Error(), string(o)))
	}
	ostring := strings.TrimSpace(string(o))
	// If output is blank, no point in wasting CPU doing the rest
	if ostring == "" {
		log.Println(filename + " is not checked into Git")
		return 0, nil
	}
	mtime, err := strconv.ParseInt(ostring, 10, 64)
	if err != nil {
		log.Println("gitGetMtime error:")
		log.Println(err)
	}

	return mtime, nil
}

// File history
// git log --pretty=format:"commit:%H date:%at message:%s" [filename]
// git log --pretty=format:"%H,%at,%s" [filename]
func gitGetFileLog(filename string) ([]*commitLog, error) {
	o, err := gitCommand("log", "--pretty=format:%H,%at,%s", filename).Output()
	if err != nil {
		return nil, errors.New(fmt.Sprintf("error during `git log`: %s\n%s", err.Error(), string(o)))
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
		//log.Println(shortsha)
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
		return []byte{}, errors.New(fmt.Sprintf("error during `git show`: %s\n%s", err.Error(), string(o)))
	}
	return o, nil
}

// Get diff for entire commit
// git show [commit sha1]
func gitGetFileCommitDiff(filename, commit string) ([]byte, error) {
	o, err := gitCommand("show", commit).CombinedOutput()
	if err != nil {
		return []byte{}, errors.New(fmt.Sprintf("error during `git show`: %s\n%s", err.Error(), string(o)))
	}
	return o, nil
}

// File modification time for specific commit, output to UNIX time
// git log -1 --format=%at [commit sha1]
func gitGetFileCommitMtime(commit string) (int64, error) {
	//var mtime int64
	o, err := gitCommand("log", "--format=%at", "-1", commit).Output()
	if err != nil {
		return 0, errors.New(fmt.Sprintf("error during `git log -1 --format=%aD --`: %s\n%s", err.Error(), string(o)))
	}
	ostring := strings.TrimSpace(string(o))
	mtime, err := strconv.ParseInt(ostring, 10, 64)
	if err != nil {
		log.Println("gitGetFileCommitMtime error:")
		log.Println(err)
	}
	return mtime, nil
}

// git ls-files [filename]
func gitLs() ([]string, error) {
	o, err := gitCommand("ls-files", "-z").Output()
	if err != nil {
		return nil, errors.New(fmt.Sprintf("error during `git ls-files`: %s\n%s", err.Error(), string(o)))
	}
	nul := bytes.Replace(o, []byte("\x00"), []byte("\n"), -1)
	// split each commit onto it's own line
	lssplit := strings.Split(string(nul), "\n")
	return lssplit, nil
}

// git log --name-only --pretty=format:"%at %H" -z HEAD
func gitHistory() ([]string, error) {
	o, err := gitCommand("log", "--name-only", "--pretty=format:_END %at %H", "-z", "HEAD").Output()
	if err != nil {
		return nil, errors.New(fmt.Sprintf("error during `git history`: %s\n%s", err.Error(), string(o)))
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

/////////////////////////////////////////////////////////////////////////////////////////////////
/*
func isPrivate(list string) bool {
	tags := strings.Split(list, " ")
	for _, v := range tags {
		if v == "private" {
			return true
		}
	}
	return false
}
*/

func isPrivate(list string) bool {
	tags := strings.Split(list, " ")
	for _, v := range tags {
		if v == "private" {
			return true
		}
	}
	return false
}

func isPrivateA(tags []string) bool {
	//tags := strings.Split(list, " ")
	for _, v := range tags {
		if v == "private" {
			return true
		}
	}
	return false
}

/*
func markdownRender(content []byte) string {
	//md := markdown.New(markdown.HTML(true), markdown.Nofollow(true), markdown.Breaks(true))
	//mds := md.RenderToString(content)

	// build full URL out of configured Domain:
	domain := "//" + viper.GetString("Domain")
	result := replaceInterwikiLinks(content, domain)

	md := markdownCommon(result)

	p := bluemonday.UGCPolicy()
	p.AllowElements("nav")

	html := p.SanitizeBytes(md)

	//mds := string(md)
	//return mds
	return string(html)
}
*/

/* Markdown renderers used for benchmarks
// May come back to using Blackfriday.v2 when it's stabalized,
// and this should be a good starting point
func markdownCommon2(input []byte) []byte {
	// set up the HTML renderer
	renderer := bf.NewHTMLRenderer(bf.HTMLRendererParameters{
		Flags:      commonHTMLFlags2,
		Extensions: commonExtensions2,
	})
	opt := bf.Options{
		Extensions: commonExtensions2,
	}
	return bf.Markdown(input, renderer, opt)
}

func markdownRender2(content []byte) string {
	opt := bf.Options{
		Extensions: commonExtensions2,
	}
	renderer := bf.NewHTMLRenderer(bf.HTMLRendererParameters{
		Flags:      commonHTMLFlags2,
		Extensions: commonExtensions2,
	})

	//html := markdownCommon2(content)
	ast := bf.Parse(content, opt)
	domain := "//" + viper.GetString("Domain")

	var buff bytes.Buffer
	//defaultRenderer := bf.NewHTMLRenderer(bf.HTMLRendererParameters{})
	ast.Walk(func(node *bf.Node, entering bool) bf.WalkStatus {
		//log.Println(string(node.Literal))
		if linkPattern.Match(node.Literal) {
			ms := linkPattern.FindAll(node.Literal, -1)
			for _, m := range ms {
				m = bytes.TrimPrefix(m, []byte("["))
				m = bytes.TrimSuffix(m, []byte("]()"))
				//log.Println(string(m2))
				//link := []byte(fmt.Sprintf(`<a href="%s%s">`, domain, m2, ))
				//node.Literal = link
				buff.WriteString(fmt.Sprintf(`<a href="%s%s">`, domain, m, ))
			}
		} else {
			renderer.RenderNode(&buff, node, entering)
		}
		return bf.GoToNext
	})
	//log.Println(string(buff.Bytes()))
	return string(buff.Bytes())
}

func commonmarkRender(content []byte) string {

	// build full URL out of configured Domain:
	domain := "//" + viper.GetString("Domain")

	result := RenderLinkCurrentPattern(content, domain)

	md := commonmark.Md2Html(string(result), commonmark.CMARK_OPT_DEFAULT)

	p := bluemonday.UGCPolicy()
	p.AllowElements("nav")

	html := p.Sanitize(md)

	return html
}
*/

func loadPage(r *http.Request) *page {
	//timer.Step("loadpageFunc")

	// Auth lib middlewares should load the user and tokens into context for reading
	user, isAdmin := auth.GetUsername(r.Context())
	msg := auth.GetFlash(r.Context())
	//token := auth.GetToken(r.Context())
	token := csrf.TemplateField(r.Context(), r)

	//log.Println("Message: ")
	//log.Println(msg)

	var message string
	if msg != "" {
		message = `
			<div class="alert callout" data-closable>
			<h5>Alert!</h5>
			<p>` + msg + `</p>
			<button class="close-button" aria-label="Dismiss alert" type="button" data-close>
				<span aria-hidden="true">&times;</span>
			</button>
			</div>			
        `
	} else {
		message = ""
	}

	// Grab list of favs from channel
	favs := make(chan []string)
	go favsHandler(favs)
	gofavs := <-favs

	//log.Println(gofavs)
	return &page{SiteName: "GoWiki", Favs: gofavs, UN: user, IsAdmin: isAdmin, Token: token, FlashMsg: message}
}

func historyHandler(w http.ResponseWriter, r *http.Request, name string) {
	wikip, err := loadWikiPage(r, name)
	if err != nil {
		panic(err)
	}

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
	renderTemplate(w, r.Context(), "wiki_history.tmpl", hp)
}

// Need to get content of the file at specified commit
// > git show [commit sha1]:[filename]
// As well as the date
// > git log -1 --format=%at [commit sha1]
// TODO: need to find a way to detect sha1s
func viewCommitHandler(w http.ResponseWriter, r *http.Request, commit, name string) {
	var fm frontmatter
	var pagetitle string
	var pageContent string

	//commit := vars["commit"]

	p := loadPage(r)

	body, err := gitGetFileCommit(name, commit)
	if err != nil {
		panic(err)
	}
	ctime, err := gitGetCtime(name)
	if err != nil && err != ErrNotInGit {
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
	fmbytes, content, err := readFront(body)
	if err != nil {
		log.Println("viewCommitHandler readFront error:")
		log.Println(err)
	}
	if content == nil {
		content = []byte("")
	}

	fm, err = marshalFrontmatter(fmbytes)
	if err != nil {
		log.Println(err)
	}

	// Render remaining content after frontmatter
	md := markdownRender(content)
	//md := commonmarkRender(content)
	if fm.Title != "" {
		pagetitle = fm.Title
	} else {
		pagetitle = name
	}
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

	renderTemplate(w, r.Context(), "wiki_commit.tmpl", cp)

}

// TODO: Fix this
func recentHandler(w http.ResponseWriter, r *http.Request) {

	p := loadPage(r)

	gh, err := gitHistory()
	if err != nil {
		log.Println(err)
	}
	//log.Println(gh)
	//log.Println(gh[10])
	/*
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(200)
	*/
	var split []string
	var split2 []string
	var recents []*recent

	//log.Println(gh[0])
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
	renderTemplate(w, r.Context(), "recents.tmpl", s)

}

func listHandler(w http.ResponseWriter, r *http.Request) {
	//searchDir := cfg.WikiDir

	p := loadPage(r)

	// ~~Currently doing a filepath.Walk over cfg.WikiDir to build a list of wiki pages~~
	// ~~But since we use git...should we use git to retrieve the list?~~
	//fileList := []string{}

	//privFileList := []string{}

	/*_ = filepath.Walk(cfg.WikiDir, func(path string, f os.FileInfo, err error) error {
		// check and skip .git
		if f.IsDir() && f.Name() == ".git" {
			return filepath.SkipDir
		}
		//log.Println(path)
		// If not .git, check if there is an index file within
		// If there is, check the frontmatter for private/admin
		// If THAT exists, we'll set SkipDir, and run a separate function for these dirs
		/*if f.IsDir() && f.Name() != ".git" {
			dirindexpath := path + "/" + "index"
			//log.Println(dirindexpath)
			dirindex, _ := os.Open(dirindexpath)
			_, dirindexfierr := dirindex.Stat()
			if !os.IsNotExist(dirindexfierr) {
				dread, err := ioutil.ReadFile(dirindexpath)
				if err != nil {
					log.Println("wikiauth dir index ReadFile error:")
					log.Println(err)
				}

				// Read YAML frontmatter into fm
				// If err, just return, as file should not contain frontmatter
				var dfm frontmatter
				dfm, _, err = readFront(dread)
				if err != nil {
					log.Println("wikiauth readFront error:")
					log.Println(err)
					//return nil
				}
				if err == nil {
					if dfm.Private || dfm.Admin {
						//log.Println(path)
						//log.Println(filepath.Dir(path))
						//log.Println(f.Name())
						//return filepath.SkipDir
					}
				}
			}
		}
		fileList = append(fileList, path)
		return nil
	})*/

	/*

		fileList, err := gitLs()
		if err != nil {
			panic(err)
		}

		//w.Header().Set("Content-Type", "text/html; charset=utf-8")
		//w.WriteHeader(200)

		var wps []*wiki
		var publicwps []*wiki
		var adminwps []*wiki
		for _, file := range fileList {

			// If using Git, build the full path:
			fullname := filepath.Join(viper.GetString("WikiDir"), file)
			//file = viper.GetString("WikiDir")+file
			//log.Println(file)
			//log.Println(fullname)

			// check if the source dir exist
			src, err := os.Stat(fullname)
			if err != nil {
				panic(err)
			}
			// If not a directory, get frontmatter from file and add to list
			if !src.IsDir() {

				_, filename := filepath.Split(file)

				// If this is an absolute path, including the cfg.WikiDir, trim it
				//withoutdotslash := strings.TrimPrefix(viper.GetString("WikiDir"), "./")
				//fileURL := strings.TrimPrefix(file, withoutdotslash)

				var wp *wiki
				var fm frontmatter
				var pagetitle string

				//log.Println(file)

				// Read YAML frontmatter into fm
				fmbytes, _, err := readFileAndFront(fullname)
				if err != nil {
					log.Println(err)
				}
				fm, err = marshalFrontmatter(fmbytes)
				if err != nil {
					log.Println("YAML unmarshal error in: " + file)
					log.Println(err)
				}

				if fm.Public {
					//log.Println("Private page!")
				}
				pagetitle = filename
				if fm.Title != "" {
					pagetitle = fm.Title
				}
				if fm.Title == "" {
					fm.Title = file
				}
				if fm.Public != true {
					fm.Public = false
				}
				if fm.Admin != true {
					fm.Admin = false
				}
				if fm.Favorite != true {
					fm.Favorite = false
				}
				if fm.Tags == nil {
					fm.Tags = []string{}
				}
				ctime, err := gitGetCtime(file)
				if err != nil && err != ErrNotInGit {
					panic(err)
				}
				mtime, err := gitGetMtime(file)
				if err != nil {
					panic(err)
				}

				// If pages are Admin or Public, add to a separate wikiPage slice
				//   So we only check on rendering
				if fm.Admin {
					wp = &wiki{
						Title:       pagetitle,
						Filename:    file,
						Frontmatter: &fm,
						CreateTime:  ctime,
						ModTime:     mtime,
					}
					adminwps = append(adminwps, wp)
				} else if fm.Public {
					wp = &wiki{
						Title:       pagetitle,
						Filename:    file,
						Frontmatter: &fm,
						CreateTime:  ctime,
						ModTime:     mtime,
					}
					publicwps = append(publicwps, wp)
				} else {
					wp = &wiki{
						Title:       pagetitle,
						Filename:    file,
						Frontmatter: &fm,
						CreateTime:  ctime,
						ModTime:     mtime,
					}
					wps = append(wps, wp)
				}
				//log.Println(string(body))
				//log.Println(string(wp.wiki.Content))
			}

		}

		l := &listPage{p, wps, publicwps, adminwps}
	*/
	l := &listPage{p, wikiList["private"], wikiList["public"], wikiList["admin"]}
	renderTemplate(w, r.Context(), "list.tmpl", l)
}

func readFile(filepath string) []byte {
	data, err := ioutil.ReadFile(filepath)
	if err != nil {
		panic(err)
		return nil
	}
	return data
}

func readFileAndFront(filepath string) (fmdata []byte, content []byte, err error) {
	data := readFile(filepath)
	return readFront(data)
}

func readFront(data []byte) (fmdata []byte, content []byte, err error) {
	//defer utils.TimeTrack(time.Now(), "readFront")

	r := bytes.NewBuffer(data)

	// eat away starting whitespace
	var ch rune = ' '
	for unicode.IsSpace(ch) {
		ch, _, err = r.ReadRune()
		if err != nil {
			// file is just whitespace
			return []byte{}, []byte{}, nil
		}
	}
	r.UnreadRune()

	// check if first line is ---
	line, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		return nil, nil, err
	}

	if strings.TrimSpace(line) != "---" {
		// no front matter, just content
		return []byte{}, data, nil
	}
	yamlStart := len(data) - r.Len()
	yamlEnd := yamlStart

	for {
		line, err = r.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return []byte{}, data, nil
			}
			return nil, nil, err
		}

		if strings.TrimSpace(line) == "---" {
			yamlEnd = len(data) - r.Len()
			break
		}
	}
	return data[yamlStart:yamlEnd], data[yamlEnd:], nil
}

func readFrontBuf(filepath string) (fmdata []byte, content []byte, err error) {
	f, err := os.Open(filepath)
	if err != nil {
		log.Println(err)
	}
	defer f.Close()

	topbuf := new(bytes.Buffer)
	bottombuf := new(bytes.Buffer)
	s := bufio.NewScanner(f)
	line := 0
	start := false
	end := false
	for s.Scan() {
		//log.Println(end)
		if start && end {
			bottombuf.Write(s.Bytes())
			bottombuf.WriteString("\n")
		}
		if start && !end {
			// Anything after the --- tag, add to the topbuffer
			if s.Text() != yamlsep {
				topbuf.Write(s.Bytes())
				topbuf.WriteString("\n")
			}
			if s.Text() == yamlsep {
				end = true
			}
		}

		// Hopefully catch the first --- tag
		if s.Text() == yamlsep && !start {
			start = true
		}
		line = line + 1
	}
	//log.Println("TOP: ")
	//log.Println(topbuf.String())
	//log.Println("-----")
	//log.Println(bottombuf.String())
	return topbuf.Bytes(), bottombuf.Bytes(), nil
}

func marshalFrontmatter(fmdata []byte) (fm frontmatter, err error) {
	err = yaml.Unmarshal(fmdata, &fm)
	if err != nil {
		m := map[string]interface{}{}
		err = yaml.Unmarshal(fmdata, &m)
		if err != nil {
			return frontmatter{}, err
		}

		title, found := m["title"].(string)
		if found {
			fm.Title = title
		}
		switch v := m["Tags"].(type) {
		case string:
			fm.Tags = strings.Split(v, ",")
		case []string:
			fm.Tags = v
		default:
			fm.Tags = []string{}
		}
		favorite, found := m["favorite"].(bool)
		if found {
			fm.Favorite = favorite
		}
		public, found := m["public"].(bool)
		if found {
			fm.Public = public
		}
		admin, found := m["admin"].(bool)
		if found {
			fm.Admin = admin
		}
	}
	return fm, nil
}

func renderTemplate(w http.ResponseWriter, c context.Context, name string, data interface{}) {
	tmpl, ok := templates[name]
	if !ok {
		log.Println(fmt.Errorf("The template %s does not exist", name))
		panic(fmt.Errorf("The template %s does not exist", name))
	}

	// Create buffer to write to and check for errors
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

// This does various checks to see if an existing page exists or not
// Also checks for and returns an error on some edge cases
// So we only proceed if this returns false AND nil
// Edge cases checked for currently:
// - If name is trying to escape or otherwise a bad path
// - If name is a /directory/file combo, but /directory is actually a file
func checkName(name string) (string, error) {
	separators := regexp.MustCompile(`[ &_=+:]`)
	dashes := regexp.MustCompile(`[\-]+`)

	// First 'sanitize' the name
	//log.Println(name)
	name = strings.Replace(name, "..", "", -1)
	//log.Println(name)
	name = path.Clean(name)
	//log.Println(name)
	// Remove trailing spaces
	name = strings.Trim(name, " ")
	//log.Println(name)

	// Build the full path
	fullfilename := filepath.Join(viper.GetString("WikiDir"), name)
	// Check that the full path is relative to the configured wikidir
	_, relerr := filepath.Rel(viper.GetString("WikiDir"), fullfilename)
	if relerr != nil {
		log.Println("Unhandled relerr: ")
		log.Println(relerr)
		return "", relerr
	}

	// Normalize the name, but into a separate variable
	normalName := strings.ToLower(name)
	normalName = separators.ReplaceAllString(normalName, "-")
	normalName = dashes.ReplaceAllString(normalName, "-")
	// Check for existence of normalName now
	fullNormalFilename := filepath.Join(viper.GetString("WikiDir"), normalName)
	_, relNerr := filepath.Rel(viper.GetString("WikiDir"), fullNormalFilename)
	if relNerr != nil {
		log.Println("Unhandled relNerr: ")
		log.Println(relNerr)
		return "", relNerr
	}

	// First check if the file exists (before modifying and normalizing the filename)
	_, fierr := os.Stat(fullfilename)

	// Quick check to see if there is a directory here
	//  If so, do some quick checking on that supposed directory
	dir, _ := filepath.Split(name)
	if dir != "" {

		// Check that the base "directory" is actually a directory
		// First we get the first path element, using strings.Split
		// TODO: This should probably be made into a for loop on the strings.Split
		//    To ensure multi-level directories are protected the same
		base := strings.Split(name, "/")[0]
		basepath := filepath.Join(viper.GetString("WikiDir"), base)
		basefile, baseerr := os.Open(basepath)
		if baseerr == nil {
			basefi, basefierr := basefile.Stat()
			// I don't think these should matter
			/*
				if os.IsNotExist(basefierr) {
					//log.Println("OMG")
					log.Println("Unhandled basefierr1: ")
					log.Println(basefierr)
					return "", basefierr
				}
			*/
			if basefierr != nil && !os.IsNotExist(basefierr) {
				log.Println("Unhandled basefierr2: ")
				log.Println(basefierr)
				return "", basefierr
			}
			if basefierr == nil {
				basefimode := basefi.Mode()
				if !basefimode.IsDir() {
					//http.Error(w, basefi.Name()+" is not a directory.", 500)
					return "", ErrBaseNotDir
				}
				if basefimode.IsRegular() {
					//http.Error(w, basefi.Name()+" is not a directory.", 500)
					return "", ErrBaseNotDir
				}
			}
		}

		// Directory without specified index
		if strings.HasSuffix(name, "/") {
			//if dir != "" && name == "" {
			log.Println("This might be a directory, trying to parse the index")
			//filename := name + "index"
			//title := name + " - Index"
			//fullfilename = cfg.WikiDir + name + "index"
			fullfilename = filepath.Join(viper.GetString("WikiDir"), name, "index")

			dirindex, _ := os.Open(fullfilename)
			_, dirindexfierr := dirindex.Stat()
			if os.IsNotExist(dirindexfierr) {
				return "", ErrNoDirIndex
			}
		}
	}

	if fierr != nil {
		// If the filename as-is does not exist, we'll normalize the name before continuing
		if os.IsNotExist(fierr) {
			_, fiNerr := os.Stat(fullNormalFilename)
			if fiNerr != nil {
				// If the normalName does not exist either, return that name
				if os.IsNotExist(fiNerr) {
					return normalName, ErrNoFile
				}
				// If there is a fiNerr and it's not IsNotExist, we'll log it
				log.Println("Unhandled fiNerr: ")
				log.Println(fiNerr)
				return normalName, fiNerr
			}
			// This should mean the normalName exists, YAY!
			if fiNerr == nil {
				return normalName, nil
			}
			// If there is a fierr and it's not IsNotExist, we'll log it
		} else {
			log.Println("Unhandled fierr: ")
			log.Println(fierr)
			return normalName, fierr
		}
	}
	// This should mean the name exists, YAY!
	if fierr == nil {
		return name, nil
	}

	//return true, nil
	// Just return nothing...
	log.Println("EXCEPTION: Unaccounted for return in checkName")
	return "", nil
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "indexHandler")

	http.Redirect(w, r, "/index", http.StatusSeeOther)
	//viewHandler(w, r, "index")
}

func viewHandler(w http.ResponseWriter, r *http.Request, name string) {
	defer httputils.TimeTrack(time.Now(), "viewHandler")

	// If this is a commit, pass along the SHA1 to that function
	if r.URL.Query().Get("commit") != "" {
		commit := r.URL.Query().Get("commit")
		//utils.Debugln(r.URL.Query().Get("commit"))
		viewCommitHandler(w, r, commit, name)
		return
	}

	// Get Wiki
	p, err := loadWikiPage(r, name)
	if err != nil {
		if err == ErrNoDirIndex {
			log.Println("No such dir index...creating one.")
			http.Redirect(w, r, "/"+name+"/index", http.StatusTemporaryRedirect)
			return
		} else if err == ErrNoFile {
			log.Println("No such file...creating one.")
			//http.Redirect(w, r, "/edit/"+name, http.StatusTemporaryRedirect)
			createWiki(w, r, name)
			return
		} else if err == ErrBaseNotDir {
			log.Println("Cannot create subdir of a file.")
			http.Error(w, "Cannot create subdir of a file.", 500)
			return
		} else if err == ErrNoDirIndex {
			log.Println("No directory index. Does this even need to be an error?")
			http.Error(w, "Cannot create subdir of a file.", 500)
			return
		} else {
			httpErrorHandler(w, r, err)
		}
		http.NotFound(w, r)
		return
	}
	renderTemplate(w, r.Context(), "wiki_view.tmpl", p)
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
func loadWikiPage(r *http.Request, name string) (*wikiPage, error) {

	//log.Println("Filename: " + name)

	p := loadPage(r)

	wikip, err := loadWiki(name)
	if err != nil {
		if err == ErrNoFile {
			newwp := &wikiPage{
				page: p,
				Wiki: &wiki{
					Title:    name,
					Filename: name,
					Frontmatter: &frontmatter{
						Title: name,
					},
					CreateTime: 0,
					ModTime:    0,
				},
			}
			return newwp, err
		}
		return nil, err
	}

	// Render remaining content after frontmatter
	md := markdownRender(wikip.Content)
	//md := commonmarkRender(wikip.Content)
	//markdownRender2(wikip.Content)

	wp := &wikiPage{
		page:     p,
		Wiki:     wikip,
		Rendered: md,
	}
	return wp, nil
}

func editHandler(w http.ResponseWriter, r *http.Request, name string) {
	defer httputils.TimeTrack(time.Now(), "editHandler")

	p, err := loadWikiPage(r, name)

	if err != nil {
		if err == ErrNoFile {
			//log.Println("No such file...creating one.")
			renderTemplate(w, r.Context(), "wiki_edit.tmpl", p)
			return
		}
		httpErrorHandler(w, r, err)
	}
	renderTemplate(w, r.Context(), "wiki_edit.tmpl", p)
	return
}

func saveHandler(w http.ResponseWriter, r *http.Request, name string) {
	defer httputils.TimeTrack(time.Now(), "saveHandler")

	r.ParseForm()
	//txt := r.Body
	content := r.FormValue("editor")
	//bwiki := txt

	// Check for and install required YAML frontmatter
	title := r.FormValue("title")
	// This is the separate input that tagdog.js throws new tags into
	tags := r.FormValue("tags_all")
	favorite := r.FormValue("favorite")
	public := r.FormValue("publicPage")
	admin := r.FormValue("adminOnly")

	favoritebool := false
	if favorite == "on" {
		favoritebool = true
	}
	publicbool := false
	if public == "on" {
		publicbool = true
	}
	adminbool := false
	if admin == "on" {
		adminbool = true
	}

	if title == "" {
		title = name
	}

	var tagsA []string
	if tags != "" {
		tagsA = strings.Split(tags, ",")
	}

	fm := &frontmatter{
		Title:    title,
		Tags:     tagsA,
		Favorite: favoritebool,
		Public:   publicbool,
		Admin:    adminbool,
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

	go refreshStuff()

	auth.SetSession("flash", "Wiki page successfully saved.", w, r)
	http.Redirect(w, r, "/"+name, http.StatusSeeOther)
	log.Println(name + " page saved!")
}

func setFlash(msg string, w http.ResponseWriter, r *http.Request) {
	auth.SetSession("flash", msg, w, r)
}

func newHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "newHandler")

	pagetitle := r.FormValue("newwiki")

	//fullfilename := cfg.WikiDir + pagetitle
	/*
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
	*/
	pagetitle2, feErr := checkName(pagetitle)
	// If page does not exist, ask to create it
	if pagetitle2 != "" && feErr == ErrNoFile {
		createWiki(w, r, pagetitle2)
		//http.Redirect(w, r, "/edit/"+pagetitle, http.StatusTemporaryRedirect)
		return
	} else if feErr != nil {
		httpErrorHandler(w, r, feErr)
		return
	}

	// If pagetitle2 isn't blank, and there is no error returned by checkName,
	//   that should mean the page exists, so redirect to it.
	if pagetitle2 != "" && feErr == nil {
		http.Redirect(w, r, pagetitle2, http.StatusTemporaryRedirect)
	}

	return

}

func urlFromPath(path string) string {
	url := filepath.Clean(viper.GetString("WikiDir")) + "/"
	return strings.TrimPrefix(path, url)
}

// readFavs should read and populate favbuf, in memory
func readFavs(path string, info os.FileInfo, err error) error {

	// check and skip .git
	if info.IsDir() && info.Name() == ".git" {
		return filepath.SkipDir
	}

	// Skip directories
	if info.IsDir() {
		return nil
	}

	name := urlFromPath(path)

	// Read YAML frontmatter into fm
	// If err, just return, as file should not contain frontmatter

	fmbytes, _, err := readFileAndFront(path)
	if err != nil {
		return nil
	}
	var fm frontmatter
	fm, err = marshalFrontmatter(fmbytes)
	if err != nil {
		return nil
	}

	if fm.Favorite {
		favbuf.WriteString(name + " ")
	}

	/*
	   // Read all files in given path, check for favorite: true tag
	   if bytes.Contains(read, []byte("favorite: true")) {
	       favbuf.WriteString(name+" ")
	   }
	*/

	return nil
}

func favsHandler(favs chan []string) {
	defer httputils.TimeTrack(time.Now(), "favsHandler")

	favss := favbuf.String()
	httputils.Debugln("Favorites: " + favss)
	sfavs := strings.Fields(favss)

	favs <- sfavs
}

// readTags should read and populate tagMap, in memory
func readTags(path string, info os.FileInfo, err error) error {

	if tagMap == nil {
		tagMap = make(map[string][]string)
	}

	// check and skip .git
	if info.IsDir() && info.Name() == ".git" {
		return filepath.SkipDir
	}
	// Skip directories
	if info.IsDir() {
		return nil
	}

	name := urlFromPath(path)

	// Read YAML frontmatter into fm
	// If err, just return, as file should not contain frontmatter
	fmbytes, _, err := readFileAndFront(path)
	if err != nil {
		return nil
	}
	var fm frontmatter
	fm, err = marshalFrontmatter(fmbytes)
	if err != nil {
		return nil
	}

	if fm.Tags != nil {
		for _, tag := range fm.Tags {
			tagMap[tag] = append(tagMap[tag], name)
		}
	}

	return nil
}

func loadWiki(name string) (*wiki, error) {
	var fm frontmatter
	var pagetitle string

	// Check if file exists before doing anything else
	/*
		name, feErr := checkName(name)
		if name != "" && feErr == ErrNoFile {
			return nil, ErrNoFile
		} else if feErr != nil {
			return nil, feErr
		}
	*/

	fullfilename := filepath.Join(viper.GetString("WikiDir"), name)

	// Directory without specified index
	if strings.HasSuffix(name, "/") {
		//if dir != "" && name == "" {
		log.Println("This might be a directory, trying to parse the index")

		fullfilename = filepath.Join(viper.GetString("WikiDir"), name, "index")

		dirindex, _ := os.Open(fullfilename)
		_, dirindexfierr := dirindex.Stat()
		if os.IsNotExist(dirindexfierr) {
			return nil, ErrNoDirIndex
		}
	}

	fmbytes, content, err := readFileAndFront(fullfilename)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	fm, err = marshalFrontmatter(fmbytes)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	//log.Println(fm)
	if content == nil {
		content = []byte("")
	}

	if fm.Title != "" {
		pagetitle = fm.Title
	} else {
		pagetitle = name
	}
	ctime, err := gitGetCtime(name)
	if err != nil {
		// If not in git, redirect to gitadd
		if err == ErrNotInGit {
			return nil, err
		}
		log.Println("gitGetCtime error:")
		log.Println(err)
	}
	mtime, err := gitGetMtime(name)
	if err != nil {
		log.Println("gitGetMtime error:")
		log.Println(err)
	}

	return &wiki{
		Title:       pagetitle,
		Filename:    name,
		Frontmatter: &fm,
		Content:     content,
		CreateTime:  ctime,
		ModTime:     mtime,
	}, nil

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
			if err != nil {
				return err
			}
		}
	}

	originalFile, err := ioutil.ReadFile(fullfilename)
	if err != nil {
		log.Println("originalFile ReadFile error:")
		log.Println(err)
	}

	// Create a buffer where we build the content of the file
	buffer := new(bytes.Buffer)
	_, err = buffer.Write([]byte("---\n"))
	if err != nil {
		return err
	}
	yamlBuffer, err := yaml.Marshal(wiki.Frontmatter)
	if err != nil {
		return err
	}
	buffer.Write(yamlBuffer)
	_, err = buffer.Write([]byte("---\n"))
	if err != nil {
		return err
	}
	buffer.Write(wiki.Content)

	// Test equality of the original file, plus the buffer we just built
	log.Println(bytes.Equal(originalFile, buffer.Bytes()))
	if bytes.Equal(originalFile, buffer.Bytes()) {
		log.Println("No changes detected.")
		return nil
	}

	// Write contents of above buffer, which should be Frontmatter+WikiContent
	ioutil.WriteFile(fullfilename, buffer.Bytes(), 0755)

	gitfilename := dir + filename

	err = gitAddFilepath(gitfilename)
	if err != nil {
		return err
	}

	// FIXME: add a message box to edit page, check for it here
	err = gitCommitEmpty()
	if err != nil {
		return err
	}

	log.Println(fullfilename + " has been saved.")
	return nil

}

func loginPageHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "loginPageHandler")

	title := "login"
	p := loadPage(r)

	gp := &genPage{
		p,
		title,
	}
	renderTemplate(w, r.Context(), "login.tmpl", gp)
}

func signupPageHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "signupPageHandler")

	title := "signup"
	p := loadPage(r)

	gp := &genPage{
		p,
		title,
	}
	renderTemplate(w, r.Context(), "signup.tmpl", gp)
}

func adminUsersHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "adminUsersHandler")

	title := "admin-users"
	p := loadPage(r)

	userlist, err := auth.Userlist()
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
	renderTemplate(w, r.Context(), "admin_users.tmpl", data)

}

func adminUserHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "adminUserHandler")

	title := "admin-user"
	p := loadPage(r)

	userlist, err := auth.Userlist()
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
	renderTemplate(w, r.Context(), "admin_user.tmpl", data)
}

// Function to take a <select><option> value and redirect to a URL based on it
func adminUserPostHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	selectedUser := r.FormValue("user")
	http.Redirect(w, r, "/admin/user/"+selectedUser, http.StatusSeeOther)
}

func adminMainHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "adminMainHandler")

	title := "admin-main"
	p := loadPage(r)

	gp := &genPage{
		p,
		title,
	}
	renderTemplate(w, r.Context(), "admin_main.tmpl", gp)
}

func adminConfigHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "adminConfigHandler")

	var cfg configuration
	// To save config to toml:
	err := viper.Unmarshal(&cfg)
	//jc := conf
	if err != nil {
		log.Println(err)
	}

	title := "admin-config"
	p := loadPage(r)

	data := struct {
		*page
		Title  string
		Config configuration
	}{
		p,
		title,
		cfg,
	}
	renderTemplate(w, r.Context(), "admin_config.tmpl", data)
}

func adminConfigPostHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	log.Println(r.PostForm)
	domain := r.FormValue("domain")
	port := r.FormValue("port")
	email := r.FormValue("email")
	wikidir := r.FormValue("wikidir")
	gitrepo := r.FormValue("gitrepo")
	adminuser := r.FormValue("adminuser")
	rawPushonsave := r.FormValue("pushonsave")
	pushonsave := false
	if rawPushonsave == "on" {
		pushonsave = true
	}

	cfg := &configuration{
		Domain:     domain,
		Port:       port,
		Email:      email,
		WikiDir:    wikidir,
		GitRepo:    gitrepo,
		AdminUser:  adminuser,
		PushOnSave: pushonsave,
	}

	viper.Set("Domain", domain)
	viper.Set("Port", port)
	viper.Set("Email", email)
	viper.Set("WikiDir", wikidir)
	viper.Set("GitRepo", gitrepo)
	viper.Set("AdminUser", adminuser)
	viper.Set("PushOnSave", pushonsave)

	bo := cfg.save()
	if !bo {
		log.Println(bo)
	}
	http.Redirect(w, r, "/admin/config", http.StatusSeeOther)
	return
}

func gitCheckinHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "gitCheckinHandler")

	title := "Git Checkin"
	p := loadPage(r)

	var owithnewlines []byte

	if r.URL.Query().Get("file") != "" {
		file := r.URL.Query().Get("file")
		owithnewlines = []byte(file)
	} else {
		o, err := gitIsClean()
		if err != nil && err != ErrGitDirty {
			panic(err)
		}
		owithnewlines = bytes.Replace(o, []byte{0}, []byte(" <br>"), -1)
	}

	gp := &gitPage{
		p,
		title,
		string(owithnewlines),
		viper.GetString("GitRepo"),
	}
	renderTemplate(w, r.Context(), "git_checkin.tmpl", gp)
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

	http.Redirect(w, r, "/admin/git", http.StatusSeeOther)

}

func gitPullPostHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "gitPullPostHandler")

	err := gitPull()
	if err != nil {
		panic(err)
	}

	http.Redirect(w, r, "/admin/git", http.StatusSeeOther)

}

func adminGitHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "adminGitHandler")

	title := "Git Management"
	p := loadPage(r)

	var owithnewlines []byte

	o, err := gitIsClean()
	if err != nil && err != ErrGitDirty {
		panic(err)
	}
	owithnewlines = bytes.Replace(o, []byte{0}, []byte(" <br>"), -1)

	gp := &gitPage{
		p,
		title,
		string(owithnewlines),
		viper.GetString("GitRepo"),
	}
	renderTemplate(w, r.Context(), "admin_git.tmpl", gp)
}

// Middleware to check for "dirty" git repo
func checkWikiGit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		_, err := gitIsClean()
		if err != nil {
			log.Println("There are wiki files waiting to be checked in.")
			http.Redirect(w, r, "/gitadd", http.StatusSeeOther)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func tagMapHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "tagMapHandler")

	a := &tagMap

	p := loadPage(r)

	tagpage := &tagMapPage{
		page:    p,
		TagKeys: *a,
	}
	renderTemplate(w, r.Context(), "tag_list.tmpl", tagpage)
}

func createWiki(w http.ResponseWriter, r *http.Request, name string) {
	//username, _ := auth.GetUsername(r.Context())
	//if username != "" {
	if auth.IsLoggedIn(r.Context()) {
		w.WriteHeader(404)
		//title := "Create " + name + "?"
		p := loadPage(r)

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
		renderTemplate(w, r.Context(), "wiki_create.tmpl", wp)
		return
	}

	auth.SetSession("flash", "Please login to view that page.", w, r)
	h := viper.GetString("Domain")
	http.Redirect(w, r, "https://"+h+"/login"+"?url="+r.URL.String(), http.StatusSeeOther)
	return

}

func HealthCheckHandler(w http.ResponseWriter, r *http.Request) {
	// A very simple health check.
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")

	// In the future we could report back on the status of our DB, or our cache
	// (e.g. Redis) by performing a simple PING, and include them in the response.
	io.WriteString(w, `{"alive": true}`)
}

func checkFiletype(fullname string) string {
	// Detect filetype first
	file, err := os.Open(fullname)
	if err != nil {
		log.Println(err)
	}
	buff := make([]byte, 512)
	_, err = file.Read(buff)
	if err != nil {
		log.Println(err)
	}
	filetype := http.DetectContentType(buff)

	//log.Println(string(buff[:3]))
	//switch filetype {
	//case "application/octet-stream":
	// Try and detect mis-detected wiki pages
	if bytes.Equal(buff[:3], []byte("---")) {
		filetype = "text/plain; charset=utf-8"
	}
	//}
	log.Println(filetype)
	return filetype
}

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

		if name != "" && feErr == ErrNoFile {
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
		filetype := checkFiletype(fullname)
		if filetype != "text/plain; charset=utf-8" {

			http.ServeFile(w, r, fullname)
		}

		// Read YAML frontmatter into fm
		// If err, just return, as file should not contain frontmatter
		fmbytes, _, fmberr := readFileAndFront(fullname)
		if fmberr != nil {
			log.Println(fmberr)
			return
		}
		fm, fmerr := marshalFrontmatter(fmbytes)
		if fmerr != nil {
			log.Println("YAML unmarshal error in: " + name)
			return
		}

		// If user is logged in, check if wiki git repo is clean, then continue
		//if username != "" {
		if auth.IsLoggedIn(r.Context()) {
			_, err := gitIsClean()
			if err != nil {
				log.Println("There are wiki files waiting to be checked in.")
				http.Redirect(w, r, "/gitadd", http.StatusSeeOther)
				return
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
			auth.SetSession("flash", "Sorry, you are not allowed to see that.", w, r)
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
			auth.SetSession("flash", "Please login to view that page.", w, r)
			http.Redirect(w, r, "http://"+r.Host+"/login"+"?url="+rurl, http.StatusSeeOther)
			return
		}

	}
}

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

func riceInit() error {
	// Parent templates directory named 'templates'
	templateBox, err := rice.FindBox("templates")
	if err != nil {
		return err
	}
	// Child directory 'templates/includes' containing the base templates
	includes, err := templateBox.Open("includes")
	if err != nil {
		return err
	}
	includeDir, err := includes.Readdir(-1)
	if err != nil {
		return err
	}
	// Child directory 'templates/layouts' containing individual page layouts
	layouts, err := templateBox.Open("layouts")
	if err != nil {
		return err
	}
	layoutsDir, err := layouts.Readdir(-1)
	if err != nil {
		return err
	}
	var boxT []string
	var templateIBuff bytes.Buffer
	for _, v := range includeDir {
		boxT = append(boxT, "includes/"+v.Name())
		iString, _ := templateBox.String("includes/" + v.Name())
		templateIBuff.WriteString(iString)
	}

	funcMap := template.FuncMap{"prettyDate": httputils.PrettyDate, "safeHTML": httputils.SafeHTML, "imgClass": httputils.ImgClass, "isLoggedIn": isLoggedIn, "jsTags": jsTags}

	// Here we are prefacing every layout with what should be every includes/ .tmpl file
	// Ex: includes/sidebar.tmpl includes/bottom.tmpl includes/base.tmpl layouts/list.tmpl
	// **THIS IS VERY IMPORTANT TO ALLOW MY BASE TEMPLATE TO WORK**
	for _, layout := range layoutsDir {
		boxT = append(boxT, "layouts/"+layout.Name())
		//DEBUG TEMPLATE LOADING
		//utils.Debugln(files)
		lString, _ := templateBox.String("layouts/" + layout.Name())
		fstring := templateIBuff.String() + lString
		templates[layout.Name()] = template.Must(template.New(layout.Name()).Funcs(funcMap).Parse(fstring))
	}
	return nil
}

func authInit(authDB string) error {
	auth.Authdb = auth.Open(authDB)
	autherr := auth.AuthDbInit()
	if autherr != nil {
		return autherr
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
		if fInit {
			log.Println("-init flag is given. Cloning " + viper.GetString("GitRepo") + "into " + wikidir + "...")
			gitClone(viper.GetString("GitRepo"))
		} else {
			repoNotExistErr := errors.New("Clone/move your existing repo here, change the config, or run with -init to clone a specified remote repo.")
			//log.Fatalln("Clone/move your existing repo here, change the config, or run with -init to clone a specified remote repo.")
			panic(repoNotExistErr)
		}
	}
}

func bleveIndex() {

	var err error
	timestamp := "2006-01-02 at 03:04:05PM"
	index, err = bleve.Open("./data/index.bleve")

	log.Println("Search crawling: started")

	// If index exists, crawl and re-index
	if err == nil {
		fileList, flerr := gitLs()
		if flerr != nil {
			panic(err)
		}
		// TODO: Turn this into a bleve.Batch() job!
		for _, file := range fileList {
			fullname := filepath.Join(viper.GetString("WikiDir"), file)
			src, err := os.Stat(fullname)
			if err != nil {
				panic(err)
			}
			// If not a directory, get frontmatter from file and add to list
			if !src.IsDir() {

				//log.Println(file)
				_, filename := filepath.Split(file)
				//var wp *wiki
				var fm frontmatter
				var pagetitle string

				// Read YAML frontmatter into fm
				fmbytes, content, err := readFileAndFront(fullname)
				if err != nil {
					log.Println(err)
				}
				fm, err = marshalFrontmatter(fmbytes)
				if err != nil {
					log.Println("YAML unmarshal error in: " + file)
					log.Println(err)
				}
				if fm.Public {
					//log.Println("Private page!")
				}
				pagetitle = filename
				if fm.Title != "" {
					pagetitle = fm.Title
				}
				if fm.Title == "" {
					fm.Title = file
				}
				if fm.Public != true {
					fm.Public = false
				}
				if fm.Admin != true {
					fm.Admin = false
				}
				if fm.Favorite != true {
					fm.Favorite = false
				}
				if fm.Tags == nil {
					fm.Tags = []string{}
				}

				ctime, err := gitGetCtime(file)
				if err != nil && err != ErrNotInGit {
					log.Println(err)
				}
				mtime, err := gitGetMtime(file)
				if err != nil {
					log.Println(err)
				}

				data := struct {
					Name     string `json:"name"`
					Public   bool
					Tags     []string `json:"tags"`
					Content  string   `json:"content"`
					Created  string
					Modified string
				}{
					Name:     pagetitle,
					Public:   fm.Public,
					Tags:     fm.Tags,
					Content:  string(content),
					Created:  time.Unix(ctime, 0).Format(timestamp),
					Modified: time.Unix(mtime, 0).Format(timestamp),
				}

				index.Index(file, data)

			}
		}
	}

	// If index does not exist, create mapping and then index
	if err == bleve.ErrorIndexPathDoesNotExist {
		nameMapping := bleve.NewTextFieldMapping()
		nameMapping.Analyzer = keyword_analyzer.Name

		contentMapping := bleve.NewTextFieldMapping()
		contentMapping.Analyzer = en.AnalyzerName

		boolMapping := bleve.NewBooleanFieldMapping()

		dateMapping := bleve.NewDateTimeFieldMapping()
		//dateMapping.DateFormat = timestamp

		tagMapping := bleve.NewTextFieldMapping()
		tagMapping.Analyzer = keyword_analyzer.Name

		wikiMapping := bleve.NewDocumentMapping()
		wikiMapping.AddFieldMappingsAt("Name", nameMapping)
		wikiMapping.AddFieldMappingsAt("Tags", tagMapping)
		wikiMapping.AddFieldMappingsAt("Content", contentMapping)
		wikiMapping.AddFieldMappingsAt("Public", boolMapping)
		wikiMapping.AddFieldMappingsAt("Created", dateMapping)
		wikiMapping.AddFieldMappingsAt("Modified", dateMapping)

		indexMapping := bleve.NewIndexMapping()
		indexMapping.AddDocumentMapping("PublicWiki", wikiMapping)
		indexMapping.TypeField = "type"
		indexMapping.DefaultAnalyzer = "en"

		index, err = bleve.New("./data/index.bleve", indexMapping)
		if err != nil {
			panic(err)
		}

		fileList, flerr := gitLs()
		if flerr != nil {
			panic(err)
		}
		// TODO: Turn this into a bleve.Batch() job!
		for _, file := range fileList {
			fullname := filepath.Join(viper.GetString("WikiDir"), file)
			src, err := os.Stat(fullname)
			if err != nil {
				panic(err)
			}
			// If not a directory, get frontmatter from file and add to list
			if !src.IsDir() {

				//log.Println(file)
				_, filename := filepath.Split(file)
				//var wp *wiki
				var fm frontmatter
				var pagetitle string

				// Read YAML frontmatter into fm
				fmbytes, content, err := readFileAndFront(fullname)
				if err != nil {
					log.Println(err)
				}
				fm, err = marshalFrontmatter(fmbytes)
				if err != nil {
					log.Println("YAML unmarshal error in: " + file)
					log.Println(err)
				}
				if fm.Public {
					//log.Println("Private page!")
				}
				pagetitle = filename
				if fm.Title != "" {
					pagetitle = fm.Title
				}
				if fm.Title == "" {
					fm.Title = file
				}
				if fm.Public != true {
					fm.Public = false
				}
				if fm.Admin != true {
					fm.Admin = false
				}
				if fm.Favorite != true {
					fm.Favorite = false
				}
				if fm.Tags == nil {
					fm.Tags = []string{}
				}

				ctime, err := gitGetCtime(file)
				if err != nil && err != ErrNotInGit {
					log.Println(err)
				}
				mtime, err := gitGetMtime(file)
				if err != nil {
					log.Println(err)
				}

				data := struct {
					Name     string `json:"name"`
					Public   bool
					Tags     []string `json:"tags"`
					Content  string   `json:"content"`
					Created  string
					Modified string
				}{
					Name:     pagetitle,
					Public:   fm.Public,
					Tags:     fm.Tags,
					Content:  string(content),
					Created:  time.Unix(ctime, 0).Format(timestamp),
					Modified: time.Unix(mtime, 0).Format(timestamp),
				}

				index.Index(file, data)

			}
		}

	}

	log.Println("Search crawling: done")

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

func search(w http.ResponseWriter, r *http.Request) {
	params := getParams(r.Context())
	name := params["name"]

	// If this is a POST request, and searchwiki form is not blank,
	//  set name to its' value
	if r.Method == "POST" {
		r.ParseForm()
		if r.PostFormValue("searchwiki") != "" {
			name = r.PostFormValue("searchwiki")
		}
	}

	p := loadPage(r)

	//query := bleve.NewMatchQuery(name)

	must := make([]bleve.Query, 1)
	mustNot := make([]bleve.Query, 1)
	must[0] = bleve.NewMatchQuery(name)

	public := false

	//username, _ := auth.GetUsername(r.Context())
	//if username != "" {
	if auth.IsLoggedIn(r.Context()) {
		public = true
	}

	mustNot[0] = bleve.NewBoolFieldQuery(public)
	mustNot[0].SetField("Public")

	query := bleve.NewBooleanQuery(must, nil, mustNot)

	searchRequest := bleve.NewSearchRequest(query)

	searchRequest.Highlight = bleve.NewHighlight()

	searchResult, _ := index.Search(searchRequest)
	//log.Println(searchResult.String())

	var results []*result

	//var results map[string]string
	for _, v := range searchResult.Hits {
		for _, fragments := range v.Fragments {
			for _, fragment := range fragments {
				var r *result
				r = &result{
					Name:   v.ID,
					Result: fragment,
				}
				//results[v.ID] = fragment
				results = append(results, r)
			}
		}
	}

	s := &searchPage{p, results}
	renderTemplate(w, r.Context(), "search_results.tmpl", s)
}

// crawlWiki builds a list of wiki pages, stored in memory
//  saves time to reference this, rebuilding on saving
func crawlWiki() {

	fileList, err := gitLs()
	if err != nil {
		panic(err)
	}
	var wps []*wiki
	var publicwps []*wiki
	var adminwps []*wiki
	for _, file := range fileList {

		// If using Git, build the full path:
		fullname := filepath.Join(viper.GetString("WikiDir"), file)
		//file = viper.GetString("WikiDir")+file
		//log.Println(file)
		//log.Println(fullname)

		// check if the source dir exist
		src, err := os.Stat(fullname)
		if err != nil {
			panic(err)
		}
		// If not a directory, get frontmatter from file and add to list
		if !src.IsDir() {

			_, filename := filepath.Split(file)

			// If this is an absolute path, including the cfg.WikiDir, trim it
			//withoutdotslash := strings.TrimPrefix(viper.GetString("WikiDir"), "./")
			//fileURL := strings.TrimPrefix(file, withoutdotslash)

			var wp *wiki
			var fm frontmatter
			var pagetitle string

			//log.Println(file)

			// Read YAML frontmatter into fm
			fmbytes, _, err := readFileAndFront(fullname)
			if err != nil {
				log.Println(err)
			}
			fm, err = marshalFrontmatter(fmbytes)
			if err != nil {
				log.Println("YAML unmarshal error in: " + file)
				log.Println(err)
			}

			if fm.Public {
				//log.Println("Private page!")
			}
			pagetitle = filename
			if fm.Title != "" {
				pagetitle = fm.Title
			}
			if fm.Title == "" {
				fm.Title = file
			}
			if fm.Public != true {
				fm.Public = false
			}
			if fm.Admin != true {
				fm.Admin = false
			}
			if fm.Favorite != true {
				fm.Favorite = false
			}
			if fm.Tags == nil {
				fm.Tags = []string{}
			}
			ctime, err := gitGetCtime(file)
			if err != nil && err != ErrNotInGit {
				log.Println(err)
			}
			mtime, err := gitGetMtime(file)
			if err != nil {
				log.Println(err)
			}

			// If pages are Admin or Public, add to a separate wikiPage slice
			//   So we only check on rendering
			if fm.Admin {
				wp = &wiki{
					Title:       pagetitle,
					Filename:    file,
					Frontmatter: &fm,
					CreateTime:  ctime,
					ModTime:     mtime,
				}
				adminwps = append(adminwps, wp)
			} else if fm.Public {
				wp = &wiki{
					Title:       pagetitle,
					Filename:    file,
					Frontmatter: &fm,
					CreateTime:  ctime,
					ModTime:     mtime,
				}
				publicwps = append(publicwps, wp)
			} else {
				wp = &wiki{
					Title:       pagetitle,
					Filename:    file,
					Frontmatter: &fm,
					CreateTime:  ctime,
					ModTime:     mtime,
				}
				wps = append(wps, wp)
			}
			//log.Println(string(body))
			//log.Println(string(wp.wiki.Content))
		}

	}
	if wikiList == nil {
		wikiList = make(map[string][]*wiki)
	}
	wikiList["public"] = publicwps
	wikiList["admin"] = adminwps
	wikiList["private"] = wps
}

// This should be all the stuff we need to be refreshed on startup and when pages are saved
func refreshStuff() {
	// Update search index
	go bleveIndex()

	// Update list of wiki pages
	go crawlWiki()

	// Crawl for new favorites only on startup and save
	log.Println("Fav crawling: started")
	err := filepath.Walk(viper.GetString("WikiDir"), readFavs)
	if err != nil {
		log.Println("init: unable to crawl for favorites")
	}
	log.Println("Fav crawling: done")

	// Crawl for tags only on startup and save
	log.Println("Tag crawling: started")
	err = filepath.Walk(viper.GetString("WikiDir"), readTags)
	if err != nil {
		log.Println("init: unable to crawl for tags")
	}
	log.Println("Tag crawling: done")
}

func markdownPreview(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	//log.Println(r.FormValue("md"))
	w.Write([]byte(markdownRender([]byte(r.FormValue("md")))))
}

func main() {

	//viper.WatchConfig()

	flag.Parse()

	httputils.AssetsBox = rice.MustFindBox("assets")
	auth.AdminUser = viper.GetString("AdminUser")

	// Open and initialize auth database
	err := authInit("./data/auth.db")
	if err != nil {
		log.Fatalln(err)
	}
	defer auth.Authdb.Close()

	err = riceInit()
	if err != nil {
		log.Fatalln(err)
	}

	initWikiDir()

	refreshStuff()

	csrfSecure := true
	if fLocal {
		csrfSecure = false
	}

	// HTTP stuff from here on out
	s := alice.New(timer, httputils.Logger, auth.UserEnvMiddle, csrf.Protect([]byte("c379bf3ac76ee306cf72270cf6c5a612e8351dcb"), csrf.Secure(csrfSecure)))

	h := httptreemux.New()
	//h.PanicHandler = httptreemux.ShowErrorsPanicHandler
	h.PanicHandler = errorHandler
	//r.PanicHandler = errorHandler

	statsdata := stats.New()

	r := h.UsingContext()

	r.GET("/", indexHandler)

	r.GET("/tags", tagMapHandler)
	/*r.GET("/panic", func(w http.ResponseWriter, r *http.Request) {
		panic("Unexpected error!")
		//http.Error(w, panic("Unexpected error!"), http.StatusInternalServerError)
	})*/

	r.POST("/new", auth.AuthMiddle(newHandler))
	//r.HandleFunc("/login", auth.LoginPostHandler).Methods("POST")
	r.GET("/login", loginPageHandler)
	//r.HandleFunc("/logout", auth.LogoutHandler).Methods("POST")
	r.GET("/logout", auth.LogoutHandler)
	//r.GET("/signup", signupPageHandler)
	r.GET("/list", listHandler)
	r.GET("/search/*name", search)
	r.POST("/search", search)
	r.GET("/recent", auth.AuthMiddle(recentHandler))
	r.GET("/health", HealthCheckHandler)

	admin := h.NewGroup("/admin").UsingContext()
	admin.GET("/", auth.AuthAdminMiddle(adminMainHandler))
	admin.GET("/config", auth.AuthAdminMiddle(adminConfigHandler))
	admin.POST("/config", auth.AuthAdminMiddle(adminConfigPostHandler))
	admin.GET("/git", auth.AuthAdminMiddle(adminGitHandler))
	admin.POST("/git/push", auth.AuthAdminMiddle(gitPushPostHandler))
	admin.POST("/git/checkin", auth.AuthAdminMiddle(gitCheckinPostHandler))
	admin.POST("/git/pull", auth.AuthAdminMiddle(gitPullPostHandler))
	admin.GET("/users", auth.AuthAdminMiddle(adminUsersHandler))
	admin.POST("/users", auth.AuthAdminMiddle(auth.UserSignupPostHandler))
	admin.POST("/user", auth.AuthAdminMiddle(adminUserPostHandler))
	admin.GET("/user/:username", auth.AuthAdminMiddle(adminUserHandler))
	admin.POST("/user/:username", auth.AuthAdminMiddle(adminUserHandler))
	admin.POST("/user/password_change", auth.AuthAdminMiddle(auth.AdminUserPassChangePostHandler))
	admin.POST("/user/delete", auth.AuthAdminMiddle(auth.AdminUserDeletePostHandler))

	a := h.NewGroup("/auth").UsingContext()
	a.POST("/login", auth.LoginPostHandler)
	a.POST("/logout", auth.LogoutHandler)
	a.GET("/logout", auth.LogoutHandler)
	//a.POST("/signup", auth.SignupPostHandler)

	//r.HandleFunc("/signup", auth.SignupPostHandler).Methods("POST")

	r.POST("/gitadd", auth.AuthMiddle(gitCheckinPostHandler))
	r.GET("/gitadd", auth.AuthMiddle(gitCheckinHandler))

	r.GET("/md_render", markdownPreview)

	r.GET("/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		stats := statsdata.Data()
		b, _ := json.Marshal(stats)
		w.Write(b)
	})

	r.GET("/uploads/*", treeMuxWrapper(http.StripPrefix("/uploads/", http.FileServer(http.Dir("uploads")))))

	r.GET(`/edit/*name`, auth.AuthMiddle(wikiHandler(editHandler)))
	r.POST(`/save/*name`, auth.AuthMiddle(wikiHandler(saveHandler)))
	r.GET(`/history/*name`, wikiHandler(historyHandler))
	r.GET(`/*name`, wikiHandler(viewHandler))

	http.HandleFunc("/robots.txt", httputils.RobotsHandler)
	http.HandleFunc("/favicon.ico", httputils.FaviconHandler)
	http.HandleFunc("/favicon.png", httputils.FaviconHandler)
	http.HandleFunc("/assets/", httputils.StaticHandler)
	http.Handle("/", s.Then(h))

	log.Println("Listening on port " + viper.GetString("Port"))
	http.ListenAndServe("0.0.0.0:"+viper.GetString("Port"), nil)

}
