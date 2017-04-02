package main

import (
	"context"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/boltdb/bolt"
	"github.com/dimfeld/httptreemux"
	"github.com/spf13/viper"
	"jba.io/go/auth"
	"jba.io/go/httputils"
)

const UserKey key = 1
const RoleKey key = 2

var (
	server    *httptest.Server
	reader    io.Reader //Ignore this for now
	serverURL string
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

type AuthDB struct {
	*auth.DB
}

// MustOpenDB returns a new, open DB at a temporary location.
func mustOpenDB() *AuthDB {
	tmpdb, err := bolt.Open(tempfile(), 0666, nil)
	if err != nil {
		panic(err)
	}
	return &AuthDB{&auth.DB{tmpdb}}
}

func (tmpdb *AuthDB) Close() error {
	//log.Println(tmpdb.Path())
	defer os.Remove(tmpdb.Path())
	return tmpdb.DB.Close()
}

func (tmpdb *AuthDB) MustClose() {
	if err := tmpdb.Close(); err != nil {
		panic(err)
	}
}

/*
func newState() *auth.AuthState {
	authDB := mustOpenDB()
	authState, err := auth.NewAuthStateWithDB(authDB.DB, tempfile(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	defer authDB.Close()
	return authState
}
*/

func TestAuthInit(t *testing.T) {
	authDB := mustOpenDB()
	authState, err := auth.NewAuthStateWithDB(authDB.DB, tempfile(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	defer authDB.Close()
	_, err = authState.Userlist()
	if err != nil {
		t.Fatal(err)
	}
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
	loadCache()
	err := gitPull()
	if err != nil {
		t.Fatal(err)
	}

	authDB := mustOpenDB()
	authState, err = auth.NewAuthStateWithDB(authDB.DB, tempfile(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	defer authDB.Close()

	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	randPage, err := httputils.RandKey(8)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest("GET", "/"+randPage, nil)
	if err != nil {
		t.Fatal(err)
	}

	handler := http.HandlerFunc(wikiMiddle(viewHandler))

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	rr := httptest.NewRecorder()
	ctx := context.Background()
	ctx = context.WithValue(ctx, auth.UserKey, &auth.User{
		Username: "admin",
		IsAdmin:  true,
	})
	//t.Log(auth.IsLoggedIn(ctx))
	//t.Log(auth.GetUsername(ctx))
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

	var err error
	authDB := mustOpenDB()
	authState, err = auth.NewAuthStateWithDB(authDB.DB, tempfile(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	defer authDB.Close()

	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	req, err := http.NewRequest("GET", "/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	handler := http.HandlerFunc(healthCheckHandler)

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
	err := gitPull()
	if err != nil {
		t.Fatal(err)
	}

	authDB := mustOpenDB()
	authState, err = auth.NewAuthStateWithDB(authDB.DB, tempfile(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	defer authDB.Close()

	err = riceInit()
	if err != nil {
		log.Fatal(err)
	}

	// Create a request to pass to our handler.
	form := url.Values{}
	form.Add("newwiki", "afefwdwdef/dwwafefe/fegegrgr")
	reader = strings.NewReader(form.Encode())
	req, err := http.NewRequest("POST", "/new", reader)
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rr := httptest.NewRecorder()

	ctx := context.Background()
	ctx = context.WithValue(ctx, auth.UserKey, &auth.User{
		Username: "admin",
		IsAdmin:  true,
	})
	rctx := req.WithContext(ctx)

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	handler := http.HandlerFunc(newHandler)

	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.
	handler.ServeHTTP(rr, rctx)

	// Check the status code is what we expect.
	if status := rr.Code; status != http.StatusSeeOther {
		t.Log(rr.Body.String())
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusSeeOther)
	}

	//log.Println(rr.Header().Get("Location"))
	/*
		// Check the response body is what we expect.
		expected := `/edit/omg/yeah/what`
		if rr.Header().Get("Location") != expected {
			t.Errorf("handler returned unexpected body: got %v want %v",
				rr.Header().Get("Location"), expected)
		}
	*/

}

// TestIndex tests if viewing the index page, as a logged in user, properly returns a 200
func TestIndexPage(t *testing.T) {
	err := gitPull()
	if err != nil {
		t.Fatal(err)
	}

	authDB := mustOpenDB()
	authState, err = auth.NewAuthStateWithDB(authDB.DB, tempfile(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	defer authDB.Close()

	err = riceInit()
	if err != nil {
		log.Fatal(err)
	}

	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	req, err := http.NewRequest("GET", "/", nil)
	if err != nil {
		t.Fatal(err)
	}

	handler := http.HandlerFunc(indexHandler)

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	rr := httptest.NewRecorder()
	ctx := context.Background()
	ctx = context.WithValue(ctx, auth.UserKey, &auth.User{
		Username: "admin",
		IsAdmin:  true,
	})
	//params := make(map[string]string)
	//params["name"] = "index"
	//ctx = context.WithValue(ctx, httptreemux.ParamsContextKey, params)
	rctx := req.WithContext(ctx)

	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.
	handler.ServeHTTP(rr, rctx)
	//t.Log(rr.Body.String())
	//t.Log(randPage)
	//t.Log(rr.Code)

	// Check the status code is what we expect.
	if status := rr.Code; status != http.StatusSeeOther {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusSeeOther)
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
	err := gitPull()
	if err != nil {
		t.Fatal(err)
	}

	authDB := mustOpenDB()
	authState, err = auth.NewAuthStateWithDB(authDB.DB, tempfile(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	defer authDB.Close()

	err = riceInit()
	if err != nil {
		log.Fatal(err)
	}

	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	req, err := http.NewRequest("GET", "/history/index", nil)
	if err != nil {
		t.Fatal(err)
	}

	handler := http.HandlerFunc(wikiMiddle(historyHandler))

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	rr := httptest.NewRecorder()
	ctx := context.Background()
	ctx = context.WithValue(ctx, auth.UserKey, &auth.User{
		Username: "admin",
		IsAdmin:  true,
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
	err := gitPull()
	if err != nil {
		t.Fatal(err)
	}

	authDB := mustOpenDB()
	authState, err = auth.NewAuthStateWithDB(authDB.DB, tempfile(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	defer authDB.Close()

	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	req, err := http.NewRequest("GET", "/edit/index", nil)
	if err != nil {
		t.Fatal(err)
	}

	handler := http.HandlerFunc(authState.AuthMiddle(wikiMiddle(editHandler)))

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	rr := httptest.NewRecorder()
	ctx := context.Background()
	ctx = context.WithValue(ctx, auth.UserKey, &auth.User{
		Username: "admin",
		IsAdmin:  true,
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

// TestDirBaseHandler tests if trying to create a file 'inside' a file fails
func TestDirBaseHandler(t *testing.T) {
	err := gitPull()
	if err != nil {
		t.Fatal(err)
	}
	//setup()
	// Create a request to pass to our handler.
	form := url.Values{}
	form.Add("newwiki", "index/what/omg")
	//log.Println(form)
	reader = strings.NewReader(form.Encode())
	req, err := http.NewRequest("POST", "/new", reader)
	if err != nil {
		t.Fatal(err)
	}

	authDB := mustOpenDB()
	authState, err = auth.NewAuthStateWithDB(authDB.DB, tempfile(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	defer authDB.Close()

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rr := httptest.NewRecorder()

	ctx := context.Background()
	ctx = context.WithValue(ctx, auth.UserKey, &auth.User{
		Username: "admin",
		IsAdmin:  true,
	})
	rctx := req.WithContext(ctx)

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	handler := http.HandlerFunc(newHandler)

	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.
	handler.ServeHTTP(rr, rctx)

	//log.Println(rr.Header())
	//log.Println(rr.Body)

	// Check the status code is what we expect.
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
	/*
		//log.Println(rr.Header().Get("Location"))

		// Check the response body is what we expect.

			expected := `/edit/omg/yeah/what`
			if rr.Header().Get("Location") != expected {
				t.Errorf("handler returned unexpected body: got %v want %v",
					rr.Header().Get("Location"), expected)
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
	_, rawmd := readFileAndFront(rawmdf)

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

func TestYamlRender(t *testing.T) {
	f, err := os.Open("./tests/yamltest")
	if err != nil {
		t.Error(err)
	}
	defer f.Close()
	fm := readFront(f)

	/*
		t.Log(fm.Title)
		t.Log(fm.Admin)
		t.Log(fm.Public)
		t.Log(fm.Favorite)
		t.Log(fm.Tags)
		t.Log(c)
		t.Log(len(fm.Tags))
	*/

	if fm.Title != "YAML Test" {
		t.Error("FM Title does not equal YAML Test." + "\n Output: " + fm.Title)
	}
	if fm.Admin {
		t.Log(fm.Admin)
		t.Error("FM Admin is not false.")
	}
	if !fm.Public {
		t.Log(fm.Public)
		t.Error("FM Public is not true.")
	}
	if fm.Favorite {
		t.Log(fm.Favorite)
		t.Error("FM Admin is not false.")
	}
	tags := []string{"yaml", "test", "omg"}
	if fm.Tags[0] != tags[0] {
		t.Error("FM Tags do not equal expected tags.")
	}
	if fm.Tags[1] != tags[1] {
		t.Error("FM Tags do not equal expected tags.")
	}
	if fm.Tags[2] != tags[2] {
		t.Error("FM Tags do not equal expected tags.")
	}

}

func TestYamlRender2(t *testing.T) {
	f, err := os.Open("./tests/yamltest2")
	if err != nil {
		t.Error(err)
	}
	defer f.Close()
	fm := readFront(f)
	/*
		t.Log(fm.Title)
		t.Log(fm.Admin)
		t.Log(fm.Public)
		t.Log(fm.Favorite)
		t.Log(fm.Tags)
		t.Log(c)
		t.Log(len(fm.Tags))
	*/

	if fm.Title != "YAML Test" {
		t.Error("FM Title does not equal YAML Test." + "\n Output: " + fm.Title)
	}
	if fm.Admin {
		t.Log(fm.Admin)
		t.Error("FM Admin is not false.")
	}
	if !fm.Public {
		t.Log(fm.Public)
		t.Error("FM Public is not true.")
	}
	if fm.Favorite {
		t.Log(fm.Favorite)
		t.Error("FM Admin is not false.")
	}
	tags := []string{"yaml", "test", "omg"}
	if fm.Tags[0] != tags[0] {
		t.Error("FM Tags do not equal expected tags.")
	}
	if fm.Tags[1] != tags[1] {
		t.Error("FM Tags do not equal expected tags.")
	}
	if fm.Tags[2] != tags[2] {
		t.Error("FM Tags do not equal expected tags.")
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

func BenchmarkReadFront(b *testing.B) {
	f, err := os.Open("./tests/gowiki-testdata/index")
	if err != nil {
		log.Println(err)
	}
	defer f.Close()

	for n := 0; n < b.N; n++ {
		readWikiPage(f)
	}
}

func BenchmarkWholeWiki(b *testing.B) {
	req, err := http.NewRequest("GET", "/", nil)
	if err != nil {
		b.Fatal(err)
	}

	handler := http.HandlerFunc(indexHandler)

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	rr := httptest.NewRecorder()
	ctx := context.Background()
	ctx = context.WithValue(ctx, auth.UserKey, &auth.User{
		Username: "admin",
		IsAdmin:  true,
	})
	//params := make(map[string]string)
	//params["name"] = "index"
	//ctx = context.WithValue(ctx, httptreemux.ParamsContextKey, params)
	rctx := req.WithContext(ctx)

	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.

	for n := 0; n < b.N; n++ {
		handler.ServeHTTP(rr, rctx)
	}
}
