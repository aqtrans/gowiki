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

// x GUI for Tags - taggle.js should do this for me
// x LDAP integration
// - Buttons
// x Private pages
// - Tests

// YAML frontmatter based on http://godoc.org/j4k.co/fmatter

// Markdown stuff from https://raw.githubusercontent.com/gogits/gogs/master/modules/markdown/markdown.go

import (
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
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"bufio"

	"github.com/justinas/alice"
	"github.com/oxtoacart/bpool"
	"github.com/russross/blackfriday"
	//"github.com/rhinoman/go-commonmark"
	//bf "gopkg.in/russross/blackfriday.v2"
	"github.com/microcosm-cc/bluemonday"
	"github.com/spf13/viper"
	"github.com/thoas/stats"
	"gopkg.in/yaml.v2"
	"jba.io/go/auth"
	"jba.io/go/utils"
	"context"
	"github.com/dimfeld/httptreemux"
	"github.com/GeertJohan/go.rice"
	"regexp"
	"github.com/BurntSushi/toml"
	"github.com/aqtrans/ctx-csrf"
	"github.com/blevesearch/bleve"
	"github.com/blevesearch/bleve/analysis/analyzers/keyword_analyzer"
	"github.com/blevesearch/bleve/analysis/language/en"
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

func markdownCommon(input []byte) []byte {
	renderer := blackfriday.HtmlRenderer(commonHtmlFlags, "", "")
	return blackfriday.MarkdownOptions(input, renderer, blackfriday.Options{
		Extensions: commonExtensions})
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
	linkPattern = regexp.MustCompile(`\[\/[0-9a-zA-Z-_\.\/]+\]\(\)`)
	bufpool   *bpool.BufferPool
	templates map[string]*template.Template
	_24K      int64 = (1 << 20) * 24
	fLocal    bool
	debug     = utils.Debug
	fInit     bool
	gitPath   string
	favbuf    bytes.Buffer
	tagMap  map[string][]string
	tagsBuf bytes.Buffer
	index bleve.Index
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
	Title    string
	Filename string
	Frontmatter *frontmatter
	Content  []byte
	CreateTime int64
	ModTime    int64	
}

type wikiPage struct {
	*page
	Wiki *wiki
	Rendered string
}

type commitPage struct {
	*page
	Wiki *wiki
	Commit     string
	Rendered    string
	Diff       string
}
type listPage struct {
	*page
	Wikis        []*wiki
	PublicWikis  []*wiki
	AdminWikis   []*wiki
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
	Wiki *wiki
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
	flag.BoolVar(&utils.Debug, "d", false, "Enabled debug logging")
	//Flag '-init' enables pulling of remote git repo into wikiDir
	flag.BoolVar(&fInit, "init", false, "Enable auto-cloning of remote wikiDir")

	bufpool = bpool.NewBufferPool(64)
	if templates == nil {
		templates = make(map[string]*template.Template)
	}

	gitPath, _ = exec.LookPath("git")
	/*if err != nil {
		log.Fatal("git must be installed")
	}*/
	/*
	templatesDir := "./templates/"
	layouts, err := filepath.Glob(templatesDir + "layouts/*.tmpl")
	if err != nil {
		log.Fatal(err)
	}
	includes, err := filepath.Glob(templatesDir + "includes/*.tmpl")
	if err != nil {
		log.Fatal(err)
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

func timeFromContext(c context.Context) (time.Time, bool) {
	t, ok := c.Value(TimerKey).(time.Time)
	return t, ok
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

// Special Markdown render helper to convert [/empty/wiki/links]() to a full <a href> link
// Borrowed most of this from https://raw.githubusercontent.com/gogits/gogs/master/modules/markdown/markdown.go
func replaceInterwikiLinks(rawBytes []byte, urlPrefix string) []byte {
	ms := linkPattern.FindAll(rawBytes, -1)
	for _, m := range ms {
		m2 := bytes.TrimPrefix(m, []byte("["))
		m2 = bytes.TrimSuffix(m2, []byte("]()"))
		//log.Println(string(m2))
		rawBytes = []byte(fmt.Sprintf(`<a href="%s%s">%s</a>`, urlPrefix, m2, m2, ))
		//rawBytes = link
	}
	return rawBytes
}

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
		return o, errors.New("directory is dirty")
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
		return 0, errors.New("NOT_IN_GIT")
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
			log.Fatalln(err)
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

func loadPage(r *http.Request) (*page, error) {
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
	return &page{SiteName: "GoWiki", Favs: gofavs, UN: user, IsAdmin: isAdmin, Token: token, FlashMsg: message}, nil
}

func historyHandler(w http.ResponseWriter, r *http.Request, name string) {

	p, err := loadPage(r)
	if err != nil {
		log.Fatalln(err)
	}

	wikip, wikierr := loadWiki(name)
	if wikierr != nil {
		log.Fatalln(err)
	}

	history, err := gitGetFileLog(name)
	if err != nil {
		log.Fatalln(err)
	}
	hp := &historyPage{
		p,
		wikip,
		name,
		history,
	}
	err = renderTemplate(w, r.Context(), "wiki_history.tmpl", hp)
	if err != nil {
		log.Fatalln(err)
	}
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

	p, err := loadPage(r)
	if err != nil {
		log.Fatalln(err)
	}
	body, err := gitGetFileCommit(name, commit)
	if err != nil {
		log.Fatalln(err)
	}
	ctime, err := gitGetCtime(name)
	if err != nil && err.Error() != "NOT_IN_GIT" {
		log.Panicln(err)
	}
	mtime, err := gitGetFileCommitMtime(commit)
	if err != nil {
		log.Panicln(err)
	}
	diff, err := gitGetFileCommitDiff(name, commit)
	if err != nil {
		log.Fatalln(err)
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
	if fm.Public {
		log.Println("Public page!")
	}
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
			Title: pagetitle,
			Filename: name,
			Frontmatter: &fm,
			Content:  content,
			CreateTime: ctime,
			ModTime: mtime,
		},
		Commit: commit,
		Rendered: pageContent,
		Diff: diffstring,
	}

	err = renderTemplate(w, r.Context(), "wiki_commit.tmpl", cp)
	if err != nil {
		log.Fatalln(err)
	}

}

// TODO: Fix this
func recentHandler(w http.ResponseWriter, r *http.Request) {
	
	p, err := loadPage(r)
	if err != nil {
		log.Fatalln(err)
	}
	

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

	log.Println(gh[0])
	for _, v := range gh {
		split = strings.Split(strings.TrimSpace(v), " ")
		date, err := strconv.ParseInt(split[0], 0, 64)
		if err != nil {
			log.Fatal(err)
		}

		// If there is a filename (initial one will not have it)...
		split2 = strings.Split(split[1], "\n")
		if len(split2) >= 2 {

			r := &recent{
				Date: date,
				Commit: split2[0],
				Filenames: strings.Split(split2[1], "\n"),
			}
			//w.Write([]byte(v + "<br>"))
			recents = append(recents, r)
		}
	}

	s := &recentsPage{p, recents}
	err = renderTemplate(w, r.Context(), "recents.tmpl", s)
	if err != nil {
		log.Fatalln(err)
	}

}

func listHandler(w http.ResponseWriter, r *http.Request) {
	//searchDir := cfg.WikiDir

	p, err := loadPage(r)
	if err != nil {
		log.Fatalln(err)
	}

	// ~~Currently doing a filepath.Walk over cfg.WikiDir to build a list of wiki pages~~
	// ~~But since we use git...should we use git to retrieve the list?~~
	fileList := []string{}
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
	
	fileList, flerr := gitLs()
	if flerr != nil {
		log.Fatalln(err)
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
		log.Println(file)
		log.Println(fullname)

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
			if err != nil && err.Error() != "NOT_IN_GIT" {
				log.Panicln(err)
			}
			mtime, err := gitGetMtime(file)
			if err != nil {
				log.Panicln(err)
			}

			// If pages are Admin or Public, add to a separate wikiPage slice
			//   So we only check on rendering
			if fm.Admin {
				wp = &wiki{
					Title: pagetitle,
					Filename: file,
					Frontmatter: &fm,
					CreateTime: ctime,
					ModTime: mtime,
				}
				adminwps = append(adminwps, wp)
			} else if fm.Public {
				wp = &wiki{
					Title: pagetitle,
					Filename: file,
					Frontmatter: &fm,
					CreateTime: ctime,
					ModTime: mtime,
				}
				publicwps = append(publicwps, wp)
			} else {
				wp = &wiki{
					Title: pagetitle,
					Filename: file,
					Frontmatter: &fm,
					CreateTime: ctime,
					ModTime: mtime,
				}
				wps = append(wps, wp)
			}
			//log.Println(string(body))
			//log.Println(string(wp.wiki.Content))
		}

	}
	l := &listPage{p, wps, publicwps, adminwps}
	err = renderTemplate(w, r.Context(), "list.tmpl", l)
	if err != nil {
		log.Fatalln(err)
	}
}

func readFile(filepath string) []byte {
	data, err := ioutil.ReadFile(filepath)
	if err != nil {
		log.Fatalln(err)
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
        line = line+1
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

func renderTemplate(w http.ResponseWriter, c context.Context, name string, data interface{}) error {
	tmpl, ok := templates[name]
	if !ok {
		return fmt.Errorf("The template %s does not exist", name)
	}

	// Create buffer to write to and check for errors
	buf := bufpool.Get()
	err := tmpl.ExecuteTemplate(buf, "base", data)
	if err != nil {
		log.Println("renderTemplate error:")
		log.Println(err)
		bufpool.Put(buf)
		return err
	}

	// Set the header and write the buffer to w
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Squeeze in our response time here
	// Real hacky solution, but better than modifying the struct
	start, ok := timeFromContext(c)
	if !ok {
		utils.Debugln("No startTime in context.")
		start = time.Now()
	}
	elapsed := time.Since(start)
	//buf.WriteString(elapsed.String())
	buf2 := bufpool.Get()
	err = tmpl.ExecuteTemplate(buf2, "footer", elapsed.String())
	if err != nil {
		log.Println("renderTemplate error:")
		log.Println(err)
		bufpool.Put(buf2)
		return err
	}
	buf3 := bufpool.Get()
	err = tmpl.ExecuteTemplate(buf3, "bottom", data)
	if err != nil {
		log.Println("renderTemplate error:")
		log.Println(err)
		bufpool.Put(buf3)
		return err
	}	

	buf.WriteTo(w)
	buf2.WriteTo(w)
	buf3.WriteTo(w)
	bufpool.Put(buf)
	bufpool.Put(buf2)
	bufpool.Put(buf3)
	return nil
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
func doesPageExist(name string) (bool, error) {
	//fullfilename := cfg.WikiDir + name
	fullfilename := filepath.Join(viper.GetString("WikiDir"), name) 
	rel, err := filepath.Rel(viper.GetString("WikiDir"), fullfilename)
	if err != nil {
		return false, err
	}
	if strings.HasPrefix(rel, "../") {
		return false, errors.New("BAD_PATH")
	}
	base := filepath.Dir(fullfilename)

	//log.Println(base)
	//log.Println(filename)

	_, fierr := os.Stat(fullfilename)
	if fierr != nil {
		//log.Println(fierr)
	}

	// Check if the base of the given filename is actually a file
	// If so, bail, return 500.
	basefile, err := os.Open("./" + base)
	if err != nil {
		return false, err
	}
	basefi, basefierr := basefile.Stat()
	// I don't think these should matter
	if os.IsNotExist(basefierr) {
		//log.Println("OMG")
		return false, basefierr
	}
	if basefierr != nil {
		return false, basefierr
	}
	if basefierr == nil {
		basefimode := basefi.Mode()
		if !basefimode.IsDir() {
			errn := errors.New("Base is not dir")
			//http.Error(w, basefi.Name()+" is not a directory.", 500)
			return false, errn
		}
		if basefimode.IsRegular() {
			errn := errors.New("Base is not dir")
			//http.Error(w, basefi.Name()+" is not a directory.", 500)
			return false, errn
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
			errn := errors.New("No such dir index")
			return false, errn
		}
	}

	if os.IsNotExist(fierr) {
		// NOW: Using os.Stat to properly check for file existence, using IsNotExist()
		// This should mean file is non-existent, so create new page
		//log.Println(fierr)
		//errn := errors.New("No such file")
		return false, nil
	}

	return true, nil
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	defer utils.TimeTrack(time.Now(), "indexHandler")

	//http.Redirect(w, r, "/index", http.StatusSeeOther)
	viewHandler(w, r, "index")
}

func viewHandler(w http.ResponseWriter, r *http.Request, name string) {
	defer utils.TimeTrack(time.Now(), "viewHandler")

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
		if err.Error() == "No such dir index" {
			log.Println("No such dir index...creating one.")
			http.Redirect(w, r, "/"+name+"/index", http.StatusTemporaryRedirect)
			return
		} else if err.Error() == "NO_SUCH_FILE" {
			log.Println("No such file...creating one.")
			//http.Redirect(w, r, "/edit/"+name, http.StatusTemporaryRedirect)
			createWiki(w, r, name)
			return
		} else if err.Error() == "Base is not dir" {
			log.Println("Cannot create subdir of a file.")
			http.Error(w, "Cannot create subdir of a file.", 500)
			return
			// If gitGetCtime returns NOT_IN_GIT, we handle it here
		//} else if err.Error() == "NOT_IN_GIT" {
		//	http.Redirect(w, r, "/gitadd?file="+name, http.StatusSeeOther)
		//	return
		} else if err.Error() == "NO_DIR_INDEX" {
			log.Println("No directory index. Does this even need to be an error?")
			http.Error(w, "Cannot create subdir of a file.", 500)
			return
		} else {
			//panic(err)
			log.Println("loadWikiPage error:")
			log.Println(err)
		}
		//log.Println(err.Error())
		http.NotFound(w, r)
		return
	}
	err = renderTemplate(w, r.Context(), "wiki_view.tmpl", p)
	if err != nil {
		//panic(err)
		log.Println("wiki_view error:")
		log.Println(err)
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
func loadWikiPage(r *http.Request, name string) (*wikiPage, error) {

	//log.Println("Filename: " + name)

	p, err := loadPage(r)
	if err != nil {
		log.Fatalln(err)
	}

	wikip, wikierr := loadWiki(name)
	if wikierr != nil {
		if wikierr.Error() == "NO_SUCH_FILE" {
			newwp := &wikiPage{
				page: p,
				Wiki: &wiki{
					Title: name,
					Filename: name,
					Frontmatter: &frontmatter{
						Title: name,
					},
					CreateTime: 0,
					ModTime: 0,
				},
			}
			return newwp, wikierr
		}
		return nil, wikierr
	}

	// Render remaining content after frontmatter
	md := markdownRender(wikip.Content)
	//md := commonmarkRender(wikip.Content)
	//markdownRender2(wikip.Content)

	wp := &wikiPage{
		page: p,
		Wiki: wikip,
		Rendered: md,
	}
	return wp, nil
}


func editHandler(w http.ResponseWriter, r *http.Request, name string) {
	defer utils.TimeTrack(time.Now(), "editHandler")

	p, err := loadWikiPage(r, name)

	if err != nil {
		if err.Error() == "NO_SUCH_FILE" {
			//log.Println("No such file...creating one.")
			terr := renderTemplate(w, r.Context(), "wiki_edit.tmpl", p)
			if terr != nil {
				log.Println("wiki_edit error:")
				log.Fatalln(terr)
			} else {
				return
			}
		} else {
			log.Println("loadWikiPage error:")
			log.Fatalln(err)
		}
	} else {
		terr := renderTemplate(w, r.Context(), "wiki_edit.tmpl", p)
		if terr != nil {
			log.Println("wiki_edit error:")
			log.Fatalln(terr)
		} else {
			return
		}
	}
}

func saveHandler(w http.ResponseWriter, r *http.Request, name string) {
	defer utils.TimeTrack(time.Now(), "saveHandler")

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
		Public:  publicbool,
		Admin:    adminbool,
	}

	thewiki := &wiki{
		Title: title,
		Filename: name,
		Frontmatter: fm,
		Content: []byte(content),
	}

	err := thewiki.save()
	if err != nil {
		auth.SetSession("flash", "Failed to save page.", w, r)
		http.Redirect(w, r, "/", http.StatusSeeOther)
		log.Fatalln(err)
		return
	}

	// Crawl for new favorites only on startup and save
	favbuf.Reset()
	err = filepath.Walk(viper.GetString("WikiDir"), readFavs)
	if err != nil {
		log.Println("filepath.Walk error:")
		log.Fatal(err)
	}

	auth.SetSession("flash", "Wiki page successfully saved.", w, r)
	http.Redirect(w, r, "/"+name, http.StatusSeeOther)
	log.Println(name + " page saved!")
}

func setFlash(msg string, w http.ResponseWriter, r *http.Request) {
	auth.SetSession("flash", msg, w, r)
}

func newHandler(w http.ResponseWriter, r *http.Request) {
	defer utils.TimeTrack(time.Now(), "newHandler")

	pagetitle := r.FormValue("newwiki")

	//fullfilename := cfg.WikiDir + pagetitle
	fullfilename := filepath.Join(viper.GetString("WikiDir"), pagetitle)
	rel, err := filepath.Rel(viper.GetString("WikiDir"), fullfilename)
	if err != nil {
		auth.SetSession("flash", "Failed to create page.", w, r)
		http.Redirect(w, r, "/", http.StatusSeeOther)
		log.Fatalln(err)
		return
	}
	if strings.HasPrefix(rel, "../") {
		auth.SetSession("flash", "Failed to create page.", w, r)
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	_, fierr := os.Stat(pagetitle)
	if os.IsNotExist(fierr) {
		http.Redirect(w, r, "/edit/"+pagetitle, http.StatusTemporaryRedirect)
		return
	} else if fierr != nil {
		log.Println("newHandler file error:")
		log.Println(fierr)
	}

	http.Redirect(w, r, pagetitle, http.StatusTemporaryRedirect)
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
	defer utils.TimeTrack(time.Now(), "favsHandler")

	favss := favbuf.String()
	utils.Debugln("Favorites: " + favss)
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

func testEq(a, b []byte) bool {

	if a == nil && b == nil {
		return true
	}

	if a == nil || b == nil {
		return false
	}

	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

func loadWiki(name string) (*wiki, error) {
	var fm frontmatter
	var pagetitle string

	//fullfilename := cfg.WikiDir + name
	fullfilename := filepath.Join(viper.GetString("WikiDir"), name)

	// Check if file exists before doing anything else
	fileExists, feErr := doesPageExist(name)
	if !fileExists && feErr == nil {
		// NOW: Using os.Stat to properly check for file existence, using IsNotExist()
		// This should mean file is non-existent, so create new page
		errn := errors.New("NO_SUCH_FILE")
		return nil, errn
	}

	// Directory without specified index
	if strings.HasSuffix(name, "/") {
		//if dir != "" && name == "" {
		log.Println("This might be a directory, trying to parse the index")
		//filename := name + "index"
		//fullfilename = cfg.WikiDir + name + "index"
		fullfilename = filepath.Join(viper.GetString("WikiDir"), name, "index")

		dirindex, _ := os.Open(fullfilename)
		_, dirindexfierr := dirindex.Stat()
		if os.IsNotExist(dirindexfierr) {
			return nil, errors.New("NO_DIR_INDEX")
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

	// TODO: improve this so private pages are actually protected
	if fm.Public {
		log.Println("Public page!")
	}
	if fm.Title != "" {
		pagetitle = fm.Title
	} else {
		pagetitle = name
	}
	ctime, err := gitGetCtime(name)
	if err != nil {
		// If not in git, redirect to gitadd
		if err.Error() == "NOT_IN_GIT" {
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
		Title: pagetitle,
		Filename: name,
		Frontmatter: &fm,
		Content:  content,
		CreateTime: ctime,
		ModTime: mtime,
	}, nil

}

func (wiki *wiki) save() error {
	defer utils.TimeTrack(time.Now(), "wiki.save()")

	dir, filename := filepath.Split(wiki.Filename)
	//fullfilename := cfg.WikiDir + dir + filename
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
		log.Fatalln(err)
		return err
	}
	yamlBuffer, err := yaml.Marshal(wiki.Frontmatter)
	if err != nil {
		log.Fatalln(err)
		return err
	}
	buffer.Write(yamlBuffer)
	_, err = buffer.Write([]byte("---\n"))
	if err != nil {
		log.Fatalln(err)
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
	defer utils.TimeTrack(time.Now(), "loginPageHandler")

	title := "login"
	p, err := loadPage(r)
	if err != nil {
		log.Fatalln(err)
	}
	gp := &genPage{
		p,
		title,
	}
	err = renderTemplate(w, r.Context(), "login.tmpl", gp)
	if err != nil {
		log.Println("render login error:")
		log.Println(err)
		return
	}
}

func signupPageHandler(w http.ResponseWriter, r *http.Request) {
	defer utils.TimeTrack(time.Now(), "signupPageHandler")

	title := "signup"
	p, err := loadPage(r)
	if err != nil {
		log.Fatalln(err)
	}
	gp := &genPage{
		p,
		title,
	}
	err = renderTemplate(w, r.Context(), "signup.tmpl", gp)
	if err != nil {
		log.Println("render signup error:")
		log.Println(err)
		return
	}
}

func adminUsersHandler(w http.ResponseWriter, r *http.Request) {
	defer utils.TimeTrack(time.Now(), "adminUsersHandler")

	title := "admin-users"
	p, err := loadPage(r)
	if err != nil {
		log.Fatalln(err)
	}
	userlist, err := auth.Userlist()
	if err != nil {
		log.Fatalln(err)
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
	err = renderTemplate(w, r.Context(), "admin_users.tmpl", data)
	if err != nil {
		log.Println("render admin_users error:")
		log.Println(err)
		return
	}
}

func adminUserHandler(w http.ResponseWriter, r *http.Request) {
	defer utils.TimeTrack(time.Now(), "adminUserHandler")

	title := "admin-user"
	p, err := loadPage(r)
	if err != nil {
		log.Fatalln(err)
	}
	userlist, err := auth.Userlist()
	if err != nil {
		log.Fatalln(err)
	}

	//ctx := r.Context()
	params := r.Context().Value(httptreemux.ParamsContextKey).(map[string]string)
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
	err = renderTemplate(w, r.Context(), "admin_user.tmpl", data)
	if err != nil {
		log.Println("render admin_user error:")
		log.Println(err)
		return
	}
}

// Function to take a <select><option> value and redirect to a URL based on it
func adminUserPostHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	selectedUser := r.FormValue("user")
	http.Redirect(w, r, "/admin/user/"+selectedUser, http.StatusSeeOther)
}

func adminMainHandler(w http.ResponseWriter, r *http.Request) {
	defer utils.TimeTrack(time.Now(), "adminMainHandler")

	title := "admin-main"
	p, err := loadPage(r)
	if err != nil {
		log.Fatalln(err)
	}
	gp := &genPage{
		p,
		title,
	}
	err = renderTemplate(w, r.Context(), "admin_main.tmpl", gp)
	if err != nil {
		log.Println("render admin_main error:")
		log.Println(err)
		return
	}
}

func adminConfigHandler(w http.ResponseWriter, r *http.Request) {
	defer utils.TimeTrack(time.Now(), "adminConfigHandler")

	var cfg configuration
	// To save config to toml:
	err := viper.Unmarshal(&cfg)
	//jc := conf
	if err != nil {
		log.Println(err)
	}

	title := "admin-config"
	p, err := loadPage(r)
	if err != nil {
		log.Fatalln(err)
	}
	data := struct {
		*page
		Title string
		Config configuration
	}{
		p,
		title,
		cfg,
	}
	err = renderTemplate(w, r.Context(), "admin_config.tmpl", data)
	if err != nil {
		log.Println("render admin_config error:")
		log.Println(err)
		return
	}
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
		Domain: domain,
		Port: port,
		Email: email,
		WikiDir: wikidir,
		GitRepo: gitrepo,
		AdminUser: adminuser,
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
	http.Redirect(w, r, "/admin/config", http.StatusOK)
	return
}

func gitCheckinHandler(w http.ResponseWriter, r *http.Request) {
	defer utils.TimeTrack(time.Now(), "gitCheckinHandler")

	title := "Git Checkin"
	p, err := loadPage(r)
	if err != nil {
		log.Fatalln(err)
	}
	var owithnewlines []byte

	if r.URL.Query().Get("file") != "" {
		file := r.URL.Query().Get("file")
		owithnewlines = []byte(file)
	} else {
		o, err := gitIsClean()
		if err != nil && err.Error() != "directory is dirty" {
			log.Fatalln(err)
		}
		owithnewlines = bytes.Replace(o, []byte{0}, []byte(" <br>"), -1)
	}

	gp := &gitPage{
		p,
		title,
		string(owithnewlines),
		viper.GetString("GitRepo"),
	}
	err = renderTemplate(w, r.Context(), "git_checkin.tmpl", gp)
	if err != nil {
		log.Println("render git_checkin error:")
		log.Println(err)
		return
	}
}

func gitCheckinPostHandler(w http.ResponseWriter, r *http.Request) {
	defer utils.TimeTrack(time.Now(), "gitCheckinPostHandler")

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
		log.Fatalln(err)
		return
	}
	err = gitCommitEmpty()
	if err != nil {
		log.Fatalln(err)
		return
	}
	if path != "." {
		http.Redirect(w, r, "/"+path, http.StatusSeeOther)
	} else {
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}

}

func gitPushPostHandler(w http.ResponseWriter, r *http.Request) {
	defer utils.TimeTrack(time.Now(), "gitPushPostHandler")

	err := gitPush()
	if err != nil {
		log.Fatalln(err)
		return
	}

	http.Redirect(w, r, "/admin/git", http.StatusSeeOther)

}

func gitPullPostHandler(w http.ResponseWriter, r *http.Request) {
	defer utils.TimeTrack(time.Now(), "gitPullPostHandler")

	err := gitPull()
	if err != nil {
		log.Fatalln(err)
		return
	}

	http.Redirect(w, r, "/admin/git", http.StatusSeeOther)

}

func adminGitHandler(w http.ResponseWriter, r *http.Request) {
	defer utils.TimeTrack(time.Now(), "adminGitHandler")

	title := "Git Management"
	p, err := loadPage(r)
	if err != nil {
		log.Fatalln(err)
	}
	var owithnewlines []byte

	o, err := gitIsClean()
	if err != nil && err.Error() != "directory is dirty" {
		log.Fatalln(err)
	}
	owithnewlines = bytes.Replace(o, []byte{0}, []byte(" <br>"), -1)

	gp := &gitPage{
		p,
		title,
		string(owithnewlines),
		viper.GetString("GitRepo"),
	}
	err = renderTemplate(w, r.Context(), "admin_git.tmpl", gp)
	if err != nil {
		log.Println("render admin_git error:")
		log.Println(err)
		return
	}
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
	defer utils.TimeTrack(time.Now(), "tagMapHandler")
	
	a := &tagMap

	p, err := loadPage(r)
	if err != nil {
		log.Fatalln(err)
	}

	tagpage := &tagMapPage{
		page:    p,
		TagKeys: *a,
	}
	err = renderTemplate(w, r.Context(), "tag_list.tmpl", tagpage)
	if err != nil {
		log.Println("render tag_list error:")
		log.Println(err)
		return
	}

}

func createWiki(w http.ResponseWriter, r *http.Request, name string) {
	username, _ := auth.GetUsername(r.Context())
	if username != "" {
		w.WriteHeader(404)
		//title := "Create " + name + "?"
		p, err := loadPage(r)
		if err != nil {
			log.Fatalln(err)
			return
		}
		/*gp := &genPage{
			p,
			name,
		}*/
		wp := &wikiPage{
			page: p,
			Wiki: &wiki{
				Title: name,
				Filename: name,
				Frontmatter: &frontmatter{
					Title: name,
				},
			},
		}
		err = renderTemplate(w, r.Context(), "wiki_create.tmpl", wp)
		if err != nil {
			//panic(err)
			log.Println("wiki_create error:")
			log.Println(err)
			return
		}
		return
	}

	auth.SetSession("flash", "Please login to view that page.", w, r)
	http.Redirect(w, r, "http://"+r.Host+"/login"+"?url="+r.URL.String(), http.StatusSeeOther)
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

// wikiHandler wraps around all wiki page handlers
// Currently it retrieves the page name from params, checks for file existence, and checks for private pages
func wikiHandler(fn wHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Here we will extract the page title from the Request,
		// and call the provided handler 'fn'

		params := r.Context().Value(httptreemux.ParamsContextKey).(map[string]string)

		// Replacing these for now:
		name := params["name"]
		fullname := filepath.Join(viper.GetString("WikiDir"), name)
		
		//dir := filepath.Dir(name)
		//wikipage := cfg.WikiDir + name

		username, isAdmin := auth.GetUsername(r.Context())

		// Check if file exists before doing anything else
		fileExists, feErr := doesPageExist(name)
		if !fileExists && feErr == nil {
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
		}

		if !fileExists && feErr != nil {
			log.Println(name+" does not exist, but has an error:")
			log.Println(feErr)
			return
		}

		// Read YAML frontmatter into fm
		// If err, just return, as file should not contain frontmatter
		fmbytes, _, err := readFileAndFront(fullname)
		if err != nil {
			log.Println(err)
			return
		}
		var fm frontmatter
		fm, err = marshalFrontmatter(fmbytes)
		if err != nil {
			log.Println("YAML unmarshal error in: " + name)
			return
		}

		if err == nil && fm.Public {
			fn(w, r, name)
			return
		}		
		if err == nil {
			if fm.Admin {
				if !isAdmin {
					log.Println(username + " attempting to access restricted URL.")
					auth.SetSession("flash", "Sorry, you are not allowed to see that.", w, r)
					http.Redirect(w, r, "/", http.StatusSeeOther)
					return
				}
			}			
		}


		if username == "" {
			rurl := r.URL.String()
			utils.Debugln("wikiHandler mitigating: " + r.Host + rurl)
			//w.Write([]byte("OMG"))

			// Detect if we're in an endless loop, if so, just panic
			if strings.HasPrefix(rurl, "login?url=/login") {
				panic("AuthMiddle is in an endless redirect loop")
			}
			auth.SetSession("flash", "Please login to view that page.", w, r)
			http.Redirect(w, r, "http://"+r.Host+"/login"+"?url="+rurl, http.StatusSeeOther)
			return
		} else {
			fn(w, r, name)
			return
		}
	}
}

func treeMuxWrapper(next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	}
}

func timer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		newt := timeNewContext(r.Context(), time.Now())
		next.ServeHTTP(w, r.WithContext(newt))
	})
}

func riceInit() error {
	templateBox, err := rice.FindBox("templates")
	if err != nil {
		return err
	}
	includes, err := templateBox.Open("includes")
	if err != nil {
		return err
	}	
	includeDir, err := includes.Readdir(-1)
	if err != nil {
		return err
	}	
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
		//log.Println(boxT)
		iString, _ := templateBox.String("includes/"+v.Name())
		templateIBuff.WriteString(iString)
	}
	
	funcMap := template.FuncMap{"prettyDate": utils.PrettyDate, "safeHTML": utils.SafeHTML, "imgClass": utils.ImgClass, "isLoggedIn": isLoggedIn, "jsTags": jsTags}

	//templatesB := make(map[string]*template.Template)

	// Here we are prefacing every layout with what should be every includes/ .tmpl file
	// Ex: includes/sidebar.tmpl includes/bottom.tmpl includes/base.tmpl layouts/list.tmpl
	// **THIS IS VERY IMPORTANT TO ALLOW MY BASE TEMPLATE TO WORK**
	for _, layout := range layoutsDir {
		boxT = append(boxT, "layouts/"+layout.Name())
		//DEBUG TEMPLATE LOADING
		//utils.Debugln(files)
		lString, _ := templateBox.String("layouts/"+layout.Name())
		fstring := templateIBuff.String() + lString
		templates[layout.Name()] = template.Must(template.New(layout.Name()).Funcs(funcMap).Parse(fstring))
	}
	return nil
}

func authInit(authDB string) error {
	auth.Open(authDB)
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
			log.Fatalln("Clone/move your existing repo here, change the config, or run with -init to clone a specified remote repo.")
		}
	}
}

func bleveIndex() {
	
	var err error
	index, err = bleve.Open("./data/index.bleve")
	if err == bleve.ErrorIndexPathDoesNotExist {
		nameMapping := bleve.NewTextFieldMapping()
		nameMapping.Analyzer = keyword_analyzer.Name

		contentMapping := bleve.NewTextFieldMapping()
		contentMapping.Analyzer = en.AnalyzerName

		boolMapping := bleve.NewBooleanFieldMapping()

		timestamp := "2006-01-02 at 03:04:05PM"
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
			log.Fatal(err)
		}

		fileList, flerr := gitLs()
		if flerr != nil {
			log.Fatalln(err)
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
				if err != nil && err.Error() != "NOT_IN_GIT" {
					log.Panicln(err)
				}
				mtime, err := gitGetMtime(file)
				if err != nil {
					log.Panicln(err)
				}
				

				data := struct {
					Name    string `json:"name"`
					Public  bool
					Tags    []string `json:"tags"`
					Content string `json:"content"`
					Created string
					Modified string
				}{
					Name: pagetitle,
					Public: fm.Public,
					Tags: fm.Tags,
					Content: string(content),
					Created: time.Unix(ctime, 0).Format(timestamp),
					Modified: time.Unix(mtime, 0).Format(timestamp),
				}
				/*
				wp = &wiki{
					Title: pagetitle,
					Filename: file,
					Frontmatter: &fm,
					Content: content,
					CreateTime: ctime,
					ModTime: mtime,
				}
				*/

				index.Index(file, data)
				
			}			
		}

	}




	

	//query := bleve.NewQueryStringQuery("lisa")

	//log.Println(searchResult.Hits[0].Fields)

	/*
	query2 := bleve.NewMatchAllQuery()
	searchRequest2 := bleve.NewSearchRequest(query2)
	searchResults, err := index.Search(searchRequest2)
	if err != nil {
		panic(err)
	}
	log.Println(searchResults.String())	
	*/

}

func search(w http.ResponseWriter, r *http.Request) {
	params := r.Context().Value(httptreemux.ParamsContextKey).(map[string]string)
	name := params["name"]

	p, err := loadPage(r)
	if err != nil {
		log.Fatalln(err)
	}

	//query := bleve.NewMatchQuery(name)

	must := make([]bleve.Query, 1)
	mustNot := make([]bleve.Query, 1)
	must[0] = bleve.NewMatchQuery(name)

	public := false

	username, _ := auth.GetUsername(r.Context())
	if username != "" {
		public = true
	}

	mustNot[0] = bleve.NewBoolFieldQuery(public)
	mustNot[0].SetField("Public")

	query := bleve.NewBooleanQuery(must, nil, mustNot)

	searchRequest := bleve.NewSearchRequest(query)

	searchRequest.Highlight = bleve.NewHighlight()

	searchResult, _ := index.Search(searchRequest)
	log.Println(searchResult.String())

	var results []*result
	
	//var results map[string]string
	for _, v := range searchResult.Hits {
		for _, fragments := range v.Fragments {
			for _, fragment := range fragments {
				var r *result
				r = &result{
					Name: v.ID,
					Result: fragment,
				}
				//results[v.ID] = fragment
				results = append(results, r)
			}
		}
	}

	s := &searchPage{p, results}
	err = renderTemplate(w, r.Context(), "search_results.tmpl", s)
	if err != nil {
		log.Fatalln(err)
	}

}

func main() {

	viper.WatchConfig()

	flag.Parse()

	// Open and initialize auth database
	err := authInit("./data/auth.db")
	if err != nil {
		log.Fatalln(err)
	}
	defer auth.Authdb.Close()

	utils.AssetsBox = rice.MustFindBox("assets")
	auth.AdminUser = viper.GetString("AdminUser")

	err = riceInit()
	if err != nil {
		log.Fatalln(err)
	}

	initWikiDir()
	bleveIndex()


	// Crawl for new favorites only on startup and save
	err = filepath.Walk(viper.GetString("WikiDir"), readFavs)
	if err != nil {
		//log.Fatal(err)
		log.Println("init: unable to crawl for favorites")
	}

	// Crawl for tags only on startup and save
	err = filepath.Walk(viper.GetString("WikiDir"), readTags)
	if err != nil {
		//log.Fatal(err)
		log.Println("init: unable to crawl for tags")
	}

	// HTTP stuff from here on out
	s := alice.New(timer, utils.Logger, auth.UserEnvMiddle, csrf.Protect([]byte("c379bf3ac76ee306cf72270cf6c5a612e8351dcb"), csrf.Secure(false)))

	r := httptreemux.New()
	r.PanicHandler = httptreemux.ShowErrorsPanicHandler
	
	statsdata := stats.New()

	r.GET("/", indexHandler)

	r.GET("/tags", tagMapHandler)
	/*r.GET("/panic", func(w http.ResponseWriter, r *http.Request) {
		panic("Unexpected error!")
		//http.Error(w, panic("Unexpected error!"), http.StatusInternalServerError)
	})*/

	r.GET("/new", auth.AuthMiddle(newHandler))
	//r.HandleFunc("/login", auth.LoginPostHandler).Methods("POST")
	r.GET("/login", loginPageHandler)
	//r.HandleFunc("/logout", auth.LogoutHandler).Methods("POST")
	r.GET("/logout", auth.LogoutHandler)
	//r.GET("/signup", signupPageHandler)
	r.GET("/list", listHandler)
	r.GET("/search/*name", search)
	r.GET("/recent", auth.AuthMiddle(recentHandler))
	r.GET("/health", HealthCheckHandler)

	admin := r.NewGroup("/admin")
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

	a := r.NewGroup("/auth")
	a.POST("/login", auth.LoginPostHandler)
	a.POST("/logout", auth.LogoutHandler)
	a.GET("/logout", auth.LogoutHandler)
	//a.POST("/signup", auth.SignupPostHandler)

	//r.HandleFunc("/signup", auth.SignupPostHandler).Methods("POST")

	r.POST("/gitadd", auth.AuthMiddle(gitCheckinPostHandler))
	r.GET("/gitadd", auth.AuthMiddle(gitCheckinHandler))

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

	http.HandleFunc("/robots.txt", utils.RobotsHandler)
	http.HandleFunc("/favicon.ico", utils.FaviconHandler)
	http.HandleFunc("/favicon.png", utils.FaviconHandler)
	http.HandleFunc("/assets/", utils.StaticHandler)
	http.Handle("/", s.Then(r))

	log.Println("Listening on port " + viper.GetString("Port"))
	http.ListenAndServe("0.0.0.0:"+viper.GetString("Port"), nil)

}
