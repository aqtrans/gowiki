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
	"embed"
	"encoding/gob"
	"encoding/json"
	"errors"
	"expvar"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
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

	"github.com/pelletier/go-toml"
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
	yaml "gopkg.in/yaml.v2"

	"github.com/dimfeld/httptreemux"
	"github.com/justinas/alice"
	"github.com/oxtoacart/bpool"
	"github.com/russross/blackfriday"
	_ "github.com/tevjef/go-runtime-metrics/expvar"

	"git.jba.io/go/auth"
	"git.jba.io/go/httputils"
	//"git.jba.io/go/wiki/vfs/assets"
	//"git.jba.io/go/wiki/vfs/templates"
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
	flashKey       key = 4
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

// Go 1.16 embed:
//go:embed assets
var assetsfs embed.FS

//go:embed templates
var templatefs embed.FS

var (
	//authState      *auth.AuthState
	//linkPattern = regexp.MustCompile(`\[\/(?P<Name>[0-9a-zA-Z-_\.\/]+)\]\(\)`)
	//bufpool        *bpool.BufferPool
	//templates      map[string]*template.Template
	//gitPath string
	//dataDir string
	//cache          *wikiCache
	debugMode      bool
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

type signupPage struct {
	page
	Title    string
	UserRole string
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

type config struct {
	GitPath        string `yaml:"GitPath,omitempty"`
	GitCommitEmail string `yaml:"GitCommitEmail,omitempty"`
	GitCommitName  string `yaml:"GitCommitName,omitempty"`
	DataDir        string `yaml:"DataDir,omitempty"`
	WikiDir        string `yaml:"WikiDir,omitempty"`
	Port           string `yaml:"Port,omitempty"`
	Domain         string `yaml:"Domain,omitempty"`
	RemoteGitRepo  string `yaml:"RemoteGitRepo,omitempty"`
	PushOnSave     bool   `yaml:"PushOnSave,omitempty"`
	InitWikiRepo   bool   `yaml:"InitWikiRepo,omitempty"`
	CacheEnabled   bool   `yaml:"CacheEnabled,omitempty"`
	CSRF           bool   `yaml:"CSRF,omitempty"`
}

// Env wrapper to hold app-specific configs, to pass to handlers
// cache is a pointer here since it's pretty large itself, and holds a mutex
type wikiEnv struct {
	cfg       config
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

	// Setting these last; they do not need to be set manually:

	//viper.SetDefault("WikiDir", filepath.Join(dataDir, "wikidata"))
	//viper.SetDefault("CacheLocation", filepath.Join(dataDir, "cache.gob"))
	//viper.SetDefault("AuthLocation", filepath.Join(dataDir, "auth.db"))
	//viper.SetDefault("InitWikiRepo", *initFlag)

}

func loadConfig(confFile string) config {
	var cfg config

	ext := filepath.Ext(confFile)
	switch ext {
	case ".toml":
		cfgTree, err := toml.LoadFile(confFile)
		if err != nil {
			log.Fatalln("Error reading", confFile, err)
		}
		err = cfgTree.Unmarshal(&cfg)
		if err != nil {
			log.Fatalln("Error unmarshaling config:", err)
		}
	case ".yaml":
		cfgBytes, err := ioutil.ReadFile(confFile)
		if err != nil {
			log.Fatalln("Error reading", confFile, err)
		}
		err = yaml.Unmarshal(cfgBytes, &cfg)
		if err != nil {
			log.Fatalln("Error unmarshaling config:", err)
		}
	case "":
		log.Println("No config file specified. Loading defaults.")
	default:
		log.Fatalln("Only able to load TOML and YAML files currently. Unable to load", confFile)
	}

	// Set defaults
	if cfg.DataDir == "" {
		cfg.DataDir = "./data/"
	}

	if cfg.DataDir != "" {
		cfg.WikiDir = filepath.Join(cfg.DataDir, "wikidata")
	}

	if cfg.GitCommitEmail == "" {
		cfg.GitCommitEmail = "golang-wiki@foo.bar"
	}

	if cfg.GitCommitName == "" {
		cfg.GitCommitName = "Golang Wiki"
	}

	if cfg.Port == "" {
		cfg.Port = "5000"
	}

	// Look for git, if GitPath is not defined in the config file
	if cfg.GitPath == "" {
		gitPath, err := exec.LookPath("git")
		if err != nil {
			log.Fatalln("Git executable was not found in PATH. Git must be installed. Install Git or define it as GitPath in", confFile)
		}
		if err == nil {
			cfg.GitPath = gitPath
		}
	}

	return cfg
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

func svg(iconName string) template.HTML {
	// MAJOR TODO:
	// Check for file existence before trying to read the file; if non-existent return ""
	iconFile, err := assetsfs.ReadFile("assets/icons/" + iconName + ".svg")
	if err != nil {
		log.Println("Error loading assets/icons/", iconName, err)
		return template.HTML("")
	}
	return template.HTML(`<div class="svg-icon">` + string(iconFile) + `</div>`)
}

func svgByte(iconName string) []byte {
	// MAJOR TODO:
	// Check for file existence before trying to read the file; if non-existent return ""
	iconFile, err := assetsfs.ReadFile("assets/icons/" + iconName + ".svg")
	if err != nil {
		log.Println("Error loading assets/icons/", iconName, err)
		return []byte("")
	}
	return []byte(`<div class="svg-icon">` + string(iconFile) + `</div>`)
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
		log.Debugln("No startTime in context.")
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
		log.Debugln("No wikiName in context.")
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
		log.Debugln("No wikiExists in context.")
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
		log.Debugln("No wiki in context.")
		return &wiki{}
	}
	return w
}

func flashNewContext(c context.Context, t string) context.Context {
	return context.WithValue(c, flashKey, t)
}

func flashFromContext(c context.Context) string {
	t, ok := c.Value(flashKey).(string)
	if !ok {
		log.Debugln("No flash in context.")
		return ""
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

func (f *favs) LoadOrStore(s string) {
	f.Lock()
	if _, ok := f.List[s]; !ok {
		log.Debugln("favs.LoadOrStore: " + s + " is not already a favorite.")
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

func (env *wikiEnv) loadPage(r *http.Request, p chan<- page) {
	defer httputils.TimeTrack(time.Now(), "loadPage")
	//timer.Step("loadpageFunc")

	// Auth lib middlewares should load the user and tokens into context for reading
	user := env.authState.GetUserState(r)

	// Flash is loaded into context in timer() because it's one-time-use
	//   requires passing http.ResponseWriter
	msg := flashFromContext(r.Context())
	//token := auth.GetToken(r.Context())
	token := auth.CSRFTemplateField(r)

	var message template.HTML
	if msg != "" {
		message = template.HTML(`
			<div class="notification anim active" id="notification">
			<p>` + msg + `
			<button class="close-button" type="button" id="close-button">
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
	gitHTML := env.gitIsCleanURLs(token)
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
			IsLoggedIn: env.authState.IsLoggedIn(r),
		},
		Token:     token,
		FlashMsg:  message,
		GitStatus: gitHTML,
	}
}

func readFileAndFront(filename string) (frontmatter, []byte) {
	//defer httputils.TimeTrack(time.Now(), "readFileAndFront")

	f, err := os.Open(filename)
	//checkErr("readFileAndFront()/Open", err)
	if err != nil {
		log.Debugln("Error in readFileAndFront:", err)
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
	err = tmpl.ExecuteTemplate(buf, "footer", getRenderTime(c))
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

	log.Debugln("doesPageExist", name, exists)

	return exists, finError
}

// Check that the given full path is relative to the configured wikidir
func (env *wikiEnv) relativePathCheck(name string) error {
	defer httputils.TimeTrack(time.Now(), "relativePathCheck")
	fullfilename := filepath.Join(env.cfg.WikiDir, name)
	dir, _ := filepath.Split(name)
	if dir != "" {
		dirErr := env.checkDir(dir)
		if dirErr != nil {
			return dirErr
		}
	}

	_, err := filepath.Rel(env.cfg.WikiDir, fullfilename)
	return err
}

// This does various checks to see if an existing page exists or not
// Also checks for and returns an error on some edge cases
// So we only proceed if this returns false AND nil
// Edge cases checked for currently:
// - If name is trying to escape or otherwise a bad path
// - If name is a /directory/file combo, but /directory is actually a file
// - If name contains a .git entry, error out
func (env *wikiEnv) checkName(name *string) (bool, error) {
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
	relErr := env.relativePathCheck(*name)
	if relErr != nil {
		return false, relErr
	}

	// Build the full path

	fullfilename := filepath.Join(env.cfg.WikiDir, *name)

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
					log.Debugln(*name + " found!")
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
		fullnewfilename := filepath.Join(env.cfg.WikiDir, normalName)
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
func (env *wikiEnv) checkDir(dir string) error {
	defer httputils.TimeTrack(time.Now(), "checkDir")

	dirs := strings.Split(dir, "/")
	var relpath string
	var err error
	for _, v := range dirs {
		// relpath progressively builds up the /path/to/file, element by element
		relpath = filepath.Join(relpath, v)
		// We combine that with the configured WikiDir to get the fullpath
		fullpath := filepath.Join(env.cfg.WikiDir, relpath)
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

func isWiki(fullFilename string) bool {
	var isWiki bool
	file, err := os.Open(fullFilename)
	if err != nil {
		isWiki = false
	}

	defer file.Close()
	buff := make([]byte, 512)
	_, err = file.Read(buff)
	if err != nil {
		isWiki = false
	}
	filetype := http.DetectContentType(buff)
	if filetype == "application/octet-stream" {
		// Definitely wiki page...but others probably
		if string(buff[:3]) == "---" {
			isWiki = true
		}

		// Account for .page files from gitit
		if filepath.Ext(fullFilename) == ".page" || filepath.Ext(fullFilename) == ".md" {
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

func (env *wikiEnv) loadWiki(name string, w chan<- wiki) {
	defer httputils.TimeTrack(time.Now(), "loadWiki")

	var fm frontmatter

	ctime := make(chan int64, 1)
	mtime := make(chan int64, 1)
	go env.gitGetTimes(name, ctime, mtime)

	fm, content := readFileAndFront(filepath.Join(env.cfg.WikiDir, name))

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

func (env *wikiEnv) loadWikiPage(r *http.Request, name string) wikiPage {
	defer httputils.TimeTrack(time.Now(), "loadWikiPage")

	var theWiki wiki
	var md string

	p := make(chan page, 1)
	go env.loadPage(r, p)

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
		go env.loadWiki(name, wc)
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

func (wiki *wiki) save(env *wikiEnv) error {
	env.mutex.Lock()
	defer httputils.TimeTrack(time.Now(), "wiki.save()")

	dir, filename := filepath.Split(wiki.Filename)
	fullfilename := filepath.Join(env.cfg.WikiDir, dir, filename)

	// If directory doesn't exist, create it
	// - Check if dir is null first
	if dir != "" {
		dirpath := filepath.Join(env.cfg.WikiDir, dir)
		if _, err := os.Stat(dirpath); os.IsNotExist(err) {
			err := os.MkdirAll(dirpath, 0755)
			if err != nil {
				env.mutex.Unlock()
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
		env.mutex.Unlock()
		return err
	}

	//buffer := new(bytes.Buffer)
	wb := bufio.NewWriter(f)

	_, err = wb.WriteString("---\n")
	if err != nil {
		env.mutex.Unlock()
		return err
	}

	yamlBuffer, err := yaml.Marshal(wiki.Frontmatter)
	if err != nil {
		env.mutex.Unlock()
		return err
	}

	_, err = wb.Write(yamlBuffer)
	if err != nil {
		env.mutex.Unlock()
		return err
	}

	_, err = wb.WriteString("---\n")
	if err != nil {
		env.mutex.Unlock()
		return err
	}

	_, err = wb.Write(wiki.Content)
	if err != nil {
		env.mutex.Unlock()
		return err
	}

	err = wb.Flush()
	if err != nil {
		env.mutex.Unlock()
		return err
	}

	err = f.Close()
	if err != nil {
		env.mutex.Unlock()
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

	err = env.gitAddFilepath(gitfilename)
	if err != nil {
		env.mutex.Unlock()
		return err
	}

	// FIXME: add a message box to edit page, check for it here
	err = env.gitCommitEmpty()
	if err != nil {
		env.mutex.Unlock()
		return err
	}

	log.Println(fullfilename + " has been saved.")
	env.mutex.Unlock()
	return err

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

func tmplInit() map[string]*template.Template {
	templates := make(map[string]*template.Template)

	//templatesDir := "./templates/"
	layouts, err := templatefs.ReadDir("templates/layouts")
	if err != nil {
		log.Fatalln(err)
	}
	includes, err := templatefs.ReadDir("templates/includes")
	if err != nil {
		log.Fatalln(err)
	}

	var includesString []string
	for _, includeEntry := range includes {
		includesString = append(includesString, "templates/includes/"+includeEntry.Name())
	}

	funcMap := template.FuncMap{"svg": svg, "typeIcon": typeIcon, "prettyDate": httputils.PrettyDate, "safeHTML": httputils.SafeHTML, "imgClass": httputils.ImgClass, "isLoggedIn": isLoggedIn, "jsTags": jsTags}

	for _, layout := range layouts {
		files := append(includesString, "templates/layouts/"+layout.Name())
		//DEBUG TEMPLATE LOADING
		//httputils.Debugln(files)
		templates[filepath.Base(layout.Name())] = template.Must(template.New("templates").Funcs(funcMap).ParseFS(templatefs, files...))
	}
	return templates
}

func initWikiDir(cfg config) error {
	// Check for root DataDir existence first
	dir, err := os.Stat(cfg.DataDir)
	if err != nil {
		if os.IsNotExist(err) {
			log.Println("Datadir", cfg.DataDir, "does not exist; creating it.")
			err = os.Mkdir(cfg.DataDir, 0755)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	} else if !dir.IsDir() {
		return errors.New(cfg.DataDir + "is not a directory. This is where wiki data is stored.")
	}

	//Check for wikiDir directory + git repo existence
	_, err = os.Stat(cfg.WikiDir)
	if err != nil && os.IsNotExist(err) {
		log.Println("Wikidir at", cfg.WikiDir, "does not exist; creating it.")
		err = os.Mkdir(cfg.WikiDir, 0755)
		if err != nil {
			return err
		}
	}
	_, err = os.Stat(filepath.Join(cfg.WikiDir, ".git"))
	if err != nil && os.IsNotExist(err) {
		log.Println("Initializing git repository at", cfg.WikiDir)
		env := &wikiEnv{
			cfg: cfg,
		}
		err := env.gitInit()
		if err != nil {
			return err
		}
	}
	return nil
}

func setPageTitle(frontmatterTitle, filename string) string {
	var name string
	if frontmatterTitle != "" {
		name = frontmatterTitle
	} else {
		name = filename
	}

	_, onlyFileName := filepath.Split(name)

	return onlyFileName
}

func (env *wikiEnv) buildCache() {
	defer httputils.TimeTrack(time.Now(), "buildCache")
	/*
		cache := new(wikiCache)
		cache.Tags = make(map[string][]string)
		cache.Favs = make(map[string]struct{})
	*/
	var wps []gitDirList

	if !env.gitIsEmpty() {

		fileList, err := env.gitLsTree()
		if err != nil {
			log.WithFields(logrus.Fields{
				"error": err,
			}).Errorln("Error loading file list from git")
			return
		}

		for _, file := range fileList {

			// If using Git, build the full path:
			fullname := filepath.Join(env.cfg.WikiDir, file.Filename)

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
				go env.gitGetTimes(file.Filename, ctime, mtime)

				var wp gitDirList

				// Read YAML frontmatter into fm
				f, err := os.Open(fullname)
				if err != nil {
					log.WithFields(logrus.Fields{
						"error": err,
						"file":  f.Name(),
					}).Errorln("Error opening file")
					return
				}

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
					if _, ok := env.cache.Favs[file.Filename]; !ok {
						log.Debugln("buildCache.favs: " + file.Filename + " is not already a favorite.")
						env.cache.Favs[file.Filename] = struct{}{}
					}
				}
				if fm.Tags != nil {
					//cache.Tags.LoadOrStore(fm.Tags, file.Filename)
					for _, tag := range fm.Tags {
						env.cache.Tags[tag] = append(env.cache.Tags[tag], file.Filename)
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
		env.cache.SHA1 = env.headHash()
	}

	env.cache.Cache = wps

	if env.cfg.CacheEnabled {
		cacheFile, err := os.Create(filepath.Join(env.cfg.DataDir, "cache.gob"))
		if err != nil {
			log.WithFields(logrus.Fields{
				"error": err,
			}).Errorln("Error creating cache file")
			return
		}
		cacheEncoder := gob.NewEncoder(cacheFile)
		err = cacheEncoder.Encode(env.cache)
		if err != nil {
			log.WithFields(logrus.Fields{
				"error": err,
			}).Errorln("Error encoding cache in gob")
			return
		}
		err = cacheFile.Close()
		if err != nil {
			log.WithFields(logrus.Fields{
				"error": err,
			}).Errorln("Error closing cache file")
			return
		}
	}
}

func (env *wikiEnv) loadCache() {
	if env.cfg.CacheEnabled {
		cacheFile, err := os.Open(filepath.Join(env.cfg.DataDir, "cache.gob"))
		defer cacheFile.Close()
		if err == nil {
			log.Println("Loading cache from gob.")
			cacheDecoder := gob.NewDecoder(cacheFile)
			err = cacheDecoder.Decode(&env.cache)
			//check(err)
			if err != nil {
				log.Println("Error loading cache. Rebuilding it.", err)
				env.buildCache()
			}
			// Check the cached sha1 versus HEAD sha1, rebuild if they differ
			if !env.gitIsEmpty() && env.cache.SHA1 != env.headHash() {
				log.Println("Cache SHA1s do not match. Rebuilding cache.")
				env.buildCache()
			}
		}
		// If cache does not exist, build it
		if os.IsNotExist(err) {
			log.Println("Cache does not exist, building it...")
			env.buildCache()
		}
	} else {
		log.Println("Building cache...")
		env.buildCache()
	}
}

func (env *wikiEnv) headHash() string {
	output, err := env.gitCommand("rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		log.Println("Error retrieving SHA1 of wikidata:", err)
		return ""
	}
	return string(output)
}

// This should be all the stuff we need to be refreshed on startup and when pages are saved
func (env *wikiEnv) refreshStuff() {
	env.loadCache()
	env.favs.List = env.cache.Favs
	env.tags.List = env.cache.Tags
}

type mdPreviewJSON struct {
	MD string `json:"md"`
}

func markdownPreview(w http.ResponseWriter, r *http.Request) {
	dec := json.NewDecoder(r.Body)
	var md mdPreviewJSON
	err := dec.Decode(&md)
	if err != nil {
		log.Println("[markdownPreview] error decoding JSON:", err)
		w.Write([]byte(""))
	}
	w.Write([]byte(markdownRender([]byte(md.MD))))
}

// return false if request should be allowed
// return true if request should be rejected
func wikiRejected(fullPath string, wikiExists, isAdmin, isLoggedIn bool) bool {

	log.Debugln("wikiRejected name", fullPath)

	// if wikiExists, read the frontmatter and reject/accept based on frontmatter.Permission
	if wikiExists {

		// If err, reject, and log that error
		f, err := os.Open(fullPath)
		if err != nil {
			log.Println("wikiRejected: Error reading", fullPath, err)
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

// mitigateWiki is a general redirect handler; redirect should be set to true for login mitigations
func mitigateWiki(redirect bool, env *wikiEnv, r *http.Request, w http.ResponseWriter) {
	log.Debugln("mitigateWiki: " + r.Host + r.URL.Path)
	env.authState.SetFlash("Unable to view that.", w)
	if redirect {
		// Use auth.Redirect to redirect while storing the current URL for future use
		auth.Redirect(&env.authState, w, r)
	} else {
		http.Redirect(w, r, "/index", http.StatusSeeOther)
	}

}

// getRenderTime calculates the time an HTTP request took, if the Logger middleware was used
func getRenderTime(c context.Context) string {
	startTime, ok := c.Value(timerKey).(time.Time)
	if !ok {
		log.Debugln("Error: Logger middleware is not used; no startTime found in context.")
		startTime = time.Now()
	}
	elapsed := time.Since(startTime)
	return elapsed.String()
}

func (env *wikiEnv) timer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Throw current time in now, to be fetched by GetRenderTime
		newTime := timeNewContext(r.Context(), time.Now())
		// Get flash from cookie, put into context
		msg := env.authState.GetFlash(r, w)
		f := flashNewContext(newTime, msg)

		next.ServeHTTP(w, r.WithContext(f))
	})
}

func (env *wikiEnv) initialSignup(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !env.authState.AnyUsers() && r.RequestURI != "/signup" {
			log.Println("Need to signup...")
			http.Redirect(w, r, "/signup", http.StatusSeeOther)
		}
		//next.ServeHTTP(w, r.WithContext(newTime))
	})
}

func router(env *wikiEnv) http.Handler {

	// HTTP stuff from here on out
	//s := alice.New(httputils.Timer, httputils.Logger, env.authState.CtxMiddle, env.authState.CSRFProtect(csrfSecure))
	csrfSecure := true
	if debugMode {
		csrfSecure = false
	}
	s := alice.New(env.timer, env.authState.CSRFProtect(csrfSecure))

	r := httptreemux.NewContextMux()

	r.PanicHandler = errorHandler

	r.GET("/", env.indexHandler)

	r.GET("/tags", env.authState.AuthMiddle(env.tagMapHandler))
	r.GET("/tag/*name", env.authState.AuthMiddle(env.tagHandler))

	r.GET("/login", env.loginPageHandler)
	r.GET("/logout", env.authState.LogoutHandler)
	r.GET("/signup", env.signupPageHandler)
	r.GET("/list", env.listHandler)
	r.GET("/search/*name", env.searchHandler)
	r.POST("/search", env.searchHandler)
	r.GET("/recent", env.authState.AuthMiddle(env.recentHandler))
	r.GET("/health", healthCheckHandler)

	admin := r.NewContextGroup("/admin")
	admin.GET("/", env.authState.AuthAdminMiddle(env.adminMainHandler))
	admin.GET("/git", env.authState.AuthAdminMiddle(env.adminGitHandler))
	admin.POST("/git/push", env.authState.AuthAdminMiddle(env.gitPushPostHandler))
	admin.POST("/git/checkin", env.authState.AuthAdminMiddle(env.gitCheckinPostHandler))
	admin.POST("/git/pull", env.authState.AuthAdminMiddle(env.gitPullPostHandler))
	admin.GET("/users", env.authState.AuthAdminMiddle(env.adminUsersHandler))
	admin.POST("/user", env.authState.AuthAdminMiddle(adminUserPostHandler))
	admin.GET("/user/:username", env.authState.AuthAdminMiddle(env.adminUserHandler))
	admin.POST("/user/:username", env.authState.AuthAdminMiddle(env.adminUserHandler))
	admin.POST("/user/generate", env.authState.AuthAdminMiddle(env.adminGeneratePostHandler))

	a := r.NewContextGroup("/auth")
	a.POST("/login", env.authState.LoginPostHandler)
	a.POST("/logout", env.authState.LogoutHandler)
	a.GET("/logout", env.authState.LogoutHandler)
	a.POST("/signup", env.authState.UserSignupTokenPostHandler)

	r.POST("/gitadd", env.authState.AuthMiddle(env.gitCheckinPostHandler))
	r.GET("/gitadd", env.authState.AuthMiddle(env.gitCheckinHandler))

	r.POST("/md_render", markdownPreview)

	r.Handler("GET", "/uploads/*", http.StripPrefix("/uploads/", http.FileServer(http.Dir("uploads"))))

	// Wiki page handlers
	r.GET(`/fav/*name`, env.authState.AuthMiddle(env.wikiMiddle(env.setFavoriteHandler)))
	r.GET(`/edit/*name`, env.authState.AuthMiddle(env.wikiMiddle(env.editHandler)))
	r.POST(`/save/*name`, env.authState.AuthMiddle(env.wikiMiddle(env.saveHandler)))
	r.GET(`/history/*name`, env.authState.AuthMiddle(env.wikiMiddle(env.historyHandler)))
	r.POST(`/delete/*name`, env.authState.AuthMiddle(env.wikiMiddle(env.deleteHandler)))
	r.GET(`/*name`, env.wikiMiddle(env.viewHandler))

	r.Handler("GET", "/debug/vars", expvar.Handler())
	r.GET("/debug/pprof/", env.authState.AuthAdminMiddle(http.HandlerFunc(pprof.Index)))
	r.GET("/debug/pprof/cmdline", env.authState.AuthAdminMiddle(http.HandlerFunc(pprof.Cmdline)))
	r.GET("/debug/pprof/profile", env.authState.AuthAdminMiddle(http.HandlerFunc(pprof.Profile)))
	r.GET("/debug/pprof/symbol", env.authState.AuthAdminMiddle(http.HandlerFunc(pprof.Symbol)))
	r.GET("/debug/pprof/trace", env.authState.AuthAdminMiddle(http.HandlerFunc(pprof.Trace)))
	//r.GET("/robots.txt", robots)
	//r.GET("/favicon.ico", faviconICO)
	//r.GET("/favicon.png", faviconPNG)
	r.Handler("GET", "/assets/*", http.FileServer(http.FS(assetsfs)))

	return s.Then(r)
}

func main() {
	// subscribe to SIGINT signals
	stopChan := make(chan os.Signal)
	signal.Notify(stopChan, os.Interrupt)

	formatter := new(log.TextFormatter)
	formatter.TimestampFormat = "01-02-2006 03:04:05pm"
	formatter.FullTimestamp = true
	formatter.DisableLevelTruncation = true
	log.SetFormatter(formatter)

	confFile := flag.String("conf", "", "Path to the TOML or YAML config file.")
	flag.BoolVar(&debugMode, "debug", false, "Toggle debug logging.")

	flag.Parse()

	if *&debugMode {
		httputils.Debug = true
		log.SetLevel(log.DebugLevel)
	}

	serverCfg := loadConfig(*confFile)

	err := initWikiDir(serverCfg)
	if err != nil {
		log.Fatalln(err)
	}

	env := &wikiEnv{
		cfg:       serverCfg,
		authState: *auth.NewAuthState(filepath.Join(serverCfg.DataDir, "auth.db")),
		templates: tmplInit(),
		mutex:     sync.Mutex{},
		cache:     new(wikiCache),
	}
	env.cache.Tags = make(map[string][]string)
	env.cache.Favs = make(map[string]struct{})

	env.loadCache()
	env.favs.List = env.cache.Favs
	env.tags.List = env.cache.Tags

	// Check for unclean Git dir on startup
	if !env.gitIsEmpty() {
		err := env.gitIsCleanStartup()
		if err != nil {
			log.Fatalln("There was an issue with the git repo:", err)
		}
	}

	/*
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
	*/

	httputils.Logfile = filepath.Join(serverCfg.DataDir, "http.log")

	log.Println("Listening on 127.0.0.1:" + serverCfg.Port)

	srv := &http.Server{
		Addr:    "127.0.0.1:" + serverCfg.Port,
		Handler: router(env),
		// Good practice: enforce timeouts for servers you create!
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
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
