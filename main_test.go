package main

import (
	"context"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	//"github.com/boltdb/bolt"
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
	dataDir = "./tests/data/"
	viper.Set("DataDir", "./tests/data/")
	viper.Set("Domain", "wiki.example.com")
	viper.Set("RemoteGitRepo", "git@jba.io:aqtrans/gowiki-testdata.git")
	viper.Set("InitWikiRepo", true)
	httputils.Debug = true
	//log.Println(viper.GetString("DataDir"), dataDir)
}

func checkT(err error, t *testing.T) {
	if err != nil {
		t.Errorf("ERROR: %v", err)
	}
}

func testGitCommand(args ...string) *exec.Cmd {
	c := exec.Command(gitPath, args...)
	//c.Dir = viper.GetString("WikiDir")
	return c
}

// Execute `git clone [repo]` in the current workingDirectory
func gitCloneTest() error {
	err := os.RemoveAll("./tests/gowiki-testdata/")
	if err != nil {
		return err
	}
	o, err := testGitCommand("clone", viper.GetString("RemoteGitRepo"), "./tests/gowiki-testdata/").CombinedOutput()
	if err != nil {
		return fmt.Errorf("error during `git clone`: %s\n%s", err.Error(), string(o))
	}
	return nil
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

func testEnv(t *testing.T, authState *auth.State) *wikiEnv {
	return &wikiEnv{
		authState: authState,
		cache: &wikiCache{
			Tags: newTagsMap(),
			Favs: newFavsMap(),
		},
		templates: make(map[string]*template.Template),
		mutex:     &sync.Mutex{},
	}
}

func TestAuthInit(t *testing.T) {
	tmpdb := tempfile()
	defer os.Remove(tmpdb)
	authState, err := auth.NewAuthState(tmpdb, "admin")
	checkT(err, t)
	_, err = authState.Userlist()
	checkT(err, t)
}

func TestTmplInit(t *testing.T) {
	tmpdb := tempfile()
	defer os.Remove(tmpdb)
	authState, err := auth.NewAuthState(tmpdb, "admin")
	checkT(err, t)
	e := testEnv(t, authState)
	err = tmplInit(e)
	checkT(err, t)
}

func TestWikiInit(t *testing.T) {
	wikiDir := filepath.Join(dataDir, "wikidata")
	_, err := os.Stat(wikiDir)
	if err != nil {
		os.Mkdir(wikiDir, 0755)
	}
	_, err = os.Stat(filepath.Join(wikiDir, ".git"))
	if err != nil {
		err = gitClone(viper.GetString("RemoteGitRepo"))
		if err != nil {
			log.Println(err)
		}
	}
	initWikiDir()
}

// TestNewWikiPage tests if viewing a non-existent article, as a logged in user, properly redirects to /edit/page_name with a 404
func TestNewWikiPage(t *testing.T) {
	err := gitCloneTest()
	checkT(err, t)
	tmpdb := tempfile()
	defer os.Remove(tmpdb)
	authState, err := auth.NewAuthState(tmpdb, "admin")
	checkT(err, t)

	e := testEnv(t, authState)
	err = tmplInit(e)
	checkT(err, t)

	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	randPage, err := httputils.RandKey(8)
	checkT(err, t)
	r, err := http.NewRequest("GET", "/"+randPage, nil)
	checkT(err, t)

	router := httptreemux.NewContextMux()
	router.GET(`/*name`, e.wikiMiddle(e.viewHandler))

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	w := httptest.NewRecorder()
	ctx := context.Background()
	ctx = context.WithValue(ctx, auth.UserKey, &auth.User{
		Username: "admin",
		IsAdmin:  true,
	})
	//t.Log(auth.IsLoggedIn(ctx))
	//t.Log(auth.GetUsername(ctx))

	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.
	//handler.ServeHTTP(rr, rctx)
	router.DefaultContext = ctx
	router.ServeHTTP(w, r)
	//t.Log(w.Body.String())
	//t.Log(randPage)
	//t.Log(w.HeaderMap)

	// Check the status code is what we expect.
	if status := w.Code; status != http.StatusNotFound {
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
	checkT(err, t)
	rr := httptest.NewRecorder()

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	router := httptreemux.NewContextMux()
	router.GET("/health", healthCheckHandler)

	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.
	router.ServeHTTP(rr, req)

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
	checkT(err, t)

	tmpdb := tempfile()
	defer os.Remove(tmpdb)
	authState, err := auth.NewAuthState(tmpdb, "admin")
	checkT(err, t)

	e := testEnv(t, authState)

	err = tmplInit(e)
	checkT(err, t)

	// Create a request to pass to our handler.
	form := url.Values{}
	form.Add("newwiki", "afefwdwdef/dwwafefe/fegegrgr")
	reader = strings.NewReader(form.Encode())
	req, err := http.NewRequest("GET", "/new", reader)
	checkT(err, t)

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rr := httptest.NewRecorder()

	ctx := context.Background()
	ctx = context.WithValue(ctx, auth.UserKey, &auth.User{
		Username: "admin",
		IsAdmin:  true,
	})
	rctx := req.WithContext(ctx)

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	router := httptreemux.NewContextMux()
	router.GET("/new", e.authState.AuthMiddle(newHandler))

	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.
	router.DefaultContext = ctx
	router.ServeHTTP(rr, rctx)

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
	checkT(err, t)

	tmpdb := tempfile()
	defer os.Remove(tmpdb)
	authState, err := auth.NewAuthState(tmpdb, "admin")
	checkT(err, t)

	e := testEnv(t, authState)

	err = tmplInit(e)
	checkT(err, t)

	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	req, err := http.NewRequest("GET", "/", nil)
	checkT(err, t)

	router := httptreemux.NewContextMux()
	router.GET("/", indexHandler)

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
	router.DefaultContext = ctx
	router.ServeHTTP(rr, rctx)
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
	checkT(err, t)

	tmpdb := tempfile()
	defer os.Remove(tmpdb)
	authState, err := auth.NewAuthState(tmpdb, "admin")
	checkT(err, t)

	e := testEnv(t, authState)

	err = tmplInit(e)
	checkT(err, t)

	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	req, err := http.NewRequest("GET", "/history/index", nil)
	checkT(err, t)

	router := httptreemux.NewContextMux()
	router.GET(`/history/*name`, e.authState.AuthMiddle(e.wikiMiddle(e.historyHandler)))

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	rr := httptest.NewRecorder()
	ctx := context.Background()
	ctx = context.WithValue(ctx, auth.UserKey, &auth.User{
		Username: "admin",
		IsAdmin:  true,
	})
	/*
		params := make(map[string]string)
		params["name"] = "index"
		ctx = context.WithValue(ctx, httptreemux.ParamsContextKey, params)
	*/
	rctx := req.WithContext(ctx)

	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.
	router.DefaultContext = ctx
	router.ServeHTTP(rr, rctx)
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
	checkT(err, t)

	tmpdb := tempfile()
	defer os.Remove(tmpdb)
	authState, err := auth.NewAuthState(tmpdb, "admin")
	checkT(err, t)

	e := testEnv(t, authState)
	err = tmplInit(e)
	checkT(err, t)
	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	req, err := http.NewRequest("GET", "/edit/index", nil)
	checkT(err, t)

	router := httptreemux.NewContextMux()
	router.GET(`/edit/*name`, e.authState.AuthMiddle(e.wikiMiddle(e.editHandler)))

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	rr := httptest.NewRecorder()
	ctx := context.Background()
	ctx = context.WithValue(ctx, auth.UserKey, &auth.User{
		Username: "admin",
		IsAdmin:  true,
	})
	/*
		params := make(map[string]string)
		params["name"] = "index"
		ctx = context.WithValue(ctx, httptreemux.ParamsContextKey, params)
	*/
	rctx := req.WithContext(ctx)

	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.
	router.DefaultContext = ctx
	router.ServeHTTP(rr, rctx)
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
	checkT(err, t)
	//setup()
	// Create a request to pass to our handler.
	req, err := http.NewRequest("GET", "/new?newwiki=index/what/omg", nil)
	checkT(err, t)

	tmpdb := tempfile()
	defer os.Remove(tmpdb)
	authState, err := auth.NewAuthState(tmpdb, "admin")
	checkT(err, t)

	testEnv(t, authState)

	e := testEnv(t, authState)
	err = tmplInit(e)
	checkT(err, t)

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rr := httptest.NewRecorder()

	ctx := context.Background()
	ctx = context.WithValue(ctx, auth.UserKey, &auth.User{
		Username: "admin",
		IsAdmin:  true,
	})
	rctx := req.WithContext(ctx)

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	router := httptreemux.NewContextMux()
	router.GET("/new", e.authState.AuthMiddle(newHandler))

	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.
	router.DefaultContext = ctx
	router.ServeHTTP(rr, rctx)

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

func TestRecentsPage(t *testing.T) {
	err := gitPull()
	checkT(err, t)

	tmpdb := tempfile()
	defer os.Remove(tmpdb)
	authState, err := auth.NewAuthState(tmpdb, "admin")
	checkT(err, t)

	e := testEnv(t, authState)

	err = tmplInit(e)
	checkT(err, t)

	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	req, err := http.NewRequest("GET", "/recent", nil)
	checkT(err, t)

	router := httptreemux.NewContextMux()
	router.GET("/recent", e.authState.AuthMiddle(e.recentHandler))

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	rr := httptest.NewRecorder()
	ctx := context.Background()
	ctx = context.WithValue(ctx, auth.UserKey, &auth.User{
		Username: "admin",
		IsAdmin:  true,
	})
	rctx := req.WithContext(ctx)

	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.
	router.DefaultContext = ctx
	router.ServeHTTP(rr, rctx)

	// Check the status code is what we expect.
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusSeeOther)
	}
}

func TestListPage(t *testing.T) {
	err := gitPull()
	checkT(err, t)

	tmpdb := tempfile()
	defer os.Remove(tmpdb)
	authState, err := auth.NewAuthState(tmpdb, "admin")
	checkT(err, t)

	e := testEnv(t, authState)
	e.cache = buildCache()
	err = os.Remove("./tests/data/cache.gob")
	if err != nil {
		t.Error(err)
	}

	err = tmplInit(e)
	checkT(err, t)

	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	req, err := http.NewRequest("GET", "/list", nil)
	checkT(err, t)

	router := httptreemux.NewContextMux()
	router.GET("/list", e.listHandler)

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	rr := httptest.NewRecorder()
	ctx := context.Background()
	ctx = context.WithValue(ctx, auth.UserKey, &auth.User{
		Username: "admin",
		IsAdmin:  true,
	})
	rctx := req.WithContext(ctx)

	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.
	router.DefaultContext = ctx
	router.ServeHTTP(rr, rctx)

	// Check the status code is what we expect.
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusSeeOther)
	}
}

func TestPrivatePageNotLoggedIn(t *testing.T) {
	err := gitPull()
	checkT(err, t)

	tmpdb := tempfile()
	defer os.Remove(tmpdb)
	authState, err := auth.NewAuthState(tmpdb, "admin")
	checkT(err, t)

	e := testEnv(t, authState)

	err = tmplInit(e)
	checkT(err, t)

	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	req, err := http.NewRequest("GET", "/sites.page", nil)
	checkT(err, t)

	router := httptreemux.NewContextMux()
	router.GET(`/*name`, e.wikiMiddle(e.viewHandler))

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	rr := httptest.NewRecorder()
	ctx := context.Background()
	ctx = context.WithValue(ctx, wikiNameKey, "sites.page")
	ctx = context.WithValue(ctx, wikiExistsKey, true)
	rctx := req.WithContext(ctx)

	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.
	router.DefaultContext = ctx
	router.ServeHTTP(rr, rctx)
	//t.Log(rr.Body.String())

	// Check the status code is what we expect.
	if status := rr.Code; status != http.StatusSeeOther {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusSeeOther)
	}

	expected := `/login?url=/sites.page`
	if rr.Header().Get("Location") != expected {
		t.Errorf("handler returned unexpected body: got %v want %v",
			rr.Header().Get("Location"), expected)
	}
}

func TestPrivatePageLoggedIn(t *testing.T) {
	err := gitPull()
	checkT(err, t)

	tmpdb := tempfile()
	defer os.Remove(tmpdb)
	authState, err := auth.NewAuthState(tmpdb, "admin")
	checkT(err, t)

	e := testEnv(t, authState)

	err = tmplInit(e)
	checkT(err, t)

	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	req, err := http.NewRequest("GET", "/sites.page", nil)
	checkT(err, t)

	router := httptreemux.NewContextMux()
	router.GET(`/*name`, e.wikiMiddle(e.viewHandler))

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	rr := httptest.NewRecorder()
	ctx := context.Background()
	ctx = context.WithValue(ctx, auth.UserKey, &auth.User{
		Username: "admin",
		IsAdmin:  true,
	})
	ctx = context.WithValue(ctx, wikiNameKey, "sites.page")
	ctx = context.WithValue(ctx, wikiExistsKey, true)
	/*
		params := make(map[string]string)
		params["name"] = "sites.page"
		ctx = context.WithValue(ctx, httptreemux.ParamsContextKey, params)
	*/
	rctx := req.WithContext(ctx)

	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.
	router.DefaultContext = ctx
	router.ServeHTTP(rr, rctx)
	//t.Log(rr.Body.String())

	// Check the status code is what we expect.
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}
}

func TestSearchPage(t *testing.T) {
	err := gitPull()
	checkT(err, t)

	tmpdb := tempfile()
	defer os.Remove(tmpdb)
	authState, err := auth.NewAuthState(tmpdb, "admin")
	checkT(err, t)

	e := testEnv(t, authState)

	err = tmplInit(e)
	checkT(err, t)

	e.cache = buildCache()
	if e.cache == nil {
		t.Error("cache is empty")
	}

	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	req, err := http.NewRequest("GET", "/search/omg", nil)
	checkT(err, t)

	router := httptreemux.NewContextMux()
	router.GET("/search/*name", e.search)

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	rr := httptest.NewRecorder()
	ctx := context.Background()
	ctx = context.WithValue(ctx, auth.UserKey, &auth.User{
		Username: "admin",
		IsAdmin:  true,
	})
	/*
		params := make(map[string]string)
		params["name"] = "omg"
		ctx = context.WithValue(ctx, httptreemux.ParamsContextKey, params)
	*/
	rctx := req.WithContext(ctx)

	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.
	router.DefaultContext = ctx
	router.ServeHTTP(rr, rctx)

	// Check the status code is what we expect.
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusSeeOther)
	}
}

func TestCache(t *testing.T) {
	cache := buildCache()
	if cache == nil {
		t.Error("cache is empty")
	}
	cache2 := loadCache()
	if cache2 == nil {
		t.Error("cache2 is empty")
	}
	err := os.Remove("./tests/data/cache.gob")
	if err != nil {
		t.Error(err)
	}
}

func TestMarkdownRender(t *testing.T) {
	// Read raw Markdown
	rawmdf := "./tests/test.md"
	rawmd, err := ioutil.ReadFile(rawmdf)
	checkT(err, t)
	// Read what rendered Markdown HTML should look like
	rendermdf := "./tests/test.html"
	rendermd, err := ioutil.ReadFile(rendermdf)
	checkT(err, t)
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
	checkT(err, t)
	// Read what rendered Markdown HTML should look like
	rendermdf := "./tests/test2.html"
	rendermd, err := ioutil.ReadFile(rendermdf)
	checkT(err, t)
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
	checkT(err, t)

	// Read what rendered Markdown HTML should look like
	rendermdf := "./tests/test3.html"
	rendermd, err := ioutil.ReadFile(rendermdf)
	checkT(err, t)
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
	checkT(err, t)
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
	checkT(err, t)
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
	if fm.Permission == "admin" {
		t.Log(fm.Permission)
		t.Error("FM Permission set to admin.")
	}
	if fm.Permission == "private" {
		t.Log(fm.Permission)
		t.Error("FM Permission set to private.")
	}
	if fm.Permission == "public" {
		t.Log(fm.Permission)
		t.Error("FM Permission set to public.")
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
	checkT(err, t)
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
	if fm.Permission == "admin" {
		t.Log(fm.Permission)
		t.Error("FM Permission set to admin.")
	}
	if fm.Permission == "private" {
		t.Log(fm.Permission)
		t.Error("FM Permission set to private.")
	}
	if fm.Permission == "public" {
		t.Log(fm.Permission)
		t.Error("FM Permission set to public.")
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

	router := httptreemux.NewContextMux()
	router.GET("/", indexHandler)

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
	router.DefaultContext = ctx

	for n := 0; n < b.N; n++ {
		router.ServeHTTP(rr, rctx)
	}
}

func BenchmarkGitCtime(b *testing.B) {
	for i := 0; i < b.N; i++ {
		gitGetCtime("index")
	}
}

func BenchmarkGitMtime(b *testing.B) {
	for i := 0; i < b.N; i++ {
		gitGetMtime("index")
	}
}

/*
func TestMultipleWrites(t *testing.T) {
	err := gitCloneTest()
	checkT(err, t)
	var mutex = &sync.Mutex{}

	var wg sync.WaitGroup
	wg.Add(50)

	for w := 0; w < 50; w++ {
		go func() {
			for {
				name := "index"
				randContent, err := httputils.RandKey(32)
				checkT(err, t)
				content := randContent

				// Check for and install required YAML frontmatter
				title := "index"
				// This is the separate input that tagdog.js throws new tags into
				tags := []string{"yeah"}
				permission := "public"

				favoritebool := false

				fm := &frontmatter{
					Title:      title,
					Tags:       tags,
					Favorite:   favoritebool,
					Permission: permission,
				}

				thewiki := &wiki{
					Title:       title,
					Filename:    name,
					Frontmatter: fm,
					Content:     []byte(content),
				}

				err = thewiki.save(mutex)
				if err != nil {
					checkT(err, t)
				}
				wg.Done()
			}
		}()
	}
	wg.Wait()
}

func TestMultipleMapReads(t *testing.T) {
	err := gitCloneTest()
	checkT(err, t)
	tmpdb := tempfile()
	defer os.Remove(tmpdb)
	authState, err := auth.NewAuthState(tmpdb, "admin")
	checkT(err, t)

	viper.Set("CacheEnabled", false)
	viper.Set("Debug", false)
	httputils.Debug = false

	e := testEnv(t, authState)
	e.cache = buildCache()

	err = tmplInit(e)
	checkT(err, t)

	var wg sync.WaitGroup

	for w := 0; w < 50; w++ {
		wg.Add(1)
		t.Log(w)
		go func() {
			for k, v := range e.cache.Tags {
				t.Log(k, v)
			}
			t.Log(e.cache.Tags["omg"])
			for k1, v1 := range e.cache.Favs {
				t.Log(k1, v1)
			}
			wg.Done()
		}()
	}
	for x := 0; x < 50; x++ {
		wg.Add(1)
		t.Log(x)
		go func() {
			e.cache.Favs["omg"] = struct{}{}
			wg.Done()
		}()
	}
	wg.Wait()
}
*/
