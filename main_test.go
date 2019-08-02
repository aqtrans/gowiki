package main

import (
	"bytes"
	"context"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync"
	"testing"

	"git.jba.io/go/auth"
	"git.jba.io/go/httputils"
	log "github.com/sirupsen/logrus"
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
	//viper.Set("Domain", "wiki.example.com")
	//viper.Set("InitWikiRepo", true)
	httputils.Debug = testing.Verbose()
	auth.Debug = testing.Verbose()
	log.SetLevel(log.DebugLevel)
}

func checkT(err error, t *testing.T) {
	if err != nil {
		t.Errorf("ERROR: %v", err)
	}
}

func testGitCommand(args ...string) *exec.Cmd {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		log.Fatalln("git must be installed")
	}
	c := exec.Command(gitPath, args...)
	//c.Dir = viper.GetString("WikiDir")
	return c
}

/*
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
*/

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

func testEnv(authState *auth.State) *wikiEnv {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		log.Fatalln("Git executable was not found in PATH. Git must be installed.")
	}

	return &wikiEnv{
		cfg: config{
			DataDir:        "./tests/data/",
			WikiDir:        "./tests/data/wikidata/",
			GitPath:        gitPath,
			CacheEnabled:   true,
			GitCommitEmail: "test@test.com",
			GitCommitName:  "gowiki-tests",
		},
		authState: *authState,
		cache: &wikiCache{
			Tags: make(map[string][]string),
			Favs: make(map[string]struct{}),
		},
		templates: tmplInit(),
		mutex:     sync.Mutex{},
		tags:      newTagsMap(),
		favs:      newFavsMap(),
	}
}

func testEnvInit() (string, *wikiEnv) {
	tmpdb := tempfile()
	//defer os.Remove(tmpdb)
	authState := auth.NewAuthState(tmpdb)
	e := testEnv(authState)
	return tmpdb, e
}

func TestAuthInit(t *testing.T) {
	tmpdb := tempfile()
	defer os.Remove(tmpdb)
	authState := auth.NewAuthState(tmpdb)
	_, err := authState.Userlist()
	checkT(err, t)
}

func TestTmplInit(t *testing.T) {
	tmpdb := tempfile()
	defer os.Remove(tmpdb)
	authState := auth.NewAuthState(tmpdb)
	testEnv(authState)
}

func TestWikiInit(t *testing.T) {
	tmpdb := tempfile()
	defer os.Remove(tmpdb)
	authState := auth.NewAuthState(tmpdb)
	e := testEnv(authState)

	err := initWikiDir(e.cfg)
	checkT(err, t)
}

// TestNewWikiPage tests if viewing a non-existent article, as a logged in user, properly redirects to /edit/page_name with a 404
func TestNewWikiPage(t *testing.T) {

	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	e.authState.NewAdmin("admin", "admin")

	randPage, err := httputils.RandKey(8)
	checkT(err, t)
	r, err := http.NewRequest("GET", "/"+randPage, nil)
	checkT(err, t)

	w := httptest.NewRecorder()
	ctx := context.Background()
	ctx = e.authState.NewUserInContext(ctx, "admin")
	r = r.WithContext(ctx)

	router(e).ServeHTTP(w, r)

	if status := w.Code; status != http.StatusNotFound {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusNotFound)
	}
}

// TestNewWikiPageNotLoggedIn tests if viewing a non-existent article, while not logged in redirects to a vague login page
func TestNewWikiPageNotLoggedIn(t *testing.T) {

	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	randPage, err := httputils.RandKey(8)
	checkT(err, t)
	r, err := http.NewRequest("GET", "/"+randPage, nil)
	checkT(err, t)

	w := httptest.NewRecorder()

	router(e).ServeHTTP(w, r)

	if status := w.Code; status != http.StatusSeeOther {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusSeeOther)
	}
	// TODO: figure out how to test this since it's in a cookie now
	expected := `/login`
	if w.Header().Get("Location") != expected {
		t.Errorf("handler returned unexpected body: got %v want %v",
			w.Header().Get("Location"), expected)
	}
}

func TestHealthCheckHandler(t *testing.T) {
	req, err := http.NewRequest("GET", "/health", nil)
	checkT(err, t)
	rr := httptest.NewRecorder()

	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	router(e).ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

	expected := `{"alive": true}`
	if rr.Body.String() != expected {
		t.Errorf("handler returned unexpected body: got %v want %v",
			rr.Body.String(), expected)
	}
}

func TestNewHandler(t *testing.T) {
	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	err := e.gitPull()
	checkT(err, t)

	e.authState.NewAdmin("admin", "admin")

	req, err := http.NewRequest("GET", "/afefwdwdef/dwwafefe/fegegrgr", reader)
	checkT(err, t)

	rr := httptest.NewRecorder()

	ctx := context.Background()
	ctx = e.authState.NewUserInContext(ctx, "admin")
	req = req.WithContext(ctx)

	router(e).ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusNotFound {
		t.Log(rr.Body.String())
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusNotFound)
	}
}

// TestIndex tests if viewing the index page, as a logged in user, properly returns a 200
func TestIndexPage(t *testing.T) {
	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	err := e.gitPull()
	checkT(err, t)

	e.authState.NewAdmin("admin", "admin")

	req, err := http.NewRequest("GET", "/", nil)
	checkT(err, t)

	rr := httptest.NewRecorder()
	ctx := context.Background()
	ctx = e.authState.NewUserInContext(ctx, "admin")

	req = req.WithContext(ctx)

	router(e).ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusSeeOther {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusSeeOther)
	}
}

// TestIndexHistoryPage tests if viewing the history of the index page, as a logged in user, properly returns a 200
func TestIndexHistoryPage(t *testing.T) {
	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	err := e.gitPull()
	checkT(err, t)

	e.authState.NewAdmin("admin", "admin")

	req, err := http.NewRequest("GET", "/history/index", nil)
	checkT(err, t)

	rr := httptest.NewRecorder()
	ctx := context.Background()
	ctx = e.authState.NewUserInContext(ctx, "admin")

	req = req.WithContext(ctx)

	router(e).ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}
}

// TestIndexEditPage tests if trying to edit the index page, as a logged in user, properly returns a 200
func TestIndexEditPage(t *testing.T) {
	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	err := e.gitPull()
	checkT(err, t)
	/*
		tmpdb := tempfile()
		defer os.Remove(tmpdb)
		authState, err := auth.NewAuthState(tmpdb, "admin")
		checkT(err, t)

		e := testEnv(t, authState)
		err = tmplInit(e)
		checkT(err, t)
	*/

	e.authState.NewAdmin("admin", "admin")

	req, err := http.NewRequest("GET", "/edit/index", nil)
	checkT(err, t)

	rr := httptest.NewRecorder()
	ctx := context.Background()
	ctx = e.authState.NewUserInContext(ctx, "admin")

	req = req.WithContext(ctx)

	router(e).ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

}

// TestDirBaseHandler tests if trying to create a file 'inside' a file fails
func TestDirBaseHandler(t *testing.T) {
	//setup()
	// Create a request to pass to our handler.
	req, err := http.NewRequest("GET", "/index/what/omg", nil)
	checkT(err, t)

	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	err = e.gitPull()
	checkT(err, t)

	e.authState.NewAdmin("admin", "admin")

	//req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rr := httptest.NewRecorder()

	ctx := context.Background()
	ctx = e.authState.NewUserInContext(ctx, "admin")
	req = req.WithContext(ctx)

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	//router := httptreemux.NewContextMux()
	//router.GET("/*name", e.wikiMiddle(e.viewHandler))

	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.
	//router.DefaultContext = ctx
	router(e).ServeHTTP(rr, req)

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
	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	err := e.gitPull()
	checkT(err, t)

	e.authState.NewAdmin("admin", "admin")

	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	req, err := http.NewRequest("GET", "/recent", nil)
	checkT(err, t)

	//router := httptreemux.NewContextMux()
	//router.GET("/recent", e.authState.AuthMiddle(e.recentHandler))

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	rr := httptest.NewRecorder()
	ctx := context.Background()
	ctx = e.authState.NewUserInContext(ctx, "admin")
	req = req.WithContext(ctx)

	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.
	//router.DefaultContext = ctx
	router(e).ServeHTTP(rr, req)

	// Check the status code is what we expect.
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusSeeOther)
	}
}

func TestListPage(t *testing.T) {
	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	err := e.gitPull()
	checkT(err, t)

	e.authState.NewAdmin("admin", "admin")

	e.buildCache()
	err = os.Remove("./tests/data/cache.gob")
	if err != nil {
		t.Error(err)
	}

	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	req, err := http.NewRequest("GET", "/list", nil)
	checkT(err, t)

	//router := httptreemux.NewContextMux()
	//router.GET("/list", e.listHandler)

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	rr := httptest.NewRecorder()
	ctx := context.Background()
	ctx = e.authState.NewUserInContext(ctx, "admin")
	req = req.WithContext(ctx)

	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.
	//router.DefaultContext = ctx
	router(e).ServeHTTP(rr, req)

	// Check the status code is what we expect.
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusSeeOther)
	}
}

/*
func TestServer(t *testing.T) {
	err := gitPull()
	checkT(err, t)

	tmpdb := tempfile()
	defer os.Remove(tmpdb)
	authState, err := auth.NewAuthState(tmpdb, "admin")
	checkT(err, t)

	e := testEnv(t, authState)

	err = tmplInit(e)
	checkT(err, t)

	ts := httptest.NewServer(router(e))
	defer ts.Close()

	//req, err := http.NewRequest("GET", ts.URL+"/", nil)
	//checkT(err, t)
	res, err := http.Get(ts.URL + "/")
	checkT(err, t)
	t.Log(res.Request.Referer(), res.Request.URL)

}
*/

func TestPrivatePageNotLoggedIn(t *testing.T) {
	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	err := e.gitPull()
	checkT(err, t)

	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	req, err := http.NewRequest("GET", "/sites.page", nil)
	checkT(err, t)

	//router := httptreemux.NewContextMux()
	//router.GET(`/*name`, e.wikiMiddle(e.viewHandler))

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	rr := httptest.NewRecorder()
	ctx := context.Background()
	ctx = context.WithValue(ctx, wikiNameKey, "sites.page")
	ctx = context.WithValue(ctx, wikiExistsKey, true)
	req = req.WithContext(ctx)

	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.
	//router.DefaultContext = ctx
	router(e).ServeHTTP(rr, req)
	//t.Log(rr.Body.String())

	// Check the status code is what we expect.
	if status := rr.Code; status != http.StatusSeeOther {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusSeeOther)
	}
	// TODO: figure out how to test with redirect URL now inside cookie
	expected := `/login`
	if rr.Header().Get("Location") != expected {
		t.Errorf("handler returned unexpected body: got %v want %v",
			rr.Header().Get("Location"), expected)
	}
}

func TestPrivatePageLoggedIn(t *testing.T) {
	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	err := e.gitPull()
	checkT(err, t)

	e.authState.NewAdmin("admin", "admin")

	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	req, err := http.NewRequest("GET", "/sites.page", nil)
	checkT(err, t)

	//router := httptreemux.NewContextMux()
	//router.GET(`/*name`, e.wikiMiddle(e.viewHandler))

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	rr := httptest.NewRecorder()
	ctx := context.Background()
	ctx = e.authState.NewUserInContext(ctx, "admin")
	ctx = context.WithValue(ctx, wikiNameKey, "sites.page")
	ctx = context.WithValue(ctx, wikiExistsKey, true)
	/*
		params := make(map[string]string)
		params["name"] = "sites.page"
		ctx = context.WithValue(ctx, httptreemux.ParamsContextKey, params)
	*/
	req = req.WithContext(ctx)

	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.
	//router.DefaultContext = ctx
	router(e).ServeHTTP(rr, req)
	//t.Log(rr.Body.String())

	// Check the status code is what we expect.
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}
}

func TestSearchPage(t *testing.T) {
	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	err := e.gitPull()
	checkT(err, t)

	e.buildCache()
	if e.cache == nil {
		t.Error("cache is empty")
	}

	e.authState.NewAdmin("admin", "admin")

	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	req, err := http.NewRequest("GET", "/search/omg", nil)
	checkT(err, t)

	//router := httptreemux.NewContextMux()
	//router.GET("/search/*name", e.search)

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	rr := httptest.NewRecorder()
	ctx := context.Background()
	ctx = e.authState.NewUserInContext(ctx, "admin")
	/*
		params := make(map[string]string)
		params["name"] = "omg"
		ctx = context.WithValue(ctx, httptreemux.ParamsContextKey, params)
	*/
	req = req.WithContext(ctx)

	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.
	//router.DefaultContext = ctx
	router(e).ServeHTTP(rr, req)

	// Check the status code is what we expect.
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusSeeOther)
	}
}

// Test that we are unable to access Git repo data (wikidata/.git)
func TestDotGit(t *testing.T) {
	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	err := e.gitPull()
	checkT(err, t)

	e.buildCache()
	if e.cache == nil {
		t.Error("cache is empty")
	}

	e.authState.NewAdmin("admin", "admin")

	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	req, err := http.NewRequest("GET", "/.git/index", nil)
	checkT(err, t)

	//router := httptreemux.NewContextMux()
	//router.GET("/*name", e.wikiMiddle(e.viewHandler))

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	rr := httptest.NewRecorder()
	ctx := context.Background()
	ctx = e.authState.NewUserInContext(ctx, "admin")

	req = req.WithContext(ctx)

	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.
	//router.DefaultContext = ctx
	router(e).ServeHTTP(rr, req)

	// Check the status code is what we expect.
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}

// Test that we are unable to access stuff outside the wikidir
func TestWikiDirEscape(t *testing.T) {
	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	err := e.gitPull()
	checkT(err, t)

	e.buildCache()
	if e.cache == nil {
		t.Error("cache is empty")
	}

	e.authState.NewAdmin("admin", "admin")

	req, err := http.NewRequest("GET", "/../../test.md", nil)
	checkT(err, t)
	rr := httptest.NewRecorder()
	ctx := context.Background()
	ctx = e.authState.NewUserInContext(ctx, "admin")
	req = req.WithContext(ctx)

	router(e).ServeHTTP(rr, req)

	// Check the status code is what we expect.
	if status := rr.Code; status != http.StatusNotFound {
		t.Log(rr.Header())
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusNotFound)
	}
}

// TestWikiHistoryNonExistent tests if trying to view /history/random properly redirects to /random
func TestWikiHistoryNonExistent(t *testing.T) {

	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	e.authState.NewAdmin("admin", "admin")

	randPage, err := httputils.RandKey(8)
	checkT(err, t)
	r, err := http.NewRequest("GET", "/history/"+randPage, nil)
	checkT(err, t)

	ctx := context.Background()
	ctx = e.authState.NewUserInContext(ctx, "admin")
	r = r.WithContext(ctx)

	w := httptest.NewRecorder()

	router(e).ServeHTTP(w, r)

	if status := w.Code; status != http.StatusSeeOther {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusSeeOther)
	}

	// Transform/normalize the randPage name
	checkName(e.cfg.WikiDir, &randPage)

	expected := `/` + randPage
	if w.Header().Get("Location") != expected {
		t.Errorf("handler returned unexpected body: got %v want %v",
			w.Header().Get("Location"), expected)
	}
}

// TestWikiDirIndex tests if trying to view /dir/ properly redirects to /dir/index when it exists
func TestWikiDirIndex(t *testing.T) {

	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	e.authState.NewAdmin("admin", "admin")

	r, err := http.NewRequest("GET", "/work", nil)
	checkT(err, t)

	ctx := context.Background()
	ctx = e.authState.NewUserInContext(ctx, "admin")
	r = r.WithContext(ctx)

	w := httptest.NewRecorder()

	router(e).ServeHTTP(w, r)

	if status := w.Code; status != http.StatusFound {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusFound)
	}

	expected := `/work/index`
	if w.Header().Get("Location") != expected {
		t.Errorf("handler returned unexpected body: got %v want %v",
			w.Header().Get("Location"), expected)
	}
}

func TestCache(t *testing.T) {
	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	e.buildCache()
	if e.cache == nil {
		t.Error("cache is empty")
	}
	e.loadCache()
	if e.cache == nil {
		t.Error("cache is empty after loadCache()")
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

// Tests my custom link renderer, with YAML frontmatter
func TestMarkdownRender5(t *testing.T) {
	rawmdf := "./tests/test5.md"
	_, rawmd := readFileAndFront(rawmdf)

	// Read what rendered Markdown HTML should look like
	rendermdf := "./tests/test5.html"
	rendermd, err := ioutil.ReadFile(rendermdf)
	checkT(err, t)
	// []byte to string
	rendermds := string(rendermd)

	rawmds := markdownRender(rawmd)
	//rawmds := commonmarkRender(rawmd)

	if rawmds != rendermds {
		//ioutil.WriteFile("./tests/test4.html", []byte(rawmds), 0755)
		t.Error("Converted Markdown does not equal test5" + "\n Output: \n" + rawmds + "Expected: \n" + rendermds)
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
	f, err := os.Open("./tests/data/wikidata/index")
	if err != nil {
		log.Println(err)
	}
	defer f.Close()

	for n := 0; n < b.N; n++ {
		topbuf := new(bytes.Buffer)
		bottombuf := new(bytes.Buffer)
		scanWikiPage(f, topbuf, bottombuf)
	}
}

func BenchmarkReadFront2(b *testing.B) {
	f, err := os.Open("./tests/data/wikidata/index")
	if err != nil {
		log.Println(err)
	}
	defer f.Close()

	for n := 0; n < b.N; n++ {
		topbuf := new(bytes.Buffer)
		bottombuf := new(bytes.Buffer)
		scanWikiPageB(f, topbuf, bottombuf)
	}
}

func BenchmarkWholeWiki(b *testing.B) {

	tmpdb := tempfile()
	//defer os.Remove(tmpdb)
	authState := auth.NewAuthState(tmpdb)
	env := &wikiEnv{
		authState: *authState,
		cache: &wikiCache{
			Tags: make(map[string][]string),
			Favs: make(map[string]struct{}),
		},
		templates: tmplInit(),
		mutex:     sync.Mutex{},
		tags:      newTagsMap(),
		favs:      newFavsMap(),
	}
	defer os.Remove(tmpdb)

	req, err := http.NewRequest("GET", "/", nil)
	if err != nil {
		b.Fatal(err)
	}

	//router := httptreemux.NewContextMux()
	//router.GET("/", indexHandler)

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	rr := httptest.NewRecorder()

	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.
	//router.DefaultContext = ctx

	for n := 0; n < b.N; n++ {
		router(env).ServeHTTP(rr, req)
	}
}

func BenchmarkGitCtime(b *testing.B) {
	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	for i := 0; i < b.N; i++ {
		e.gitGetCtime("index")
	}
}

func BenchmarkGitMtime(b *testing.B) {
	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	for i := 0; i < b.N; i++ {
		e.gitGetMtime("index")
	}
}

func TestMultipleWrites(t *testing.T) {
	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	err := e.gitPull()
	checkT(err, t)

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

				fm := frontmatter{
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

				err = thewiki.save(e)
				if err != nil {
					checkT(err, t)
				}
				wg.Done()
			}
		}()
	}
	wg.Wait()
}

/*
func TestMultipleMapReads(t *testing.T) {
	err := gitPull()
	checkT(err, t)
	tmpdb, e := testEnvInit(t)
	defer os.Remove(tmpdb)

	viper.Set("CacheEnabled", false)
	viper.Set("Debug", false)
	httputils.Debug = false

	var wg sync.WaitGroup
	wg.Add(100)

	for w := 0; w < 50; w++ {
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
		t.Log(x)
		go func() {
			e.cache.Favs["omg"] = struct{}{}
			wg.Done()
		}()
	}
	wg.Wait()
}
*/
