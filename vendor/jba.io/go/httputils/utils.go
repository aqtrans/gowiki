package httputils

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"github.com/GeertJohan/go.rice"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

const timestamp = "2006-01-02 at 03:04:05PM"

type assets struct {
	box *Box
}

type Box struct {
	*rice.Box
}

var (
	Debug     bool
	startTime = time.Now().UTC()
	//AssetsBox *rice.Box
)

func OpenAssetBox(path string) *assets {
	theBox := rice.MustFindBox(path)
	return &assets{box: &Box{theBox}}
}

func OpenRiceBox(path string) *rice.Box {
	return rice.MustFindBox(path)
}

func Debugln(v ...interface{}) {
	if Debug {
		var buf bytes.Buffer
		debuglogger := log.New(&buf, "Debug: ", log.Ltime)
		debuglogger.SetOutput(os.Stderr)
		debuglogger.Print(v)
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
	if strings.HasSuffix(s, ".webm") {
		return "gifs"
	}
	return "imgs"
}

func ImgExt(s string) string {
	if strings.HasSuffix(s, ".gif") {
		return "gif"
	}
	if strings.HasSuffix(s, ".webm") {
		return "webm"
	}
	return ""
}

//SafeHTML is a template function to ensure HTML isn't escaped
func SafeHTML(s string) template.HTML {
	return template.HTML(s)
}

//GetScheme is a hack to allow me to make full URLs due to absence of http:// from URL.Scheme in dev situations
//When behind Nginx, use X-Forwarded-Proto header to retrieve this, then just tack on "://"
//getScheme(r) should return http:// or https://
func GetScheme(r *http.Request) (scheme string) {
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

//TimeTrack is a simple function to time the duration of any function you wish
// Example (at the beginning of a function you wish to time): defer utils.TimeTrack(time.Now(), "[func name]")
func TimeTrack(start time.Time, name string) {
	if Debug {
		elapsed := time.Since(start)
		//log.Printf("[timer] %s took %s", name, elapsed)

		var buf bytes.Buffer
		timerlogger := log.New(&buf, "Timer: ", log.Ltime)
		timerlogger.SetOutput(os.Stderr)
		timerlogger.Printf("[timer] %s took %s", name, elapsed)
	}
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

//Logger is my custom logging middleware
// It prints all HTTP requests to a file called http.log, as well as helps the expvarHandler log the status codes
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		//Log to file
		f, err := os.OpenFile("./http.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
		if err != nil {
			log.Fatalf("error opening file: %v", err)
		}
		defer f.Close()

		start := time.Now()
		writer := statusWriter{w, 0, 0}

		buf.WriteString("Started ")
		fmt.Fprintf(&buf, "%s ", r.Method)
		fmt.Fprintf(&buf, "%q ", r.URL.String())
		fmt.Fprintf(&buf, "|Host: %s |RawURL: %s |UserAgent: %s |Scheme: %s |IP: %s ", r.Host, r.Header.Get("X-Raw-URL"), r.Header.Get("User-Agent"), GetScheme(r), r.Header.Get("X-Forwarded-For"))
		buf.WriteString("from ")
		buf.WriteString(r.RemoteAddr)

		//log.SetOutput(io.MultiWriter(os.Stdout, f))
		toplogger := log.New(&buf, "HTTP: ", log.LstdFlags)
		toplogger.SetOutput(f)
		toplogger.Print(buf.String())
		Debugln(buf.String())

		//Reset buffer to be reused by the end stuff
		buf.Reset()

		next.ServeHTTP(&writer, r)

		end := time.Now()
		latency := end.Sub(start)
		status := writer.Status()

		buf.WriteString("Returning ")
		fmt.Fprintf(&buf, "%v", status)
		buf.WriteString(" for ")
		fmt.Fprintf(&buf, "%q ", r.URL.String())
		buf.WriteString(" in ")
		fmt.Fprintf(&buf, "%s", latency)
		//log.SetOutput(io.MultiWriter(os.Stdout, f))

		bottomlogger := log.New(&buf, "HTTP: ", log.LstdFlags)
		bottomlogger.SetOutput(f)
		bottomlogger.Print(buf.String())
		Debugln(buf.String())

	})
}

func RandBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	// Note that err == nil only if we read len(b) bytes.
	if err != nil {
		return nil, err
	}
	return b, nil
}

//RandKey generates a random key of specific length
func RandKey(n int) (string, error) {
	/*
		dictionary := "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
		rb := make([]byte, leng)
		rand.Read(rb)
		for k, v := range rb {
			rb[k] = dictionary[v%byte(len(dictionary))]
		}
		return string(rb)
	*/
	b, err := RandBytes(n)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil

}

//ServeContent checks for file existence, and if there, serves it so it can be cached
func ServeContent(w http.ResponseWriter, r *http.Request, dir, file string) {
	f, err := http.Dir(dir).Open(file)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	content := io.ReadSeeker(f)
	http.ServeContent(w, r, file, time.Now(), content)
	return
}

// This serves a file of the requested name from the "assets" rice box
func (a *assets) ServeRiceAsset(w http.ResponseWriter, r *http.Request, file string) {
	f, err := a.box.HTTPBox().Open(file)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	content := io.ReadSeeker(f)
	http.ServeContent(w, r, file, time.Now(), content)
	return
}

// Taken from http://reinbach.com/golang-webapps-1.html
func (a *assets) StaticHandler(w http.ResponseWriter, r *http.Request) {
	staticFile := r.URL.Path[len("/assets/"):]

	defer TimeTrack(time.Now(), "StaticHandler "+staticFile)

	//log.Println(staticFile)
	if len(staticFile) != 0 {
		a.ServeRiceAsset(w, r, staticFile)
		return
	}
	http.NotFound(w, r)
	return
}

func (a *assets) FaviconHandler(w http.ResponseWriter, r *http.Request) {
	//log.Println(r.URL.Path)
	if r.URL.Path == "/favicon.ico" {
		a.ServeRiceAsset(w, r, "/favicon.ico")
		return
	} else if r.URL.Path == "/favicon.png" {
		a.ServeRiceAsset(w, r, "/favicon.png")
		return
	} else {
		http.NotFound(w, r)
		return
	}

}

func (a *assets) RobotsHandler(w http.ResponseWriter, r *http.Request) {
	//log.Println(r.URL.Path)
	if r.URL.Path == "/robots.txt" {
		a.ServeRiceAsset(w, r, "/robots.txt")
		return
	}
	http.NotFound(w, r)
	return
}
