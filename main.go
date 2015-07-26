package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/justinas/alice"
	"github.com/oxtoacart/bpool"
	"github.com/opennota/markdown"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const timestamp = "2006-01-02 at 03:04:05PM"

type Configuration struct {
	Port     string
	Username string
	Password string
	Email    string
	WikiDir  string
	MainTLD  string
	LDAPport uint16
	LDAPurl  string
	LDAPdn   string
	LDAPun   string
}

var (
	bufpool   *bpool.BufferPool
	templates map[string]*template.Template
	_24K      int64 = (1 << 20) * 24
	fLocal    bool
	debug 	  bool 
	cfg       = Configuration{}

)

//Base struct, Page ; has to be wrapped in a data {} strut for consistency reasons
type Base struct {
	SiteName    string
}

type Wiki struct {
	Created     int64
	LastModTime int64
	Title       string
	Content     []byte
}

type ListPage struct {
	*Wiki
	Wikis    []*Wiki
}

//JSON Response
type jsonresponse struct {
	Name    string `json:"name,omitempty"`
	Success bool   `json:"success"`
}

type WikiByDate []*Wiki

func (a WikiByDate) Len() int           { return len(a) }
func (a WikiByDate) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a WikiByDate) Less(i, j int) bool { return a[i].Created < a[j].Created }
	
type WikiByModDate []*Wiki

func (a WikiByModDate) Len() int           { return len(a) }
func (a WikiByModDate) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a WikiByModDate) Less(i, j int) bool { return a[i].LastModTime < a[j].LastModTime }
	
	
func init() {
	//Flag '-l' enables go.dev and *.dev domain resolution
	flag.BoolVar(&fLocal, "l", false, "Turn on localhost resolving for Handlers")
	//Flag '-d' enabled debug logging
	flag.BoolVar(&debug, "d", false, "Enabled debug logging")

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
}

func Debugln(v ...interface{}) {
	if debug {
		d := log.New(os.Stdout, "DEBUG: ", log.Ldate)
		d.Println(v)
	}
}	

func PrettyDate(date int64) string {
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

func markdownRender(content []byte) string {
	md := markdown.New(markdown.XHTMLOutput(true), markdown.Nofollow(true))	
	return md.RenderToString(content)
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

func ParseBool(value string) bool {
	boolValue, err := strconv.ParseBool(value)
	if err != nil {
		return false
	}
	return boolValue
}

func loadPage(title string) (*Wiki, error) {
    filename := "./md/" + title + ".md"
    body, err := ioutil.ReadFile(filename)
    if err != nil {
		log.Println(err)
    	return nil, err
    }
    return &Wiki{Title: title, Content: body}, nil
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
func Logger(next http.Handler) http.Handler {
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
func RandKey(leng int8) string {
	dictionary := "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	rb := make([]byte, leng)
	rand.Read(rb)
	for k, v := range rb {
		rb[k] = dictionary[v%byte(len(dictionary))]
	}
	sess_id := string(rb)
	return sess_id
}

func makeJSON(w http.ResponseWriter, data interface{}) ([]byte, error) {
	jsonData, err := json.MarshalIndent(data, "", "    ")
	if err != nil {
		return nil, err
	}
	Debugln(string(jsonData))
	return jsonData, nil
}

func WriteJ(w http.ResponseWriter, name string, success bool) error {
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
	defer timeTrack(time.Now(), "indexHandler")
	name := "index"
	p, err := loadPage(name)
	if err != nil {
		log.Println(err)
		http.NotFound(w, r)
		return
	}

	body, err := ioutil.ReadFile("./md/" + name + ".md")
	if err != nil {
		http.NotFound(w, r)
		log.Println(err)
		return
	}
	//unsafe := blackfriday.MarkdownCommon(body)
	md := markdownRender(body)
	mdhtml := template.HTML(md)
	//html := bluemonday.UGCPolicy().SanitizeBytes(unsafe)

	data := struct {
		Wiki  *Wiki
		Title string
		MD    template.HTML
	}{
		p,
		name,
		mdhtml,
	}
	err = renderTemplate(w, "md.tmpl", data)
	if err != nil {
		log.Println(err)
	}
	log.Println(name + " Page rendered!")
}

func getTitle(r *http.Request) (string, error) {
	vars := mux.Vars(r)
	name := vars["name"]
	//fmt.Print(name)
	return name, nil
}

func viewHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	name := vars["name"]
	p, err := loadPage(name)
	if err != nil {
		log.Println(err)
		http.NotFound(w, r)
		return
	}

	body, err := ioutil.ReadFile("./md/" + name + ".md")
	if err != nil {
		http.NotFound(w, r)
		log.Println(err)
		return
	}
	//unsafe := blackfriday.MarkdownCommon(body)
	md := markdownRender(body)
	mdhtml := template.HTML(md)
	//html := bluemonday.UGCPolicy().SanitizeBytes(unsafe)

	data := struct {
		Wiki  *Wiki
		Title string
		MD    template.HTML
	}{
		p,
		name,
		mdhtml,
	}
	err = renderTemplate(w, "md.tmpl", data)
	if err != nil {
		log.Println(err)
	}
	log.Println(name + " Page rendered!")
}

func editHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	title := vars["name"]
	p, err := loadPage(title)
	if err != nil {
		log.Println(err)
		p = &Wiki{Title: title}
	}
	renderTemplate(w, "edit.tmpl", p)
}

func saveHandler(w http.ResponseWriter, r *http.Request) {
		title, err := getTitle(r)
		body := r.FormValue("body")
		wiki := &Wiki{Created: 123, LastModTime: 123, Title: title, Content: []byte(body)}
		err = wiki.save()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/view/"+title, http.StatusFound)
		log.Println(title + " page saved!")
}

func (wiki *Wiki) save() error {
    filename := wiki.Title + ".md"
    fullfilename := "data/" + filename 
    return ioutil.WriteFile(fullfilename, wiki.Content, 0600)
}

func main() {
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

	//Check for essential directory existence
	_, err = os.Stat(cfg.WikiDir)
	if err != nil {
		os.Mkdir(cfg.WikiDir, 0755)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = cfg.Port
	}

	new_sess := RandKey(32)
	log.Println("Session ID: " + new_sess)

	flag.Parse()
	flag.Set("bind", ":3000")

	std := alice.New(Logger)
	//stda := alice.New(Auth, Logger)

	r := mux.NewRouter().StrictSlash(true)
	//d := r.Host("go.jba.io").Subrouter()
	r.HandleFunc("/", indexHandler).Methods("GET")
	r.HandleFunc("/{name}", viewHandler).Methods("GET")
	r.HandleFunc("/edit/{name}", editHandler)
	r.HandleFunc("/save/{name}", saveHandler)
	r.PathPrefix("/").Handler(http.FileServer(http.Dir("./public/")))
	http.Handle("/", std.Then(r))
	http.ListenAndServe(":3000", nil)
}
