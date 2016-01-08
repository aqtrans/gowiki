package main

//TODO:
// - Check for and possibly ask to initiate new wikiDir and git repo within if it does not exist
//    - For consistency and convenience, I should use some cfg.wikiGitRepo to pull down the existing wiki data in this case
//    - For safety, so as not to nuke existing changes accidentally, I should check for an -init flag and only clone in this case, otherwise Fatalln
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
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	//"bufio"
	"github.com/golang-commonmark/markdown"
	"github.com/gorilla/mux"
	"github.com/justinas/alice"
	"github.com/oxtoacart/bpool"
	//"gopkg.in/libgit2/git2go.v23"
	"gopkg.in/yaml.v2"
	"html/template"
	"io"
	"io/ioutil"
	"jba.io/go/auth"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const timestamp = "2006-01-02 at 03:04:05PM"

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
	debug     bool
	fInit     bool
	cfg       = configuration{}
	gitPath   string
)

//Base struct, page ; has to be wrapped in a data {} strut for consistency reasons
type page struct {
	SiteName string
	Cats     []string
	UN       string
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
	UN    string
	Title string
}

type historyPage struct {
	*page
	History []string
}

//JSON Response
type jsonresponse struct {
	Name    string `json:"name,omitempty"`
	Success bool   `json:"success"`
}

type jsonfresponse struct {
	Href string `json:"href,omitempty"`
	Name string `json:"name,omitempty"`
}

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
	flag.BoolVar(&debug, "d", false, "Enabled debug logging")
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

	funcMap := template.FuncMap{"prettyDate": PrettyDate, "safeHTML": SafeHTML, "imgClass": ImgClass}

	for _, layout := range layouts {
		files := append(includes, layout)
		//DEBUG TEMPLATE LOADING
		Debugln(files)
		templates[filepath.Base(layout)] = template.Must(template.New("templates").Funcs(funcMap).ParseFiles(files...))
	}

	//var err error
	gitPath, err = exec.LookPath("git")
	if err != nil {
		log.Fatal("git must be installed")
	}

}

func Debugln(v ...interface{}) {
	if debug {
		d := log.New(os.Stdout, "DEBUG: ", log.Ldate)
		d.Println(v)
	}
}

func PrettyDate(date int64) string {
	if date == 0 {
		return "N/A"
	}
	t := time.Unix(date, 0)
	return t.Format(timestamp)
}

func ImgClass(s string) string {
	if strings.HasSuffix(s, ".gif") {
		return "gifs"
	}
	return "imgs"
}

func SafeHTML(s string) template.HTML {
	return template.HTML(s)
}

func timeTrack(start time.Time, name string) {
	elapsed := time.Since(start)
	log.Printf("[timer] %s took %s", name, elapsed)
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
func gitGetLog(filename string) ([]string, error) {
	o, err := gitCommand("log", "--pretty=format:'commit:%H date:%at message:%s'", filename).Output()
	if err != nil {
		return []string{""}, errors.New(fmt.Sprintf("error during `git log`: %s\n%s", err.Error(), string(o)))
	}
	logsplit := strings.Split(string(o), "\n")
	return logsplit, nil
}

// Get file as it existed at specific commit
// git show [commit sha1]:[filename]
func gitGetFileCommit(filename, commit string) ([]byte, error) {
	o, err := gitCommand("show", commit, filename).Output()
	if err != nil {
		return []byte{}, errors.New(fmt.Sprintf("error during `git show`: %s\n%s", err.Error(), string(o)))
	}
	return o, nil
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
	md := markdown.New(markdown.HTML(true), markdown.Nofollow(true), markdown.Breaks(true))
	mds := md.RenderToString(content)
	//log.Println("MDS:"+ mds)
	return mds
}

func loadPage(r *http.Request) (*page, error) {
	//timer.Step("loadpageFunc")
	user := auth.GetUsername(r)
	cats := make(chan []string)
	go catsHandler(cats)
	zcats := <-cats
	//log.Println(zcats)
	return &page{SiteName: "GoWiki", UN: user, Cats: zcats}, nil
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
		history,
	}
	err = renderTemplate(w, "history.tmpl", hp)
	if err != nil {
		log.Fatalln(err)
	}
}

// Need to figure out how I am going to call this, in what order the vars are coming, etc
/*
func commitHandler(w http.ResponseWriter, r *http.Request) {
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
	ctime, err := gitGetCtime(filename)
	if err != nil {
		log.Panicln(err)
	}
	mtime, err := gitGetMtime(commit)
	if err != nil {
		log.Panicln(err)
	}

	hp := &historyPage{
		p,
		history,
	}
	err = renderTemplate(w, "history.tmpl", hp)
	if err != nil {
		log.Fatalln(err)
	}
}
*/

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
			// FIXME: Traverse directory
			log.Println(file + " is a directory......")
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
			log.Println(file)
			body, err := ioutil.ReadFile(file)
			if err != nil {
				log.Fatalln(err)
			}
			// Read YAML frontmatter into fm
			_, err = readFront(body, &fm)
			if err != nil {
				// If YAML frontmatter doesn't exist, proceed, but log it
				//log.Fatalln(err)
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

//Hack to allow me to make full URLs due to absence of http:// from URL.Scheme in dev situations
//When behind Nginx, use X-Forwarded-Proto header to retrieve this, then just tack on "://"
//getScheme(r) should return http:// or https://
func getScheme(r *http.Request) (scheme string) {
	scheme = r.Header.Get("X-Forwarded-Proto") + "://"
	/*
		scheme = "http://"
		if r.TLS != nil {
			scheme = "https://"
		}
	*/
	if scheme == "://" {
		scheme = "http://"
	}
	return scheme
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
	log.Println("Dir:" + dir)
	log.Println("Filename:" + filename)
	fullfilename := "./md/" + name
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
		log.Fatalln(err)
		return nil, err
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

type statusWriter struct {
	http.ResponseWriter
	status int
	size   int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Status() int {
	return w.status
}

func (w *statusWriter) Size() int {
	return w.size
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	written, err := w.ResponseWriter.Write(b)
	w.size += written
	return written, err
}

//Custom Logging Middleware
func logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer

		start := time.Now()
		writer := statusWriter{w, 0, 0}

		buf.WriteString("Started ")
		fmt.Fprintf(&buf, "%s ", r.Method)
		fmt.Fprintf(&buf, "%q ", r.URL.String())
		fmt.Fprintf(&buf, "|Host: %s |RawURL: %s |UserAgent: %s |Scheme: %s |IP: %s ", r.Host, r.Header.Get("X-Raw-URL"), r.Header.Get("User-Agent"), getScheme(r), r.Header.Get("X-Forwarded-For"))
		buf.WriteString("from ")
		buf.WriteString(r.RemoteAddr)

		//Log to file
		f, err := os.OpenFile("./req.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
		if err != nil {
			log.Fatalf("error opening file: %v", err)
		}
		defer f.Close()
		log.SetOutput(io.MultiWriter(os.Stdout, f))
		log.Print(buf.String())
		//Reset buffer to be reused by the end stuff
		buf.Reset()

		next.ServeHTTP(&writer, r)

		end := time.Now()
		latency := end.Sub(start)
		status := writer.Status()

		buf.WriteString("Returning ")
		fmt.Fprintf(&buf, "%v", status)
		buf.WriteString(" in ")
		fmt.Fprintf(&buf, "%s", latency)
		//log.SetOutput(io.MultiWriter(os.Stdout, f))
		log.Print(buf.String())
	})
}

//Generate a random key of specific length
func randKey(leng int8) string {
	dictionary := "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	rb := make([]byte, leng)
	rand.Read(rb)
	for k, v := range rb {
		rb[k] = dictionary[v%byte(len(dictionary))]
	}
	sessID := string(rb)
	return sessID
}

func makeJSON(w http.ResponseWriter, data interface{}) ([]byte, error) {
	jsonData, err := json.MarshalIndent(data, "", "    ")
	if err != nil {
		return nil, err
	}
	Debugln(string(jsonData))
	return jsonData, nil
}

func writeJ(w http.ResponseWriter, name string, success bool) error {
	j := jsonresponse{
		Name:    name,
		Success: success,
	}
	json, err := makeJSON(w, j)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	w.Write(json)
	Debugln(string(json))
	return nil
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
	defer timeTrack(time.Now(), "viewHandler")

	// Get Wiki
	p, err := loadWikiPage(r)
	if err != nil {
		if err.Error() == "No such dir index" {
			log.Println("No such dir index...creating one.")
			http.Redirect(w, r, "/edit/"+p.Filename, 302)
			return
		} else if err.Error() == "No such file" {
			log.Println("No such file...creating one.")
			http.Redirect(w, r, "/edit/"+p.Filename, 302)
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
	log.Println(p.Title + " Page rendered!")
}

func editHandler(w http.ResponseWriter, r *http.Request) {
	defer timeTrack(time.Now(), "editHandler")
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
	defer timeTrack(time.Now(), "saveHandler")
	vars := mux.Vars(r)
	name := vars["name"]
	r.ParseForm()
	//txt := r.Body
	body := r.FormValue("editor")
	//bwiki := txt

	// Check for and install required YAML frontmatter
	title := r.FormValue("title")
	tags := r.FormValue("tags")
	//log.Println("TITLE: "+ title)
	//log.Println("TAGS: "+ tags)
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

	//log.Print(bwiki)

	rp := &rawPage{
		name,
		body,
	}

	err := rp.save()
	if err != nil {
		writeJ(w, "", false)
		log.Fatalln(err)
		return
	}

	writeJ(w, name, true)
	log.Println(name + " page saved!")

}

func newHandler(w http.ResponseWriter, r *http.Request) {
	defer timeTrack(time.Now(), "saveHandler")
	pagetitle := r.FormValue("newwiki")
	//log.Println(pagetitle)
	//log.Println(r)
	http.Redirect(w, r, "/edit/"+pagetitle, 301)

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
	defer timeTrack(time.Now(), "wiki.save()")
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
	//log.Println("Dir is empty")

	log.Println(fullfilename)
	log.Println(wiki.Content)

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
	defer timeTrack(time.Now(), "loginPageHandler")
	title := "login"
	user := auth.GetUsername(r)
	p, err := loadPage(r)
	if err != nil {
		log.Fatalln(err)
	}
	//p, err := loadPage(title, r)
	gp := &genPage{
		p,
		user,
		title,
	}
	err = renderTemplate(w, "login.tmpl", gp)
	if err != nil {
		log.Println(err)
		return
	}
}

func loginPost(w http.ResponseWriter, r *http.Request) {
	auth.LoginPostHandler(cfg.AuthConf, w, r)
}

func main() {
	flag.Parse()
	/* for reference
	p1 := &Page{Title: "TestPage", Body: []byte("This is a sample page.")}
	p1.save()
	p2, _ := loadPage("TestPage")
	fmt.Println(string(p2.Body))
	*/
	//t := time.Now().Unix()
	//tm := time.Unix(t, 0)
	//log.Println(t)
	//log.Println(tm)
	//log.Println(tm.Format(timestamp))

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

	newSess := randKey(32)
	log.Println("Session ID: " + newSess)
	log.Println("Listening on port 3000")

	flag.Set("bind", ":3000")

	std := alice.New(logger)
	//stda := alice.New(Auth, Logger)

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
	r.HandleFunc("/history/{name:.*}", historyHandler).Methods("GET")
	r.HandleFunc("/cats", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, "./md/cats") })
	r.HandleFunc("/save/{name:.*}", saveHandler).Methods("POST")
	r.HandleFunc("/edit/{name:.*}", editHandler)
	//http.Handle("/s/", http.StripPrefix("/s/", http.FileServer(http.Dir("public"))))
	r.PathPrefix("/s/").Handler(http.StripPrefix("/s/", http.FileServer(http.Dir("public"))))
	r.PathPrefix("/uploads/").Handler(http.StripPrefix("/uploads/", http.FileServer(http.Dir("uploads"))))
	//r.HandleFunc("/{name}", viewHandler).Methods("GET")
	r.HandleFunc("/{name:.*}", viewHandler).Methods("GET")
	http.Handle("/", std.Then(r))
	http.ListenAndServe(":3000", nil)
}
