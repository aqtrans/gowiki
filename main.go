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

// - GUI for Tags - taggle.js should do this for me
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
	//"bufio"
	//"github.com/golang-commonmark/markdown"
    "github.com/russross/blackfriday"
	"github.com/gorilla/mux"
    "github.com/gorilla/handlers"
    //"github.com/rs/xhandler"
    //"github.com/gorilla/context"
	"github.com/justinas/alice"
    //"github.com/cyclopsci/apollo"
	"github.com/oxtoacart/bpool"
    "github.com/thoas/stats"
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
	"gopkg.in/yaml.v2"
	"jba.io/go/auth"
    "jba.io/go/utils"
    //"golang.org/x/net/context"
)

const (
	EXTENSION_NO_INTRA_EMPHASIS          = 1 << iota // ignore emphasis markers inside words
	EXTENSION_TABLES                                 // render tables
	EXTENSION_FENCED_CODE                            // render fenced code blocks
	EXTENSION_AUTOLINK                               // detect embedded URLs that are not explicitly marked
	EXTENSION_STRIKETHROUGH                          // strikethrough text using ~~test~~
	EXTENSION_LAX_HTML_BLOCKS                        // loosen up HTML block parsing rules
	EXTENSION_SPACE_HEADERS                          // be strict about prefix header rules
	EXTENSION_HARD_LINE_BREAK                        // translate newlines into line breaks
	EXTENSION_TAB_SIZE_EIGHT                         // expand tabs to eight spaces instead of four
	EXTENSION_FOOTNOTES                              // Pandoc-style footnotes
	EXTENSION_NO_EMPTY_LINE_BEFORE_BLOCK             // No need to insert an empty line to start a (code, quote, ordered list, unordered list) block
	EXTENSION_HEADER_IDS                             // specify header IDs  with {#id}
	EXTENSION_TITLEBLOCK                             // Titleblock ala pandoc
	EXTENSION_AUTO_HEADER_IDS                        // Create the header ID from the text
	EXTENSION_BACKSLASH_LINE_BREAK                   // translate trailing backslashes into line breaks
	EXTENSION_DEFINITION_LISTS                       // render definition lists
        
    commonHtmlFlags = 0 |
		blackfriday.HTML_USE_XHTML |
		blackfriday.HTML_USE_SMARTYPANTS |
		blackfriday.HTML_SMARTYPANTS_FRACTIONS |
		blackfriday.HTML_SMARTYPANTS_DASHES |
		blackfriday.HTML_SMARTYPANTS_LATEX_DASHES
        
    commonExtensions = 0 |
		EXTENSION_NO_INTRA_EMPHASIS |
		EXTENSION_TABLES |
		EXTENSION_FENCED_CODE |
		EXTENSION_AUTOLINK |
		EXTENSION_STRIKETHROUGH |
		EXTENSION_SPACE_HEADERS |
		EXTENSION_HEADER_IDS |
		EXTENSION_BACKSLASH_LINE_BREAK |
		EXTENSION_DEFINITION_LISTS |
        EXTENSION_NO_EMPTY_LINE_BEFORE_BLOCK |
        EXTENSION_FOOTNOTES  
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
    //sessID   string
)

//Base struct, page ; has to be wrapped in a data {} strut for consistency reasons
type page struct {
	SiteName string
	Cats     []string
	UN       string
    Token    string
}

type frontmatter struct {
	Title string `yaml:"title"`
	Tags  string `yaml:"tags,omitempty"`
	//	Created     int64    `yaml:"created,omitempty"`
	//	LastModTime int64	 `yaml:"lastmodtime,omitempty"`
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
	IsPrivate  bool
	CreateTime int64
	ModTime    int64
}

type commitPage struct {
	*page
	PageTitle string
	Filename  string
	*frontmatter
	*wiki
	IsPrivate  bool
	CreateTime int64
	ModTime    int64
    Diff       string
    Commit     string
}

type rawPage struct {
	Name    string
	Content string
}

type listPage struct {
	*page
	Wikis []*wikiPage
}

type genPage struct {
	*page
	Title string
}

type historyPage struct {
	*page
    Filename    string
	FileHistory []*commitLog
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

	funcMap := template.FuncMap{"prettyDate": utils.PrettyDate, "safeHTML": utils.SafeHTML, "imgClass": utils.ImgClass}

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
func gitIsClean() error {
	c := gitCommand("status", "-s")

	o, err := c.Output()
	if err != nil {
		return err
	}

	if len(o) != 0 {
		return errors.New("directory is dirty")
	}

	return nil
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
        // vs[0] = commit, vs[1] = date, vs[2] = message
        theCommit := &commitLog {
            Filename: filename,
            Commit: vs[0],
            Date: mtime,
            Message: vs[2],
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
	//user := auth.GetUsername(r)
    //token := auth.SetToken(w, r)
    //log.Println(token)

    // Auth lib middlewares should load the user and tokens into context for reading
    user := auth.GetUsername(r)
    token := auth.GetToken(r)
    /*
    user, ok := context.GetOk(r, auth.UserKey)
    if !ok {
        log.Println("No username in context.")
        user = ""
    }
    token, ok := context.GetOk(r, auth.TokenKey)
    if !ok {
        log.Println("No token in context.")
        token = ""
    }*/
    //fmt.Print("Token: ")
    //log.Println(token)
    //fmt.Print("User: ")
    //log.Println(user)
    
    // Grab list of cats from channel
	cats := make(chan []string)
	go catsHandler(cats)
	zcats := <-cats
    
	//log.Println(zcats)
	return &page{SiteName: "GoWiki", Cats: zcats, UN: user, Token: token}, nil
}

func historyHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	name := vars["name"]
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
	err = renderTemplate(w, "history.tmpl", hp)
	if err != nil {
		log.Fatalln(err)
	}
}

// Need to get content of the file at specified commit
// > git show [commit sha1]:[filename]
// As well as the date
// > git log -1 --format=%at [commit sha1]
// TODO: need to find a way to detect sha1s
func viewCommitHandler(w http.ResponseWriter, r *http.Request) {
	var fm frontmatter
	var priv bool
	var pagetitle string    
	vars := mux.Vars(r)
	name := vars["name"]
	commit := vars["commit"]

	p, err := loadPage(r)
	if err != nil {
		log.Fatalln(err)
	}
	body, err := gitGetFileCommit(name, commit)
	if err != nil {
		log.Fatalln(err)
	}
	ctime, err := gitGetCtime(name)
	if err != nil {
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
	content, err := readFront(body, &fm)
	if err != nil {
		log.Println(err)
	}
	if content == nil {
		content = body
	}
	// Render remaining content after frontmatter
	md := markdownRender(content)
	if isPrivate(fm.Tags) {
		log.Println("Private page!")
		priv = true
	} else {
		priv = false
	}
	if fm.Title != "" {
		pagetitle = fm.Title
	} else {
		pagetitle = name
	}
    diffstring := strings.Replace(string(diff),"\n","<br>",-1)
    //log.Println(diffstring)

	cp := &commitPage{
		p,
		pagetitle,
		name,
		&fm,
		&wiki{
			Rendered: md,
			Content:  string(content),
		},
		priv,
		ctime,
		mtime,
        diffstring,
        commit,
	}
    
	err = renderTemplate(w, "md-commit.tmpl", cp)
	if err != nil {
		log.Fatalln(err)
	}
}


func listHandler(w http.ResponseWriter, r *http.Request) {
	searchDir := "./md/"
	p, err := loadPage(r)
	if err != nil {
		log.Fatalln(err)
	}

	fileList := []string{}
	_ = filepath.Walk(searchDir, func(path string, f os.FileInfo, err error) error {
		// check and skip .git
		if path == "md/.git" {
			return filepath.SkipDir
		}
		fileList = append(fileList, path)
		return nil
	})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(200)
	var wps []*wikiPage
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
			var wp *wikiPage
			var fm *frontmatter
			var priv bool
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
			_, err = readFront(body, &fm)
			if err != nil {
				// If YAML frontmatter doesn't exist, proceed, but log it
				//log.Fatalln(err)
                log.Println("YAML unmarshal error in: " + filename)
				log.Println(err)
			}
			if fm != nil {
				//log.Println(fm.Tags)
				//log.Println(fm)

				if isPrivate(fm.Tags) {
					//log.Println("Private page!")
					priv = true
				} else {
					priv = false
				}
				if fm.Title != "" {
					pagetitle = fm.Title
				}
			} else {
				// If file doesn't have frontmatter, add in crap
				log.Println(file + " doesn't have frontmatter :( ")
				fm = &frontmatter{
					file,
					"",
				}
			}
			ctime, err := gitGetCtime(filename)
			if err != nil {
				log.Panicln(err)
			}
			mtime, err := gitGetMtime(filename)
			if err != nil {
				log.Panicln(err)
			}

			wp = &wikiPage{
				p,
				pagetitle,
				filename,
				fm,
				&wiki{},
				priv,
				ctime,
				mtime,
			}
			wps = append(wps, wp)
			//log.Println(string(body))
			//log.Println(string(wp.wiki.Content))
		}

	}
	l := &listPage{p, wps}
	err = renderTemplate(w, "list.tmpl", l)
	if err != nil {
		log.Fatalln(err)
	}
}

func readFront(data []byte, frontmatter interface{}) (content []byte, err error) {
	r := bytes.NewBuffer(data)

	// eat away starting whitespace
	var ch rune = ' '
	for unicode.IsSpace(ch) {
		ch, _, err = r.ReadRune()
		if err != nil {
			// file is just whitespace
			return []byte{}, nil
		}
	}
	r.UnreadRune()

	// check if first line is ---
	line, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		return nil, err
	}

	if strings.TrimSpace(line) != "---" {
		// no front matter, just content
		return data, nil
	}

	yamlStart := len(data) - r.Len()
	yamlEnd := yamlStart

	for {
		line, err = r.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return data, nil
			}
			return nil, err
		}

		if strings.TrimSpace(line) == "---" {
			yamlEnd = len(data) - r.Len()
			break
		}
	}

	err = yaml.Unmarshal(data[yamlStart:yamlEnd], frontmatter)
	if err != nil {
		return nil, err
	}
	content = data[yamlEnd:]
	err = nil
	return
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
	IsPrivate    bool
}
type Wiki struct {
	Rendered     string
    Content      string
}*/
/////////////////////////////
func loadWikiPage(r *http.Request) (*wikiPage, error) {
	var fm frontmatter
	var priv bool
	var pagetitle string
	var body []byte
	vars := mux.Vars(r)
	name := vars["name"]
	p, err := loadPage(r)
	if err != nil {
		log.Fatalln(err)
	}
	dir, filename := filepath.Split(name)
	//log.Println("Dir:" + dir)
	//log.Println("Filename:" + filename)
	fullfilename := "./md/" + name
    base := filepath.Dir(fullfilename)
	// Directory without specified index
	if dir != "" && filename == "" {
		log.Println("This is a directory, trying to parse the index")
		fullfilename = "./md/" + name + "index"
		filename = name + "index"
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
			false,
			0,
			0,
		}
		return newwp, errn
	}
	_, fierr := os.Stat(fullfilename)
    if fierr != nil {
       //log.Println(fierr)
    }
    
    // Check if the base of the given filename is actually a file
    // If so, bail, return 500.
    basefile, _ := os.Open("./"+base)
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
    
	if os.IsNotExist(fierr) {
		// NOW: Using os.Stat to properly check for file existence, using IsNotExist()
		// This should mean file is non-existent, so create new page
		log.Println(fierr)
		errn := errors.New("No such file")
		newwp := &wikiPage{
			p,
			filename,
			name,
			&frontmatter{
				Title: filename,
			},
			&wiki{},
			false,
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
	content, err := readFront(body, &fm)
	if err != nil {
		log.Println(err)
		//return nil, err
	}
	if content == nil {
		content = body
	}
	// Render remaining content after frontmatter
	md := markdownRender(content)
	if isPrivate(fm.Tags) {
		log.Println("Private page!")
		priv = true
	} else {
		priv = false
	}
	if fm.Title != "" {
		pagetitle = fm.Title
	} else if dir != "" {
		pagetitle = dir + " - " + filename
	} else {
		pagetitle = filename
	}
	ctime, err := gitGetCtime(filename)
	if err != nil {
		log.Panicln(err)
	}
	mtime, err := gitGetMtime(filename)
	if err != nil {
		log.Panicln(err)
	}
	wp := &wikiPage{
		p,
		pagetitle,
		filename,
		&fm,
		&wiki{
			Rendered: md,
			Content:  string(content),
		},
		priv,
		ctime,
		mtime,
	}
	return wp, nil
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	p, err := loadPage(r)
	if err != nil {
		log.Fatalln(err)
	}
	//log.Println(p)
	ctime, err := gitGetCtime("index")
	if err != nil {
		log.Panicln(err)
	}
	mtime, err := gitGetMtime("index")
	if err != nil {
		log.Panicln(err)
	}
	wp := &wikiPage{
		p,
		"Index",
		"index",
		&frontmatter{},
		&wiki{},
		false,
		ctime,
		mtime,
	}

	err = renderTemplate(w, "index.tmpl", wp)
	if err != nil {
		log.Fatalln(err)
	}

	/*
			defer timeTrack(time.Now(), "indexHandler")
			var fm frontmatter
			var priv bool
			var pagetitle string
			filename := "index.md"
		    fullfilename := "./" + filename
		    body, err := ioutil.ReadFile(fullfilename)
		    if err != nil {
				log.Fatalln(err)
		    }
			// Read YAML frontmatter into fm
			content, err := readFront(body, &fm)
			if err != nil {
				log.Fatalln(err)
			}
			p, err := loadPage(r)
			if err != nil {
				log.Fatalln(err)
			}
			// Render remaining content after frontmatter
			md := markdownRender(content)
			//log.Println(md)
			if fm.Title != "" {
				pagetitle = fm.Title
			} else {
				pagetitle = filename
			}
			wp := &wikiPage{
				p,
				pagetitle,
				filename,
				&fm,
				&wiki{
		            Rendered: md,
					Content: string(content),
				},
				priv,
			}
			// FIXME: Fetch create date, frontmatter, etc
			err = renderTemplate(w, "md.tmpl", wp)
			if err != nil {
				log.Fatalln(err)
			}
			//log.Println("Index rendered!")
	*/
}

func viewHandler(w http.ResponseWriter, r *http.Request) {
	defer utils.TimeTrack(time.Now(), "viewHandler")

	// Get Wiki
	p, err := loadWikiPage(r)
	if err != nil {
		if err.Error() == "No such dir index" {
			log.Println("No such dir index...creating one.")
			http.Redirect(w, r, p.Filename+"/edit", 302)
			return
		} else if err.Error() == "No such file" {
			log.Println("No such file...creating one.")
			http.Redirect(w, r, p.Filename+"/edit", 302)
			return
        } else if err.Error() == "Base is not dir" {
            log.Println("Cannot create subdir of a file.")
            http.Error(w, "Cannot create subdir of a file.", 500)
            return
		} else {
			log.Fatalln(err)
		}
		//log.Println(err.Error())
		http.NotFound(w, r)
		return
	}
	err = renderTemplate(w, "md.tmpl", p)
	if err != nil {
		log.Fatalln(err)
	}
	//log.Println(p.Title + " Page rendered!")
}

func editHandler(w http.ResponseWriter, r *http.Request) {
	defer utils.TimeTrack(time.Now(), "editHandler")
	p, err := loadWikiPage(r)
	//log.Println(p.Filename)
	//log.Println(p.PageTitle)
	if err != nil {
		if err.Error() == "No such file" {
			//log.Println("No such file...creating one.")
			terr := renderTemplate(w, "edit.tmpl", p)
			if terr != nil {
				log.Fatalln(terr)
			}
		} else {
			log.Fatalln(err)
		}
	} else {
		terr := renderTemplate(w, "edit.tmpl", p)
		if terr != nil {
			log.Fatalln(terr)
		}
	}
}

func saveHandler(w http.ResponseWriter, r *http.Request) {
	defer utils.TimeTrack(time.Now(), "saveHandler")
	vars := mux.Vars(r)
	name := vars["name"]
	r.ParseForm()
	//txt := r.Body
	body := r.FormValue("editor")
	//bwiki := txt

	// Check for and install required YAML frontmatter
	title := r.FormValue("title")
	tags := r.FormValue("tags")
    
	// If title or tags aren't empty, declare a buffer
	//  And load the YAML+body into it, then override body
	if title != "" || tags != "" {
		var buffer bytes.Buffer
		buffer.WriteString("---\n")
		if title == "" {
			title = name
		}
		buffer.WriteString("title: " + title)
		buffer.WriteString("\n")
		if tags != "" {
			buffer.WriteString("tags: " + tags)
			buffer.WriteString("\n")
		}
		buffer.WriteString("---\n")
		buffer.WriteString(body)
		body = buffer.String()
	}

	rp := &rawPage{
		name,
		body,
	}

	err := rp.save()
	if err != nil {
		utils.WriteJ(w, "", false)
		log.Fatalln(err)
		return
	}
    
    utils.WriteJ(w, name, true)
	log.Println(name + " page saved!")


}

func newHandler(w http.ResponseWriter, r *http.Request) {
	defer utils.TimeTrack(time.Now(), "saveHandler")
	pagetitle := r.FormValue("newwiki")
	//log.Println(pagetitle)
	//log.Println(r)
	http.Redirect(w, r, pagetitle+"/edit", 301)

}

func catsHandler(cats chan []string) {
	rawcats, err := ioutil.ReadFile("./md/cats")
	if err != nil {
		log.Fatalln(err)
	}
	scats := strings.Split(strings.TrimSpace(string(rawcats)), ",")
	//log.Printf("%s", strings.TrimSpace(string(rawcats)))
	//log.Println(scats)
	cats <- scats
}

func (wiki *rawPage) save() error {
	defer utils.TimeTrack(time.Now(), "wiki.save()")
	//filename := wiki.Name
	dir, filename := filepath.Split(wiki.Name)
	//log.Println("Dir: " + dir)
	//log.Println("Filename: " + filename)
	fullfilename := "./md/" + dir + filename

	// Check for and install required YAML frontmatter
	//if strings.Contains(wiki.Content, "---") {
	//}

	// If directory doesn't exist, create it
	// - Check if dir is null first
	if dir != "" {
		//log.Println("Dir is not empty")
		dirpath := "./md/" + dir
		if _, err := os.Stat(dirpath); os.IsNotExist(err) {
			err := os.MkdirAll(dirpath, 0755)
			if err != nil {
				return err
			}
		}
	}

	//log.Println(fullfilename)
	//log.Println(wiki.Content)

	ioutil.WriteFile(fullfilename, []byte(wiki.Content), 0755)

	gitfilename := dir + filename

	// Now using libgit2
	/*
		// Filename relative to git dir, now ./md/
		gitfilename := dir + filename

		signature := &git.Signature{
			Name: "golang git wiki",
			Email: "server@jba.io",
			When: time.Now(),
		}

		repo, err := git.OpenRepository("./md")
		if err != nil {
			return err
		}
		defer repo.Free()

		index, err := repo.Index()
		if err != nil {
			return err
		}
		defer index.Free()

		err = index.AddByPath(gitfilename)
		if err != nil {
			return err
		}

		treeID, err := index.WriteTree()
		if err != nil {
			return err
		}

		err = index.Write()
		if err != nil {
			return err
		}

		tree, err := repo.LookupTree(treeID)
		if err != nil {
			return err
		}

		message := "Wiki commit. Filename: " + fullfilename

		currentBranch, err := repo.Head()
		if err == nil && currentBranch != nil {
			currentTip, err2 := repo.LookupCommit(currentBranch.Target())
			if err2 != nil {
				return err2
			}
			_, err = repo.CreateCommit("HEAD", signature, signature, message, tree, currentTip)
		} else {
			_, err = repo.CreateCommit("HEAD", signature, signature, message, tree)
		}

		if err != nil {
			return err
		}
	*/

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

// Following functions unnecessary until I implement file uploading myself
// This was for woofmark file uploading
/*
func WriteFJ(w http.ResponseWriter, name string, success bool) error {
	j := jsonfresponse{
		Href: "./uploads/"+name,
		Name:    name,
	}
	json, err := makeJSON(w, j)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if !success {
		w.WriteHeader(400)
		w.Write(json)
		return nil
	}
	w.WriteHeader(200)
	w.Write(json)
	Debugln(string(json))
	return nil
}

func uploadFile(w http.ResponseWriter, r *http.Request) {
	//vars := mux.Vars(r)
	//name := vars["name"]
	//contentLength := r.ContentLength
	//var reader io.Reader
	var f io.WriteCloser
	//var err error
	var filename string
	//var cli bool
	//var remote bool
	//var uptype string
	//fi := &File{}
	path := "./uploads/"
	contentType := r.Header.Get("Content-Type")

	log.Println("Content-type is "+contentType)
	err := r.ParseMultipartForm(_24K)
	if err != nil {
		log.Println("ParseMultiform reader error")
		log.Println(err)
		WriteFJ(w, "", false)
		return
	}
	file, handler, err := r.FormFile("woofmark_upload")
	filename = handler.Filename
	defer file.Close()
	if err != nil {
		fmt.Println(err)
		WriteFJ(w, "", false)
	}

	f, err = os.OpenFile(filepath.Join(path, filename), os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		fmt.Println(err)
		WriteFJ(w, "", false)
		return
	}
	defer f.Close()
	io.Copy(f, file)

	WriteFJ(w, filename, true)

}
*/

func loginPageHandler(w http.ResponseWriter, r *http.Request) {
	defer utils.TimeTrack(time.Now(), "loginPageHandler")
	title := "login"
	p, err := loadPage(r)
	if err != nil {
		log.Fatalln(err)
	}
	//p, err := loadPage(title, r)
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

// Simple wrapper in order to pass auth conf to my auth lib
func loginPost(w http.ResponseWriter, r *http.Request) {
	auth.LoginPostHandler(cfg.AuthConf, w, r)
}

func main() {

	flag.Parse()

	//Load conf.json
	conf, _ := os.Open("conf.json")
	decoder := json.NewDecoder(conf)
	err := decoder.Decode(&cfg)
	if err != nil {
		fmt.Println("error decoding config:", err)
	}
	//log.Println(cfg)
	//log.Println(cfg.AuthConf)

	//Check for wikiDir directory + git repo existence
	_, err = os.Stat(cfg.WikiDir)
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

	port := os.Getenv("PORT")
	if port == "" {
		port = cfg.Port
	}
	//sessID = utils.RandKey(32)
	//log.Println("Session ID: " + sessID)
	log.Println("Listening on port 3000")

	flag.Set("bind", ":3000")
    
    statsdata := stats.New()

	//std := alice.New(utils.Logger)
	//stda := alice.New(Auth, Logger)
    s := alice.New(handlers.RecoveryHandler(), auth.UserEnvMiddle, auth.XsrfMiddle)
    
	r := mux.NewRouter().StrictSlash(false)
	//d := r.Host("go.jba.io").Subrouter()
	r.HandleFunc("/", indexHandler).Methods("GET")
	r.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "") })
	//r.HandleFunc("/up/{name}", uploadFile).Methods("POST", "PUT")
	//r.HandleFunc("/up", uploadFile).Methods("POST", "PUT")
	r.HandleFunc("/new", newHandler)
	r.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) { auth.LoginPostHandler(cfg.AuthConf, w, r) }).Methods("POST")
	r.HandleFunc("/login", loginPageHandler).Methods("GET")
	r.HandleFunc("/logout", auth.LogoutHandler).Methods("POST")
	r.HandleFunc("/logout", auth.LogoutHandler).Methods("GET")
	r.HandleFunc("/list", listHandler).Methods("GET")
    
    r.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
            w.Header().Set("Content-Type", "application/json")
            stats := statsdata.Data()
            b, _ := json.Marshal(stats)
            w.Write(b)
    })

	r.HandleFunc("/cats", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, "./md/cats") })

	//http.Handle("/s/", http.StripPrefix("/s/", http.FileServer(http.Dir("public"))))
	//r.PathPrefix("/s/").Handler(http.StripPrefix("/s/", http.FileServer(http.Dir("public"))))
	r.PathPrefix("/uploads/").Handler(http.StripPrefix("/uploads/", http.FileServer(http.Dir("uploads"))))
	//r.HandleFunc("/{name}", viewHandler).Methods("GET")
	//r.HandleFunc("/save/{name:.*}", saveHandler).Methods("POST")
	//r.HandleFunc("/edit/{name:.*}", editHandler)
	//r.HandleFunc("/history/{name:.*}", historyHandler).Methods("GET") 

    // wiki functions, should accept alphanumerical, "_", "-", "."
	r.HandleFunc("/{name:[A-Za-z0-9_/.-]+}/edit", editHandler).Methods("GET")
    r.HandleFunc("/{name:[A-Za-z0-9_/.-]+}/save", saveHandler).Methods("POST")
    r.HandleFunc("/{name:[A-Za-z0-9_/.-]+}/history", auth.AuthMiddle(historyHandler)).Methods("GET")
    r.HandleFunc("/{name:[A-Za-z0-9_/.-]+}/{commit:[a-f0-9]{40}}", viewCommitHandler).Methods("GET")    
    r.HandleFunc("/{name:[A-Za-z0-9_/.-]+}", viewHandler).Methods("GET")
    
    assets := http.StripPrefix("/s/", http.FileServer(http.Dir("public/")))
    http.Handle("/", s.Then(r))
    http.Handle("/s/", assets)
	http.ListenAndServe(":3000", nil)
}
