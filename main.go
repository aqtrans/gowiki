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

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	//"github.com/golang-commonmark/markdown"
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

	"github.com/Machiel/slugify"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/justinas/alice"
	"github.com/oxtoacart/bpool"
	"github.com/russross/blackfriday"
	"github.com/spf13/viper"
	"github.com/thoas/stats"
	"gopkg.in/yaml.v2"
	"jba.io/go/auth"
	"jba.io/go/utils"
	//"net/url"
)

const (
	commonHtmlFlags = 0 |
		blackfriday.HTML_USE_SMARTYPANTS |
		blackfriday.HTML_SMARTYPANTS_FRACTIONS |
		blackfriday.HTML_SMARTYPANTS_DASHES |
		blackfriday.HTML_SMARTYPANTS_LATEX_DASHES |
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
)

func markdownCommon(input []byte) []byte {
	renderer := blackfriday.HtmlRenderer(commonHtmlFlags, "", "")
	return blackfriday.MarkdownOptions(input, renderer, blackfriday.Options{
		Extensions: commonExtensions})
}

type configuration struct {
	Port     string
	Email    string
	WikiDir  string
	MainTLD  string
	GitRepo  string
	AuthConf auth.AuthConf
}

var (
	bufpool   *bpool.BufferPool
	templates map[string]*template.Template
	_24K      int64 = (1 << 20) * 24
	fLocal    bool
	debug     = utils.Debug
	fInit     bool
	cfg       = configuration{}
	gitPath   string
	favbuf    bytes.Buffer
	//sessID   string
	tagMap  map[string][]string
	tagsBuf bytes.Buffer
)

//Base struct, page ; has to be wrapped in a data {} strut for consistency reasons
type page struct {
	SiteName string
	Favs     []string
	UN       string
	Role     string
	Token    string
	FlashMsg string
}

type frontmatter struct {
	Title    string   `yaml:"title"`
	Tags     []string `yaml:"tags,omitempty"`
	Favorite bool     `yaml:"favorite,omitempty"`
	Private  bool     `yaml:"private,omitempty"`
	Admin    bool     `yaml:"admin,omitempty"`
}

type badFrontmatter struct {
	Title    string `yaml:"title"`
	Tags     string `yaml:"tags,omitempty"`
	Favorite bool   `yaml:"favorite,omitempty"`
	Private  bool   `yaml:"private,omitempty"`
	Admin    bool   `yaml:"admin,omitempty"`
}

type wiki struct {
	Rendered string
	Content  string
}

type wikiPage struct {
	*page
	PageTitle string
	Filename  string
	*frontmatter
	*wiki
	CreateTime int64
	ModTime    int64
}

type commitPage struct {
	*page
	PageTitle string
	Filename  string
	*frontmatter
	*wiki
	CreateTime int64
	ModTime    int64
	Commit     string
	Content    string
}

type rawPage struct {
	Name    string
	Content []byte
}

type listPage struct {
	*page
	Wikis        []*wikiPage
	PrivateWikis []*wikiPage
	AdminWikis   []*wikiPage
}

type genPage struct {
	*page
	Title string
}

type gitPage struct {
	*page
	Title    string
	GitFiles string
}

type historyPage struct {
	*page
	Filename    string
	FileHistory []*commitLog
}

type tagMapPage struct {
	*page
	TagKeys map[string][]string
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

var urlSlugifier = slugify.New(slugify.Configuration{
	IsValidCharacterChecker: func(c rune) bool {
		if c >= 'a' && c <= 'z' {
			return true
		}

		if c >= '0' && c <= '9' {
			return true
		}

		if c == '/' {
			return true
		}

		if c == '.' {
			return true
		}

		return false
	},
})

// Sorting functions
type wikiByDate []*wikiPage

func (a wikiByDate) Len() int           { return len(a) }
func (a wikiByDate) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a wikiByDate) Less(i, j int) bool { return a[i].CreateTime < a[j].CreateTime }

type wikiByModDate []*wikiPage

func (a wikiByModDate) Len() int           { return len(a) }
func (a wikiByModDate) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a wikiByModDate) Less(i, j int) bool { return a[i].ModTime < a[j].ModTime }

func init() {

	// Viper config
	viper.SetConfigName("conf")
	viper.AddConfigPath(".")
	err := viper.ReadInConfig() // Find and read the config file
	if err != nil {             // Handle errors reading the config file
		//panic(fmt.Errorf("Fatal error config file: %s \n", err))
		fmt.Println("No configuration file loaded - using defaults")
	}
	viper.SetConfigType("json")
	viper.WatchConfig()
	/*
			Port     string
			Email    string
			WikiDir  string
			MainTLD  string
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
	viper.SetDefault("Port", "3000")
	viper.SetDefault("Email", "unused@the.moment")
	viper.SetDefault("WikiDir", "./md/")
	viper.SetDefault("MainTLD", "wiki.jba.io")
	viper.SetDefault("GitRepo", "git@jba.io:conf/gowiki-data.git")
	defaultauthstruct := &auth.AuthConf{
		LdapEnabled: false,
		LdapConf:    auth.LdapConf{},
	}
	viper.SetDefault("AuthConf", defaultauthstruct)
	viper.Unmarshal(&cfg)

	//log.Println(&cfg)

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

	//var err error
	gitPath, err = exec.LookPath("git")
	if err != nil {
		log.Fatal("git must be installed")
	}

	// Crawl for new favorites only on startup and save
	err = filepath.Walk("./md", readFavs)
	if err != nil {
		log.Fatal(err)
	}

	// Crawl for tags only on startup and save
	err = filepath.Walk("./md", readTags)
	if err != nil {
		log.Fatal(err)
	}

}

func fullName(r *http.Request) string {
	vars := mux.Vars(r)
	name := vars["name"]
	dir := vars["dir"]
	if dir != "" {
		name = dir + "/" + name
	}
	return name
}

// Turn the given URL into a slug
// Redirect to slugURL if it is different from input
func slugURL(w http.ResponseWriter, r *http.Request) {
	name := fullName(r)
	//log.Println(name)
	//log.Println(r.URL.String())

	// In case a non-slugified filename was created outside, check for existence before we react
	fullfilename := cfg.WikiDir + name
	if _, err := os.Stat(fullfilename); err == nil {
		return
	}

	slugName := urlSlugifier.Slugify(name)
	if name != slugName {
		//log.Println(name + " and " + slugName + " differ.")
		//log.Println(strings.Replace(r.URL.String(), name, slugName, 1))
		//log.Println(r.URL.RequestURI())
		http.Redirect(w, r, "/"+slugName+"?"+r.URL.RawQuery, http.StatusTemporaryRedirect)
		return
	}
	//log.Println(r.URL)
	//log.Println(r.URL.RawQuery)
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
	log.Println(tags)
	return tags
}

// CUSTOM GIT WRAPPERS
// Construct an *exec.Cmd for `git {args}` with a workingDirectory
func gitCommand(args ...string) *exec.Cmd {
	c := exec.Command(gitPath, args...)
	//c.Dir = "./md"
	c.Dir = cfg.WikiDir
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
func gitPush(msg string) error {
	o, err := gitCommand("push").CombinedOutput()
	if err != nil {
		return errors.New(fmt.Sprintf("error during `git push`: %s\n%s", err.Error(), string(o)))
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
		log.Println(err)
	}

	return mtime, nil
}

// File history
// git log --pretty=format:"commit:%H date:%at message:%s" [filename]
// git log --pretty=format:"%H,%at,%s" [filename]
func gitGetLog(filename string) ([]*commitLog, error) {
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
		log.Println(err)
	}
	return mtime, nil
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

	md := markdownCommon(content)
	mds := string(md)

	//log.Println("MDS:"+ mds)
	return mds
}

func loadPage(r *http.Request) (*page, error) {
	//timer.Step("loadpageFunc")

	// Auth lib middlewares should load the user and tokens into context for reading
	user, role, msg := auth.GetUsername(r)
	token := auth.GetToken(r)

	//log.Println("Message: ")
	//log.Println(msg)

	var message string
	if msg != "" {
		message = `
    <input id="alert_modal" type="checkbox" checked />
    <label for="alert_modal" class="overlay"></label>
        <article>
            <header>Alert!</header>
            <section>
            <label for="alert_modal" class="close">&times;</label>
                ` + template.HTMLEscapeString(msg) + `
                <hr>
            <label for="alert_modal" class="button">
                Okay
            </label>
            </section>
        </article>        
        `
	} else {
		message = ""
	}

	// Grab list of favs from channel
	favs := make(chan []string)
	go favsHandler(favs)
	gofavs := <-favs

	//log.Println(gofavs)
	return &page{SiteName: "GoWiki", Favs: gofavs, UN: user, Role: role, Token: token, FlashMsg: message}, nil
}

func historyHandler(w http.ResponseWriter, r *http.Request) {

	slugURL(w, r)

	name := fullName(r)

	p, err := loadPage(r)
	if err != nil {
		log.Fatalln(err)
	}
	history, err := gitGetLog(name)
	if err != nil {
		log.Fatalln(err)
	}
	hp := &historyPage{
		p,
		name,
		history,
	}
	err = renderTemplate(w, "wiki_history.tmpl", hp)
	if err != nil {
		log.Fatalln(err)
	}
}

// Need to get content of the file at specified commit
// > git show [commit sha1]:[filename]
// As well as the date
// > git log -1 --format=%at [commit sha1]
// TODO: need to find a way to detect sha1s
func viewCommitHandler(w http.ResponseWriter, r *http.Request, commit string) {
	var fm frontmatter
	var pagetitle string
	var pageContent string

	slugURL(w, r)

	name := fullName(r)

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
	fm, content, err := readFront(body)
	if err != nil {
		log.Println(err)
	}
	if content == nil {
		content = body
	}
	// Render remaining content after frontmatter
	md := markdownRender(content)
	if fm.Private {
		log.Println("Private page!")
	}
	if fm.Title != "" {
		pagetitle = fm.Title
	} else {
		pagetitle = name
	}
	diffstring := strings.Replace(string(diff), "\n", "<br>", -1)
	//log.Println(diffstring)

	pageContent = md

	// Check for ?a={file,diff} and toss either the file or diff
	if r.URL.Query().Get("a") != "" {
		action := r.URL.Query().Get("a")
		//log.Println(action)
		if action == "diff" {
			pageContent = "<code>" + diffstring + "</code>"
		} else {
			pageContent = md
		}
	}

	cp := &commitPage{
		p,
		pagetitle,
		name,
		&fm,
		&wiki{
			Rendered: md,
			Content:  string(content),
		},
		ctime,
		mtime,
		commit,
		pageContent,
	}

	// Check for ?a={file,diff} and toss either the file or diff
	if r.URL.Query().Get("a") != "" {
		action := r.URL.Query().Get("a")
		//log.Println(action)
		if action == "diff" {
			err = renderTemplate(w, "wiki_commit_diff.tmpl", cp)
			if err != nil {
				log.Fatalln(err)
			}
		} else {
			err = renderTemplate(w, "wiki_commit.tmpl", cp)
			if err != nil {
				log.Fatalln(err)
			}
		}
	} else {
		err = renderTemplate(w, "wiki_commit.tmpl", cp)
		if err != nil {
			log.Fatalln(err)
		}
	}

}

func listHandler(w http.ResponseWriter, r *http.Request) {
	searchDir := cfg.WikiDir
	p, err := loadPage(r)
	if err != nil {
		log.Fatalln(err)
	}

	fileList := []string{}
	_ = filepath.Walk(searchDir, func(path string, f os.FileInfo, err error) error {
		// check and skip .git
		if f.IsDir() && f.Name() == ".git" {
			return filepath.SkipDir
		}
		fileList = append(fileList, path)
		return nil
	})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(200)
	var wps []*wikiPage
	var privatewps []*wikiPage
	var adminwps []*wikiPage
	for _, file := range fileList {

		// check if the source dir exist
		src, err := os.Stat(file)
		if err != nil {
			panic(err)
		}
		// check if its a directory
		if src.IsDir() {
			// Do something if a directory
			// TODO: Do we need to do anything if it's a directory?
			//       The filewalk already descends automatically.
			//log.Println(file + " is a directory.")

		} else {

			_, filename := filepath.Split(file)

			// If this is an absolute path, including the cfg.WikiDir, trim it
			withoutWikidir := strings.TrimPrefix(cfg.WikiDir, "./")
			fileURL := strings.TrimPrefix(file, withoutWikidir)

			var wp *wikiPage
			var fm frontmatter
			var pagetitle string
			//fmt.Println(file)
			//w.Write([]byte(file))
			//w.Write([]byte("<br>"))

			pagetitle = filename
			//log.Println(file)
			body, err := ioutil.ReadFile(file)
			if err != nil {
				log.Fatalln(err)
			}
			// Read YAML frontmatter into fm
			fm, _, err = readFront(body)
			if err != nil {
				// If YAML frontmatter doesn't exist, proceed, but log it
				//log.Fatalln(err)
				log.Println("YAML unmarshal error in: " + file)
				log.Println(err)
			}
			if fm.Private {
				//log.Println("Private page!")
			}
			if fm.Title != "" {
				pagetitle = fm.Title
			}
			if fm.Title == "" {
				fm.Title = fileURL
			}
			if fm.Private != true {
				fm.Private = false
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
			ctime, err := gitGetCtime(fileURL)
			if err != nil && err.Error() != "NOT_IN_GIT" {
				log.Panicln(err)
			}
			mtime, err := gitGetMtime(fileURL)
			if err != nil {
				log.Panicln(err)
			}

			// If pages are Admin or Private, add to a separate wikiPage slice
			//   So we only check on rendering
			if fm.Admin {
				wp = &wikiPage{
					p,
					pagetitle,
					fileURL,
					&fm,
					&wiki{},
					ctime,
					mtime,
				}
				adminwps = append(adminwps, wp)
			} else if fm.Private {
				wp = &wikiPage{
					p,
					pagetitle,
					fileURL,
					&fm,
					&wiki{},
					ctime,
					mtime,
				}
				privatewps = append(privatewps, wp)
			} else {
				wp = &wikiPage{
					p,
					pagetitle,
					fileURL,
					&fm,
					&wiki{},
					ctime,
					mtime,
				}
				wps = append(wps, wp)
			}
			//log.Println(string(body))
			//log.Println(string(wp.wiki.Content))
		}

	}
	l := &listPage{p, wps, privatewps, adminwps}
	err = renderTemplate(w, "list.tmpl", l)
	if err != nil {
		log.Fatalln(err)
	}
}

func readFront(data []byte) (fm frontmatter, content []byte, err error) {
	defer utils.TimeTrack(time.Now(), "readFront")
	r := bytes.NewBuffer(data)

	// eat away starting whitespace
	var ch rune = ' '
	for unicode.IsSpace(ch) {
		ch, _, err = r.ReadRune()
		if err != nil {
			// file is just whitespace
			return frontmatter{}, []byte{}, nil
		}
	}
	r.UnreadRune()

	// check if first line is ---
	line, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		return frontmatter{}, nil, err
	}

	if strings.TrimSpace(line) != "---" {
		// no front matter, just content
		return frontmatter{}, data, nil
	}

	yamlStart := len(data) - r.Len()
	yamlEnd := yamlStart

	for {
		line, err = r.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return frontmatter{}, data, nil
			}
			return frontmatter{}, nil, err
		}

		if strings.TrimSpace(line) == "---" {
			yamlEnd = len(data) - r.Len()
			break
		}
	}

	m := map[string]interface{}{}
	err = yaml.Unmarshal(data[yamlStart:yamlEnd], &fm)
	if err != nil {
		err = yaml.Unmarshal(data[yamlStart:yamlEnd], &m)
		if err != nil {
			return frontmatter{}, nil, err
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
		private, found := m["private"].(bool)
		if found {
			fm.Private = private
		}
		admin, found := m["admin"].(bool)
		if found {
			fm.Admin = admin
		}
	}
	return fm, data[yamlEnd:], nil
}

func renderTemplate(w http.ResponseWriter, name string, data interface{}) error {
	tmpl, ok := templates[name]
	if !ok {
		return fmt.Errorf("The template %s does not exist", name)
	}

	// Create buffer to write to and check for errors
	buf := bufpool.Get()
	err := tmpl.ExecuteTemplate(buf, "base", data)
	if err != nil {
		log.Println(err)
		bufpool.Put(buf)
		return err
	}

	// Set the header and write the buffer to w
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
	bufpool.Put(buf)
	return nil
}

func parseBool(value string) bool {
	boolValue, err := strconv.ParseBool(value)
	if err != nil {
		return false
	}
	return boolValue
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
func loadWikiPage(r *http.Request) (*wikiPage, error) {

	name := fullName(r)

	return loadWikiPageHelper(r, name)
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	defer utils.TimeTrack(time.Now(), "indexHandler")

	// In case I want to switch to queries some time

	//log.Println(r.URL.Query())
	query := r.URL.RawQuery
	if query != "" {
		//utils.Debugln("Query string: " + query)
	}
	if r.URL.Query().Get("commit") != "" {
		commit := r.URL.Query().Get("commit")
		//utils.Debugln(r.URL.Query().Get("commit"))
		viewCommitHandler(w, r, commit)
		return
	}

	// Get Wiki
	p, err := loadWikiPageHelper(r, "index")
	if err != nil {
		//log.Println(err.Error())
		http.NotFound(w, r)
		return
	}
	err = renderTemplate(w, "wiki_view.tmpl", p)
	if err != nil {
		panic(err)
	}
}

func viewHandler(w http.ResponseWriter, r *http.Request) {
	defer utils.TimeTrack(time.Now(), "viewHandler")

	slugURL(w, r)
	name := fullName(r)

	// In case I want to switch to queries some time
	query := r.URL.RawQuery
	if query != "" {
		//utils.Debugln(query)
	}
	if r.URL.Query().Get("commit") != "" {
		commit := r.URL.Query().Get("commit")
		//utils.Debugln(r.URL.Query().Get("commit"))
		viewCommitHandler(w, r, commit)
		return
	}

	// Get Wiki
	p, err := loadWikiPage(r)
	if err != nil {
		if err.Error() == "No such dir index" {
			log.Println("No such dir index...creating one.")
			http.Redirect(w, r, "/"+name+"?a=edit", http.StatusTemporaryRedirect)
			return
		} else if err.Error() == "No such file" {
			log.Println("No such file...creating one.")
			http.Redirect(w, r, "/"+name+"?a=edit", http.StatusTemporaryRedirect)
			return
		} else if err.Error() == "Base is not dir" {
			log.Println("Cannot create subdir of a file.")
			http.Error(w, "Cannot create subdir of a file.", 500)
			return
			// If gitGetCtime returns NOT_IN_GIT, we handle it here
		} else if err.Error() == "NOT_IN_GIT" {
			http.Redirect(w, r, "/gitadd?file="+name, http.StatusSeeOther)
			return
		} else {
			panic(err)
		}
		//log.Println(err.Error())
		http.NotFound(w, r)
		return
	}
	err = renderTemplate(w, "wiki_view.tmpl", p)
	if err != nil {
		panic(err)
	}
	//log.Println(p.Title + " Page rendered!")
}

func loadWikiPageHelper(r *http.Request, name string) (*wikiPage, error) {
	var fm frontmatter
	var pagetitle string
	var body []byte

	p, err := loadPage(r)
	if err != nil {
		log.Fatalln(err)
	}

	log.Println("Filename:" + name)

	fullfilename := cfg.WikiDir + name
	rel, err := filepath.Rel(cfg.WikiDir, fullfilename)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(rel, "../") {
		return nil, errors.New("BAD_PATH")
	}
	base := filepath.Dir(fullfilename)

	//log.Println(base)
	//log.Println(dir)
	//log.Println(filename)

	_, fierr := os.Stat(fullfilename)
	if fierr != nil {
		//log.Println(fierr)
	}

	// Check if the base of the given filename is actually a file
	// If so, bail, return 500.
	basefile, _ := os.Open("./" + base)
	basefi, basefierr := basefile.Stat()
	basefimode := basefi.Mode()
	if !basefimode.IsDir() {
		errn := errors.New("Base is not dir")
		//http.Error(w, basefi.Name()+" is not a directory.", 500)
		return nil, errn
	}
	if basefimode.IsRegular() {
		errn := errors.New("Base is not dir")
		//http.Error(w, basefi.Name()+" is not a directory.", 500)
		return nil, errn
	}
	//log.Println(base)
	if basefierr != nil {
		log.Println(basefierr)
	}

	// Directory without specified index
	if strings.HasSuffix(name, "/") {
		//if dir != "" && name == "" {
		log.Println("This might be a directory, trying to parse the index")
		filename := name + "index"
		fullfilename = cfg.WikiDir + name + "index"

		dirindex, _ := os.Open(fullfilename)
		_, dirindexfierr := dirindex.Stat()

		if os.IsNotExist(dirindexfierr) {
			//filename = name + "index"
			title := name + " - Index"
			// FIXME: logic looks wrong here; should probably have another if/else
			// ...Checking if file exists, before throwing an error
			errn := errors.New("No such dir index")
			newwp := &wikiPage{
				p,
				title,
				filename,
				&frontmatter{
					Title: title,
				},
				&wiki{},
				0,
				0,
			}
			return newwp, errn
		}

	}

	if os.IsNotExist(fierr) {
		// NOW: Using os.Stat to properly check for file existence, using IsNotExist()
		// This should mean file is non-existent, so create new page
		log.Println(fierr)
		errn := errors.New("No such file")
		newwp := &wikiPage{
			p,
			name,
			name,
			&frontmatter{
				Title: name,
			},
			&wiki{},
			0,
			0,
		}
		return newwp, errn
	}
	body, err = ioutil.ReadFile(fullfilename)
	if err != nil {
		// FIXME Not sure what to do here, probably panic?
		return nil, err
	}
	// Read YAML frontmatter into fm
	fm, content, err := readFront(body)
	if err != nil {
		log.Println("YAML unmarshal error in: " + name)
		log.Println(err)
	}
	if content == nil {
		content = body
	}
	// Render remaining content after frontmatter
	md := markdownRender(content)

	// TODO: improve this so private pages are actually protected
	if fm.Private {
		log.Println("Private page!")
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
		log.Panicln(err)
	}
	mtime, err := gitGetMtime(name)
	if err != nil {
		log.Panicln(err)
	}
	wp := &wikiPage{
		p,
		pagetitle,
		name,
		&fm,
		&wiki{
			Rendered: md,
			Content:  string(content),
		},
		ctime,
		mtime,
	}
	return wp, nil
}

func editHandler(w http.ResponseWriter, r *http.Request) {
	defer utils.TimeTrack(time.Now(), "editHandler")

	slugURL(w, r)

	p, err := loadWikiPage(r)
	//log.Println(p.Filename)
	//log.Println(p.PageTitle)
	if err != nil {
		if err.Error() == "No such file" {
			//log.Println("No such file...creating one.")
			terr := renderTemplate(w, "wiki_edit.tmpl", p)
			if terr != nil {
				log.Fatalln(terr)
			}
		} else {
			log.Fatalln(err)
		}
	} else {
		terr := renderTemplate(w, "wiki_edit.tmpl", p)
		if terr != nil {
			log.Fatalln(terr)
		}
	}
}

func saveHandler(w http.ResponseWriter, r *http.Request) {
	defer utils.TimeTrack(time.Now(), "saveHandler")

	slugURL(w, r)
	name := fullName(r)

	r.ParseForm()
	//txt := r.Body
	content := r.FormValue("editor")
	//bwiki := txt

	// Check for and install required YAML frontmatter
	title := r.FormValue("title")
	tags := r.FormValue("tags")
	var tagsA []string
	favorite := r.FormValue("favorite")
	private := r.FormValue("private")
	admin := r.FormValue("admin")

	favoritebool := false
	if favorite == "on" {
		favoritebool = true
	}
	privatebool := false
	if private == "on" {
		privatebool = true
	}
	adminbool := false
	if admin == "on" {
		adminbool = true
	}

	if title == "" {
		title = name
	}

	if tags != "" {
		tagsA = strings.Split(tags, ",")
	}

	//var buffer bytes.Buffer
	buffer := new(bytes.Buffer)

	bfm := &frontmatter{
		Title:    title,
		Tags:     tagsA,
		Favorite: favoritebool,
		Private:  privatebool,
		Admin:    adminbool,
	}
	_, err := buffer.Write([]byte("---\n"))
	if err != nil {
		log.Fatalln(err)
		return
	}
	yamlBuffer, err := yaml.Marshal(bfm)
	if err != nil {
		log.Fatalln(err)
		return
	}
	buffer.Write(yamlBuffer)

	_, err = buffer.Write([]byte("---\n"))
	if err != nil {
		log.Fatalln(err)
		return
	}
	buffer.Write([]byte(content))

	/*
		    buffer.WriteString("---\n")
		    buffer.WriteString("title: " + title)
		    buffer.WriteString("\n")
		    if tags != "" {
		        buffer.WriteString("tags: [ " + tags + " ]")
		        buffer.WriteString("\n")
		    }
		    buffer.WriteString("favorite: " + strconv.FormatBool(favoritebool))
		    buffer.WriteString("\n")
		    buffer.WriteString("private: " + strconv.FormatBool(privatebool))
		    buffer.WriteString("\n")
		    buffer.WriteString("admin: " + strconv.FormatBool(adminbool))
		    buffer.WriteString("\n")
		    buffer.WriteString("---\n")
		    buffer.WriteString(content)
			body := buffer.String()
	*/

	rp := &rawPage{
		name,
		buffer.Bytes(),
	}

	err = rp.save()
	if err != nil {
		auth.SetSession("flash", "Failed to save page.", w, r)
		http.Redirect(w, r, "/", http.StatusSeeOther)
		log.Fatalln(err)
		return
	}

	// Crawl for new favorites only on startup and save
	favbuf.Reset()
	err = filepath.Walk(cfg.WikiDir, readFavs)
	if err != nil {
		log.Fatal(err)
	}

	auth.SetSession("flash", "Wiki page successfully saved.", w, r)
	log.Println(name)
	http.Redirect(w, r, "/"+name, http.StatusSeeOther)
	log.Println(name + " page saved!")
}

func setFlash(msg string, w http.ResponseWriter, r *http.Request) {
	auth.SetSession("flash", msg, w, r)
}

func newHandler(w http.ResponseWriter, r *http.Request) {
	defer utils.TimeTrack(time.Now(), "newHandler")
	pagetitle := r.FormValue("newwiki")

	fullfilename := cfg.WikiDir + pagetitle
	rel, err := filepath.Rel(cfg.WikiDir, fullfilename)
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

	slugName := urlSlugifier.Slugify(pagetitle)
	if slugName != pagetitle {
		pagetitle = slugName
	}

	_, fierr := os.Stat(pagetitle)
	if os.IsNotExist(fierr) {
		http.Redirect(w, r, pagetitle+"?a=edit", http.StatusTemporaryRedirect)
		return
	} else if fierr != nil {
		panic(fierr)
	}

	http.Redirect(w, r, pagetitle, http.StatusTemporaryRedirect)
	return

}

func urlFromPath(path string) string {
	url := filepath.Clean(cfg.WikiDir) + "/"
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

	read, err := ioutil.ReadFile(path)
	if err != nil {
		log.Print(err)
		return nil
	}

	name := urlFromPath(path)

	// Read YAML frontmatter into fm
	// If err, just return, as file should not contain frontmatter
	var fm frontmatter
	fm, _, err = readFront(read)
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
		//log.Println("tagMap is blank")
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

	read, err := ioutil.ReadFile(path)
	if err != nil {
		log.Println(err)
		return nil
	}

	name := urlFromPath(path)

	// Read YAML frontmatter into fm
	// If err, just return, as file should not contain frontmatter
	var fm frontmatter
	fm, _, err = readFront(read)
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

func (wiki *rawPage) save() error {
	defer utils.TimeTrack(time.Now(), "wiki.save()")

	dir, filename := filepath.Split(wiki.Name)
	fullfilename := cfg.WikiDir + dir + filename

	// If directory doesn't exist, create it
	// - Check if dir is null first
	if dir != "" {
		dirpath := cfg.WikiDir + dir
		if _, err := os.Stat(dirpath); os.IsNotExist(err) {
			err := os.MkdirAll(dirpath, 0755)
			if err != nil {
				return err
			}
		}
	}

	ioutil.WriteFile(fullfilename, wiki.Content, 0755)

	gitfilename := dir + filename

	err := gitAddFilepath(gitfilename)
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
	err = renderTemplate(w, "login.tmpl", gp)
	if err != nil {
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
	err = renderTemplate(w, "signup.tmpl", gp)
	if err != nil {
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
	err = renderTemplate(w, "admin_users.tmpl", data)
	if err != nil {
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

	vars := mux.Vars(r)
	selectedUser := vars["username"]

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
	err = renderTemplate(w, "admin_user.tmpl", data)
	if err != nil {
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
	err = renderTemplate(w, "admin_main.tmpl", gp)
	if err != nil {
		log.Println(err)
		return
	}
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
		//log.Println(action)
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
	}
	err = renderTemplate(w, "git_checkin.tmpl", gp)
	if err != nil {
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
	err = renderTemplate(w, "tag_list.tmpl", tagpage)
	if err != nil {
		log.Println(err)
		return
	}

}

func wikiAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		name := vars["name"]
		dir := vars["dir"]
		log.Println("Name " + name)
		log.Println("Dir " + dir)

		wikipage := cfg.WikiDir + r.URL.Path
		//log.Println(wikipage)

		_, fierr := os.Stat(wikipage)
		if fierr != nil {
			//log.Println(fierr)
			next.ServeHTTP(w, r)
			return
		}

		if os.IsNotExist(fierr) {
			next.ServeHTTP(w, r)
			return
		}

		read, err := ioutil.ReadFile(wikipage)
		if err != nil {
			log.Println(err)
		}

		// Read YAML frontmatter into fm
		// If err, just return, as file should not contain frontmatter
		var fm frontmatter
		fm, _, err = readFront(read)
		if err != nil {
			log.Println("YAML unmarshal error in: " + dir + "/" + name)
			log.Println(err)
			return
		}

		username, role, _ := auth.GetUsername(r)

		if fm.Private || fm.Admin {
			if username == "" {
				rurl := r.URL.String()
				utils.Debugln("AuthMiddleware mitigating: " + r.Host + rurl)
				//w.Write([]byte("OMG"))

				// Detect if we're in an endless loop, if so, just panic
				if strings.HasPrefix(rurl, "login?url=/login") {
					panic("AuthMiddle is in an endless redirect loop")
					return
				}
				auth.SetSession("flash", "Please login to view that page.", w, r)
				http.Redirect(w, r, "http://"+r.Host+"/login"+"?url="+rurl, http.StatusSeeOther)
				return
			}
		}
		if fm.Admin {
			if role != "Admin" {
				log.Println(username + " attempting to access restricted URL.")
				auth.SetSession("flash", "Sorry, you are not allowed to see that.", w, r)
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
		}

		// Directory checking
		// Check dir/index for a private or admin flag, and use this for the entire directory contents
		if dir != "" {
			fi, _ := os.Stat(cfg.WikiDir + dir)
			if fi.IsDir() {
				dirindex, _ := os.Open(cfg.WikiDir + dir + "/" + "index")
				_, dirindexfierr := dirindex.Stat()
				if !os.IsNotExist(dirindexfierr) {
					dread, err := ioutil.ReadFile(cfg.WikiDir + dir + "/" + "index")
					if err != nil {
						log.Println(err)
					}

					// Read YAML frontmatter into fm
					// If err, just return, as file should not contain frontmatter
					var dfm frontmatter
					dfm, _, err = readFront(dread)
					if err != nil {
						log.Println(err)
						return
					}

					username, role, _ := auth.GetUsername(r)

					if dfm.Private || dfm.Admin {
						if username == "" {
							rurl := r.URL.String()
							utils.Debugln("AuthMiddleware mitigating due to " + dir + "/index: " + r.Host + rurl)
							//w.Write([]byte("OMG"))

							// Detect if we're in an endless loop, if so, just panic
							if strings.HasPrefix(rurl, "login?url=/login") {
								panic("AuthMiddle is in an endless redirect loop")
								return
							}
							auth.SetSession("flash", "This directory is private. You must login to view any pages within.", w, r)
							http.Redirect(w, r, "http://"+r.Host+"/login"+"?url="+rurl, http.StatusSeeOther)
							return
						}
					}
					if dfm.Admin {
						if role != "Admin" {
							log.Println(username + " attempting to access restricted URL.")
							auth.SetSession("flash", "Sorry, this directory is private.", w, r)
							http.Redirect(w, r, "/", http.StatusSeeOther)
							return
						}
					}
				}
			}

		}

		next.ServeHTTP(w, r)
	})
}

func HealthCheckHandler(w http.ResponseWriter, r *http.Request) {
	// A very simple health check.
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")

	// In the future we could report back on the status of our DB, or our cache
	// (e.g. Redis) by performing a simple PING, and include them in the response.
	io.WriteString(w, `{"alive": true}`)
}

func Router(r *mux.Router) *mux.Router {
	statsdata := stats.New()

	//wiki := s.Append(checkWikiGit)
	//wikiauth := wiki.Append(wikiAuth)

	//r := mux.NewRouter()
	//d := r.Host("go.jba.io").Subrouter()
	r.HandleFunc("/", indexHandler).Methods("GET")

	r.HandleFunc("/tags", tagMapHandler)
	r.HandleFunc("/panic", func(w http.ResponseWriter, r *http.Request) {
		panic("Unexpected error!")
		//http.Error(w, panic("Unexpected error!"), http.StatusInternalServerError)
	})

	r.HandleFunc("/new", auth.AuthMiddle(newHandler)).Methods("GET")
	//r.HandleFunc("/login", auth.LoginPostHandler).Methods("POST")
	r.HandleFunc("/login", loginPageHandler).Methods("GET")
	//r.HandleFunc("/logout", auth.LogoutHandler).Methods("POST")
	r.HandleFunc("/logout", auth.LogoutHandler).Methods("GET")
	r.HandleFunc("/signup", signupPageHandler).Methods("GET")
	r.HandleFunc("/list", listHandler).Methods("GET")
	r.HandleFunc("/health", HealthCheckHandler).Methods("GET")

	admin := r.PathPrefix("/admin").Subrouter()
	admin.HandleFunc("/", auth.AuthAdminMiddle(adminMainHandler)).Methods("GET")
	admin.HandleFunc("/users", auth.AuthAdminMiddle(adminUsersHandler)).Methods("GET")
	admin.HandleFunc("/users", auth.AuthAdminMiddle(auth.UserSignupPostHandler)).Methods("POST")
	admin.HandleFunc("/user", auth.AuthAdminMiddle(adminUserPostHandler)).Methods("POST")
	admin.HandleFunc("/user/{username}", auth.AuthAdminMiddle(adminUserHandler)).Methods("GET")
	admin.HandleFunc("/user/{username}", auth.AuthAdminMiddle(adminUserHandler)).Methods("POST")

	a := r.PathPrefix("/auth").Subrouter()
	a.HandleFunc("/login", auth.LoginPostHandler).Methods("POST")
	a.HandleFunc("/logout", auth.LogoutHandler).Methods("POST")
	a.HandleFunc("/logout", auth.LogoutHandler).Methods("GET")
	a.HandleFunc("/signup", auth.SignupPostHandler).Methods("POST")

	//r.HandleFunc("/signup", auth.SignupPostHandler).Methods("POST")

	r.HandleFunc("/gitadd", auth.AuthMiddle(gitCheckinPostHandler)).Methods("POST")
	r.HandleFunc("/gitadd", auth.AuthMiddle(gitCheckinHandler)).Methods("GET")

	r.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		stats := statsdata.Data()
		b, _ := json.Marshal(stats)
		w.Write(b)
	})

	r.PathPrefix("/uploads/").Handler(http.StripPrefix("/uploads/", http.FileServer(http.Dir("uploads"))))

	//r.HandleFunc("/{name:.*}", wikiHandler)

	// wiki functions, should accept alphanumerical, "_", "-", ".", "@"
	r.HandleFunc(`/{name}`, auth.AuthMiddle(editHandler)).Methods("GET").Queries("a", "edit")
	r.HandleFunc(`/{name}`, auth.AuthMiddle(saveHandler)).Methods("POST").Queries("a", "save")
	r.Handle(`/{name}`, alice.New(wikiAuth).ThenFunc(historyHandler)).Methods("GET").Queries("a", "history")
	r.Handle(`/{name}`, alice.New(wikiAuth).ThenFunc(viewHandler)).Methods("GET")

	// With dirs:
	r.HandleFunc(`/{dir}/{name}`, auth.AuthMiddle(editHandler)).Methods("GET").Queries("a", "edit")
	r.HandleFunc(`/{dir}/{name}`, auth.AuthMiddle(saveHandler)).Methods("POST").Queries("a", "save")
	r.Handle(`/{dir}/{name}`, alice.New(wikiAuth).ThenFunc(historyHandler)).Methods("GET").Queries("a", "history")
	r.Handle(`/{dir}/{name}`, alice.New(wikiAuth).ThenFunc(viewHandler)).Methods("GET")

	//r.NotFoundHandler = alice.New(wikiAuth).ThenFunc(viewHandler)
	return r
}

func main() {

	flag.Parse()

	// Open and initialize auth database
	auth.Open("./auth.db")
	autherr := auth.AuthDbInit()
	if autherr != nil {
		log.Fatalln(autherr)
	}
	defer auth.Authdb.Close()

	/*
		//Load conf.json
		conf, _ := os.Open("conf.json")
		decoder := json.NewDecoder(conf)
		err := decoder.Decode(&cfg)
		if err != nil {
			fmt.Println("error decoding config:", err)
		}
		//log.Println(cfg)
		//log.Println(cfg.AuthConf)
	*/

	//Check for wikiDir directory + git repo existence
	_, err := os.Stat(cfg.WikiDir)
	if err != nil {
		log.Println(cfg.WikiDir + " does not exist, creating it.")
		os.Mkdir(cfg.WikiDir, 0755)
	}
	_, err = os.Stat(cfg.WikiDir + ".git")
	if err != nil {
		log.Println(cfg.WikiDir + " is not a git repo!")
		if fInit {
			log.Println("-init flag is given. Cloning " + cfg.GitRepo + "into " + cfg.WikiDir + "...")
			gitClone(cfg.GitRepo)
		} else {
			log.Fatalln("Clone/move your existing repo here, change the config, or run with -init to clone a specified remote repo.")
		}
	}

	s := alice.New(handlers.RecoveryHandler(handlers.PrintRecoveryStack(true)), utils.Logger, auth.UserEnvMiddle, auth.XsrfMiddle)

	r := mux.NewRouter().StrictSlash(true)

	/*
		    statsdata := stats.New()

		    //wiki := s.Append(checkWikiGit)
		    //wikiauth := wiki.Append(wikiAuth)

			r := mux.NewRouter().StrictSlash(false)

		    //r := mux.NewRouter()
			//d := r.Host("go.jba.io").Subrouter()
			r.HandleFunc("/", indexHandler).Methods("GET")

		    r.HandleFunc("/tags", tagMapHandler)

			r.HandleFunc("/new", auth.AuthMiddle(newHandler))
			r.HandleFunc("/login", auth.LoginPostHandler).Methods("POST")
			r.HandleFunc("/login", loginPageHandler).Methods("GET")
			r.HandleFunc("/logout", auth.LogoutHandler).Methods("POST")
			r.HandleFunc("/logout", auth.LogoutHandler).Methods("GET")
			r.HandleFunc("/list", listHandler).Methods("GET")

		    a := r.PathPrefix("/auth").Subrouter()
		    a.HandleFunc("/login", auth.LoginPostHandler).Methods("POST")
		    a.HandleFunc("/logout", auth.LogoutHandler).Methods("POST")
			a.HandleFunc("/logout", auth.LogoutHandler).Methods("GET")
		    a.HandleFunc("/signup", auth.SignupPostHandler).Methods("POST")

		    r.HandleFunc("/admin/users", auth.AuthAdminMiddle(adminUserHandler)).Methods("GET")
		    r.HandleFunc("/admin/users", auth.AuthAdminMiddle(auth.AdminUserPostHandler)).Methods("POST")

			r.HandleFunc("/signup", auth.SignupPostHandler).Methods("POST")
			r.HandleFunc("/signup", signupPageHandler).Methods("GET")

			r.HandleFunc("/gitadd", auth.AuthMiddle(gitCheckinPostHandler)).Methods("POST")
			r.HandleFunc("/gitadd", auth.AuthMiddle(gitCheckinHandler)).Methods("GET")

		    r.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		            w.Header().Set("Content-Type", "application/json")
		            stats := statsdata.Data()
		            b, _ := json.Marshal(stats)
		            w.Write(b)
		    })

			r.PathPrefix("/uploads/").Handler(http.StripPrefix("/uploads/", http.FileServer(http.Dir("uploads"))))

		    // wiki functions, should accept alphanumerical, "_", "-", ".", "@"
			r.HandleFunc("/{name:.*}", auth.AuthMiddle(editHandler)).Methods("GET").Queries("a", "edit")
		    r.HandleFunc("/{name:.*}", auth.AuthMiddle(saveHandler)).Methods("POST").Queries("a", "save")

		    r.Handle("/{name:.*}", alice.New(wikiAuth).ThenFunc(historyHandler)).Methods("GET").Queries("a", "history")
		    r.Handle("/{name:.*}", alice.New(wikiAuth).ThenFunc(viewHandler)).Methods("GET")
	*/

	http.HandleFunc("/robots.txt", utils.RobotsHandler)
	http.HandleFunc("/favicon.ico", utils.FaviconHandler)
	http.HandleFunc("/favicon.png", utils.FaviconHandler)
	http.HandleFunc("/assets/", utils.StaticHandler)
	http.Handle("/", s.Then(Router(r)))

	log.Println("Listening on port " + cfg.Port)
	http.ListenAndServe("0.0.0.0:"+cfg.Port, nil)

}
