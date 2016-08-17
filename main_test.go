package main

import (
	"bytes"
	"io"
	"io/ioutil"
	"testing"
	//"github.com/drewolson/testflight"
	"net/http"
	"net/http/httptest"
	//"html/template"
	//"github.com/gorilla/context"
	//"fmt"
	//"log"
	"net/url"
	"os"
	//"jba.io/go/auth"
	"strings"
	"context"
	//"github.com/gorilla/mux"
	"jba.io/go/auth"
	"github.com/dimfeld/httptreemux"
	//"jba.io/go/utils"
	"github.com/boltdb/bolt"
	"github.com/spf13/viper"
	//"github.com/rhinoman/go-commonmark"
	//"github.com/GeertJohan/go.rice"
	//"gopkg.in/gavv/httpexpect.v1"
	//"github.com/stretchr/testify/assert"
)

const UserKey key = 1
const RoleKey key = 2

var (
	server    *httptest.Server
	reader    io.Reader //Ignore this for now
	serverUrl string
	//m         *mux.Router
	//req       *http.Request
	//rr        *httptest.ResponseRecorder
)

func init() {
	viper.Set("WikiDir", "./tests/gowiki-testdata")
	viper.Set("Domain", "wiki.example.com")
	viper.Set("GitRepo", "git@jba.io:conf/gowiki-data.git")
}

// tempfile returns a temporary file path.
func tempfile() string {
	f, err := ioutil.TempFile("", "bolt-")
	if err != nil {
		panic(err)
	}
	if err := f.Close(); err != nil {
		panic(err)
	}
	if err := os.Remove(f.Name()); err != nil {
		panic(err)
	}
	return f.Name()
}

type DB struct {
	*bolt.DB
}

// MustOpenDB returns a new, open DB at a temporary location.
func mustOpenDB() *DB {
	db, err := bolt.Open(tempfile(), 0666, nil)
	if err != nil {
		panic(err)
	}
	return &DB{db}
}

func (db *DB) Close() error {
	defer os.Remove(db.Path())
	return db.DB.Close()
}

func (db *DB) MustClose() {
	if err := db.Close(); err != nil {
		panic(err)
	}
}



func TestAuthInit(t *testing.T) {
	//authDB := mustOpenDB()
	db := mustOpenDB()
	t.Log(db.Path())
	auth.Authdb = db.DB
	autherr := auth.AuthDbInit()
	if autherr != nil {
		t.Fatal(autherr)
	}
	db.MustClose()
}

func TestRiceInit(t *testing.T) {
	err := riceInit()
	if err != nil {
		t.Fatal(err)
	}	
}

func TestWikiInit(t *testing.T) {
	_, err := os.Stat(viper.GetString("WikiDir"))
	if err != nil {
		os.Mkdir(viper.GetString("WikiDir"), 0755)
	}
	_, err = os.Stat(viper.GetString("WikiDir") + ".git")
	if err != nil {
		gitClone(viper.GetString("GitRepo"))
	}
}

// TestNewWikiPage tests if viewing a non-existent article, as a logged in user, properly redirects to /edit/page_name with a 404
func TestNewWikiPage(t *testing.T) {
    // Create a request to pass to our handler. We don't have any query parameters for now, so we'll
    // pass 'nil' as the third parameter.
	randPage := auth.RandKey(8)
    req, err := http.NewRequest("GET", "/"+randPage, nil)
    if err != nil {
        t.Fatal(err)
    }

	handler := http.HandlerFunc(wikiHandler(viewHandler))

    // We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
    rr := httptest.NewRecorder()
	ctx := context.Background()
	ctx = context.WithValue(ctx, auth.UserKey, &auth.User{
		Username: "admin",
		IsAdmin: true,
	})
	params := make(map[string]string)
	params["name"] = randPage
	ctx = context.WithValue(ctx, httptreemux.ParamsContextKey, params)
	rctx := req.WithContext(ctx)

    // Our handlers satisfy http.Handler, so we can call their ServeHTTP method 
    // directly and pass in our Request and ResponseRecorder.
    handler.ServeHTTP(rr, rctx)
	//t.Log(rr.Body.String())
	//t.Log(randPage)
	//t.Log(rr.Code)
	
    // Check the status code is what we expect.
    if status := rr.Code; status != http.StatusNotFound {
        t.Errorf("handler returned wrong status code: got %v want %v",
            status, http.StatusNotFound)
    }

	/*
    // Check the response body is what we expect.
    expected := `{"alive": true}`
    if rr.Body.String() != expected {
        t.Errorf("handler returned unexpected body: got %v want %v",
            rr.Body.String(), expected)
    }
	*/
}

func TestHealthCheckHandler(t *testing.T) {
	//assert := assert.New(t)
	//setup()
	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	req, err := http.NewRequest("GET", "/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	handler := http.HandlerFunc(HealthCheckHandler)

	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.
	handler.ServeHTTP(rr, req)

	// Check the status code is what we expect.
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}
	//assert.Equal(rr.Code, http.StatusOK, "HealthCheckHandler not returning 200")

	// Check the response body is what we expect.
	expected := `{"alive": true}`
	if rr.Body.String() != expected {
		t.Errorf("handler returned unexpected body: got %v want %v",
			rr.Body.String(), expected)
	}
}

func TestNewHandler(t *testing.T) {
	//setup()
	// Create a request to pass to our handler.
	form := url.Values{}
	form.Add("newwiki", "omg/yeah/what")
	//log.Println(form)
	reader = strings.NewReader(form.Encode())
	req, err := http.NewRequest("POST", "/new", reader)
	if err != nil {
		t.Fatal(err)
	}
	//log.Println(reader)

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rr := httptest.NewRecorder()

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	handler := http.HandlerFunc(newHandler)

	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.
	handler.ServeHTTP(rr, req)

	//log.Println(rr.Header())

	// Check the status code is what we expect.
	if status := rr.Code; status != http.StatusTemporaryRedirect {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusCreated)
	}

	//log.Println(rr.Header().Get("Location"))

	// Check the response body is what we expect.
	expected := `/edit/omg/yeah/what`
	if rr.Header().Get("Location") != expected {
		t.Errorf("handler returned unexpected body: got %v want %v",
			rr.Header().Get("Location"), expected)
	}

}

// TestIndex tests if viewing the index page, as a logged in user, properly returns a 200
func TestIndexPage(t *testing.T) {
    // Create a request to pass to our handler. We don't have any query parameters for now, so we'll
    // pass 'nil' as the third parameter.
    req, err := http.NewRequest("GET", "/", nil)
    if err != nil {
        t.Fatal(err)
    }

	handler := http.HandlerFunc(wikiHandler(viewHandler))

    // We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
    rr := httptest.NewRecorder()
	ctx := context.Background()
	ctx = context.WithValue(ctx, auth.UserKey, &auth.User{
		Username: "admin",
		IsAdmin: true,
	})
	params := make(map[string]string)
	params["name"] = "index"
	ctx = context.WithValue(ctx, httptreemux.ParamsContextKey, params)
	rctx := req.WithContext(ctx)

    // Our handlers satisfy http.Handler, so we can call their ServeHTTP method 
    // directly and pass in our Request and ResponseRecorder.
    handler.ServeHTTP(rr, rctx)
	//t.Log(rr.Body.String())
	//t.Log(randPage)
	//t.Log(rr.Code)
	
    // Check the status code is what we expect.
    if status := rr.Code; status != http.StatusOK {
        t.Errorf("handler returned wrong status code: got %v want %v",
            status, http.StatusOK)
    }

	/*
    // Check the response body is what we expect.
    expected := `{"alive": true}`
    if rr.Body.String() != expected {
        t.Errorf("handler returned unexpected body: got %v want %v",
            rr.Body.String(), expected)
    }
	*/
}

// TestIndexHistoryPage tests if viewing the history of the index page, as a logged in user, properly returns a 200
func TestIndexHistoryPage(t *testing.T) {
    // Create a request to pass to our handler. We don't have any query parameters for now, so we'll
    // pass 'nil' as the third parameter.
    req, err := http.NewRequest("GET", "/history/index", nil)
    if err != nil {
        t.Fatal(err)
    }

	handler := http.HandlerFunc(wikiHandler(historyHandler))

    // We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
    rr := httptest.NewRecorder()
	ctx := context.Background()
	ctx = context.WithValue(ctx, auth.UserKey, &auth.User{
		Username: "admin",
		IsAdmin: true,
	})
	params := make(map[string]string)
	params["name"] = "index"
	ctx = context.WithValue(ctx, httptreemux.ParamsContextKey, params)
	rctx := req.WithContext(ctx)

    // Our handlers satisfy http.Handler, so we can call their ServeHTTP method 
    // directly and pass in our Request and ResponseRecorder.
    handler.ServeHTTP(rr, rctx)
	//t.Log(rr.Body.String())
	//t.Log(randPage)
	//t.Log(rr.Code)
	
    // Check the status code is what we expect.
    if status := rr.Code; status != http.StatusOK {
        t.Errorf("handler returned wrong status code: got %v want %v",
            status, http.StatusOK)
    }

	/*
    // Check the response body is what we expect.
    expected := `{"alive": true}`
    if rr.Body.String() != expected {
        t.Errorf("handler returned unexpected body: got %v want %v",
            rr.Body.String(), expected)
    }
	*/
}

// TestIndexEditPage tests if trying to edit the index page, as a logged in user, properly returns a 200
func TestIndexEditPage(t *testing.T) {
    // Create a request to pass to our handler. We don't have any query parameters for now, so we'll
    // pass 'nil' as the third parameter.
    req, err := http.NewRequest("GET", "/edit/index", nil)
    if err != nil {
        t.Fatal(err)
    }

	handler := http.HandlerFunc(auth.AuthMiddle(wikiHandler(editHandler)))

    // We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
    rr := httptest.NewRecorder()
	ctx := context.Background()
	ctx = context.WithValue(ctx, auth.UserKey, &auth.User{
		Username: "admin",
		IsAdmin: true,
	})
	params := make(map[string]string)
	params["name"] = "index"
	ctx = context.WithValue(ctx, httptreemux.ParamsContextKey, params)
	rctx := req.WithContext(ctx)

    // Our handlers satisfy http.Handler, so we can call their ServeHTTP method 
    // directly and pass in our Request and ResponseRecorder.
    handler.ServeHTTP(rr, rctx)
	//t.Log(rr.Body.String())
	//t.Log(randPage)
	//t.Log(rr.Code)
	
    // Check the status code is what we expect.
    if status := rr.Code; status != http.StatusOK {
        t.Errorf("handler returned wrong status code: got %v want %v",
            status, http.StatusOK)
    }

	/*
    // Check the response body is what we expect.
    expected := `{"alive": true}`
    if rr.Body.String() != expected {
        t.Errorf("handler returned unexpected body: got %v want %v",
            rr.Body.String(), expected)
    }
	*/
}

func TestMarkdownRender(t *testing.T) {
	// Read raw Markdown
	rawmdf := "./tests/test.md"
	rawmd, err := ioutil.ReadFile(rawmdf)
	if err != nil {
		t.Error("Unable to access test.md")
	}
	// Read what rendered Markdown HTML should look like
	rendermdf := "./tests/test.html"
	rendermd, err := ioutil.ReadFile(rendermdf)
	if err != nil {
		t.Error("Unable to access test.html")
	}
	// []byte to string
	rendermds := string(rendermd)

	rawmds := markdownRender(rawmd)
	//rawmds := commonmarkRender(rawmd)
	
	if rawmds != rendermds {
		//ioutil.WriteFile("./tests/test.html", []byte(rawmds), 0755)
		t.Error("Converted Markdown does not equal test" + "\n Output: \n" + rawmds + "Expected: \n" + rendermds)
	}

}

// Tests a corner case where stuff without markdown wasn't being rendered
func TestMarkdownRender2(t *testing.T) {
	// Read raw Markdown
	rawmdf := "./tests/test2.md"
	rawmd, err := ioutil.ReadFile(rawmdf)
	if err != nil {
		t.Error("Unable to access test2.md")
	}
	// Read what rendered Markdown HTML should look like
	rendermdf := "./tests/test2.html"
	rendermd, err := ioutil.ReadFile(rendermdf)
	if err != nil {
		t.Error("Unable to access test2.html")
	}
	// []byte to string
	rendermds := string(rendermd)

	rawmds := markdownRender(rawmd)
	//rawmds := commonmarkRender(rawmd)
	if rawmds != rendermds {
		//ioutil.WriteFile("./tests/test2.html", []byte(rawmds), 0755)
		t.Error("Converted Markdown does not equal test2" + "\n Output: \n" + rawmds + "Expected: \n" + rendermds)
	}
}


// Tests my custom link renderer, without YAML frontmatter
func TestMarkdownRender3(t *testing.T) {
	rawmdf := "./tests/test3.md"
	rawmd, err := ioutil.ReadFile(rawmdf)
	if err != nil {
		t.Error("Unable to access test3.md")
	}

	// Read what rendered Markdown HTML should look like
	rendermdf := "./tests/test3.html"
	rendermd, err := ioutil.ReadFile(rendermdf)
	if err != nil {
		t.Error("Unable to access test3.html")
	}
	// []byte to string
	rendermds := string(rendermd)

	rawmds := markdownRender(rawmd)

	if rawmds != rendermds {
		//ioutil.WriteFile("./tests/test3.html", []byte(rawmds), 0755)
		t.Error("Converted Markdown does not equal test3" + "\n Output: \n" + rawmds + "Expected: \n" + rendermds)
	}
	
}

// Tests my custom link renderer, with YAML frontmatter
func TestMarkdownRender4(t *testing.T) {
	rawmdf := "./tests/test4.md"
	_, rawmd, err := readFileAndFront(rawmdf)
	if err != nil {
		t.Error("Unable to access test4.md")
	}
	
	// Read what rendered Markdown HTML should look like
	rendermdf := "./tests/test4.html"
	rendermd, err := ioutil.ReadFile(rendermdf)
	if err != nil {
		t.Error("Unable to access test4.html")
	}
	// []byte to string
	rendermds := string(rendermd)

	rawmds := markdownRender(rawmd)
	//rawmds := commonmarkRender(rawmd)

	if rawmds != rendermds {
		//ioutil.WriteFile("./tests/test4.html", []byte(rawmds), 0755)
		t.Error("Converted Markdown does not equal test4" + "\n Output: \n" + rawmds + "Expected: \n" + rendermds)
	}	
}

// Below is for testing the difference between just writing the Tags string directly as fed in from the wiki form, or using a []string as the source, but having to write them using a for loop
// Results, seems string is the best bet for now:
//    BenchmarkBufferString-4    10000            996527 ns/op
//    BenchmarkBufferArray-4      1000           1651610 ns/op

var title string = "YEAH BENCHMARK OMG"
var name string = "WOOHOO"
var tagsarray = []string{"OMG", "YEAH", "WHAT", "ZZZZ", "FFFF", "EEEE", "RRRTRT", "GRHTH", "GBHFT", "QPFLG", "MGJHIB", "LRIGJB", "DJCUDK", "WIFJV", "GKBIBK", "XKSDFM", "RUFJS", "SLDKF", "ZKDJF", "WIFKFG", "EIFLG", "DKFIBJ", "WWRKG", "SLFIBK", "PRIVATE"}
var tagsstring string = "OMG, YEAH, WHAT, ZZZZ, FFFF, EEEE, RRRTRT, GRHTH, GBHFT, QPFLG, MGJHIB, LRIGJB, DJCUDK, WIFJV, GKBIBK, XKSDFM, RUFJS, SLDKF ,ZKDJF, WIFKFG, EIFLG, DKFIBJ, WWRKG, SLFIBK, PRIVATE"
var body string = "WOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOO\n# OMG \n # YEAH"

func benchmarkBufferString(b *testing.B) {
	for n := 0; n < b.N; n++ {
		var buffer bytes.Buffer
		buffer.WriteString("---\n")
		if title == "" {
			title = name
		}
		buffer.WriteString("title: " + title)
		buffer.WriteString("\n")
		if tagsstring != "" {
			buffer.WriteString("tags: " + tagsstring)
			buffer.WriteString("\n")
		}
		buffer.WriteString("---\n")
		buffer.WriteString(body)
		body = buffer.String()
	}
}

func benchmarkBufferArray(b *testing.B) {
	for n := 0; n < b.N; n++ {
		var buffer bytes.Buffer
		buffer.WriteString("---\n")
		if title == "" {
			title = name
		}
		buffer.WriteString("title: " + title)
		buffer.WriteString("\n")
		if tagsarray != nil {
			buffer.WriteString("tags: [")
			for _, v := range tagsarray {
				buffer.WriteString(v + " ")
			}
			buffer.WriteString("]")
			buffer.WriteString("\n")
		}
		buffer.WriteString("---\n")
		buffer.WriteString(body)
		body = buffer.String()
	}
}

// Below is for testing the difference between sorting through a []string and creating a []string using SplitString, then sorting through it

func benchmarkIsPrivate(size int, b *testing.B) {
	list := "testing"
	n := size
	for i := 0; i < n; i++ {
		list = " " + list
	}
	//tags := strings.Split(list, " ")
	for n := 0; n < b.N; n++ {
		isPrivate(list)
	}
}

func benchmarkIsPrivateArray(size int, b *testing.B) {
	list := []string{"testing"}
	n := size
	for i := 0; i < n; i++ {
		list = append(list, "testing")
	}
	//tags := strings.Split(list, " ")
	for n := 0; n < b.N; n++ {
		isPrivateA(list)
	}
}

func BenchmarkReadFront(b *testing.B) {
	for n := 0; n < b.N; n++ {
		readFileAndFront("./tests/bench.md")
	}
}

func BenchmarkReadFrontBuffer(b *testing.B) {
	for n := 0; n < b.N; n++ {
		readFrontBuf("./tests/bench.md")
	}
}

func BenchmarkMarkdownRender(b *testing.B) {
	for n := 0; n < b.N; n++ {
		rawmdf := "./tests/bench.md"
		rawmd, err := ioutil.ReadFile(rawmdf)
		if err != nil {
			b.Error("Unable to access bench.md")
		}
		markdownRender(rawmd)
	}
}

/*
func BenchmarkCommonmarkRender(b *testing.B) {
	for n := 0; n < b.N; n++ {
		rawmdf := "./tests/bench.md"
		rawmd, err := ioutil.ReadFile(rawmdf)
		if err != nil {
			b.Error("Unable to access bench.md")
		}
		commonmarkRender(rawmd)
	}
}

func BenchmarkMarkdown2Render(b *testing.B) {
	for n := 0; n < b.N; n++ {
		rawmdf := "./tests/bench.md"
		rawmd, err := ioutil.ReadFile(rawmdf)
		if err != nil {
			b.Error("Unable to access bench.md")
		}
		markdownRender2(rawmd)
	}
}
*/
//func BenchmarkIsPrivate2(b *testing.B) { benchmarkIsPrivate(2, b) }
//func BenchmarkIsPrivateArray2(b *testing.B) { benchmarkIsPrivateArray(2, b) }

//func BenchmarkIsPrivate10(b *testing.B) { benchmarkIsPrivate(10, b) }
//func BenchmarkIsPrivateArray10(b *testing.B) { benchmarkIsPrivateArray(10, b) }

//func BenchmarkIsPrivate100(b *testing.B) { benchmarkIsPrivate(100, b) }
//func BenchmarkIsPrivateArray100(b *testing.B) { benchmarkIsPrivateArray(100, b) }

//func BenchmarkIsPrivate1000(b *testing.B) { benchmarkIsPrivate(1000, b) }
//func BenchmarkIsPrivateArray1000(b *testing.B) { benchmarkIsPrivateArray(1000, b) }

func BenchmarkIsPrivate10000(b *testing.B)      { benchmarkIsPrivate(10000, b) }
func BenchmarkIsPrivateArray10000(b *testing.B) { benchmarkIsPrivateArray(10000, b) }

//func BenchmarkIsPrivate100000(b *testing.B) { benchmarkIsPrivate(100000, b) }
//func BenchmarkIsPrivateArray100000(b *testing.B) { benchmarkIsPrivateArray(100000, b) }
