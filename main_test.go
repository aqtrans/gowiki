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
	"path/filepath"
	"sync"
	"testing"

	auth "git.sr.ht/~aqtrans/goauth/v2"
	httputils "git.sr.ht/~aqtrans/gohttputils"
	"github.com/oxtoacart/bpool"
	log "github.com/sirupsen/logrus"
)

const UserKey key = 1
const RoleKey key = 2

var (
	server      *httptest.Server
	reader      io.Reader //Ignore this for now
	serverURL   string
	tempDataDir = tempdir()
	//m         *mux.Router
	//req       *http.Request
	//rr        *httptest.ResponseRecorder
)

/*
func init() {
	//viper.Set("Domain", "wiki.example.com")
	//viper.Set("InitWikiRepo", true)
	httputils.Debug = testing.Verbose()
	auth.Debug = testing.Verbose()
	if testing.Verbose() {
		log.SetLevel(log.DebugLevel)
	}
}
*/

func checkT(err error, t *testing.T) {
	if err != nil {
		t.Errorf("ERROR: %v", err)
	}
}

/*
func testGitCommand(args ...string) *exec.Cmd {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		log.Fatalln("git must be installed")
	}
	c := exec.Command(gitPath, args...)
	//c.Dir = viper.GetString("WikiDir")
	return c
}
*/
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
func tempfile() auth.Config {
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
	cfg := auth.Config{
		DbPath: f.Name(),
	}
	return cfg
}

// tempdir creates a temporary directory for use as DataDir and WikiDir
func tempdir() string {
	tempDataDir, err := ioutil.TempDir("", "gowikidata-")
	if err != nil {
		panic(err)
	}
	return tempDataDir
}

// setup a wiki dir for testing. just add a few random commits so various functions work
func initTestWikiDir(e *wikiEnv) {

	// index page
	indexPage := &wiki{
		Title:    "index",
		Filename: "index",
		Frontmatter: frontmatter{
			Title:      "index",
			Tags:       []string{"yeah"},
			Favorite:   false,
			Permission: publicPermission,
		},
		Content: []byte("This is the index page!"),
	}

	err := indexPage.save(e)
	if err != nil {
		panic(err)
	}

	// page index page
	dirIndexPage := &wiki{
		Title:    "omg/index",
		Filename: "omg/index",
		Frontmatter: frontmatter{
			Title:      "index",
			Tags:       []string{"index", "omg"},
			Favorite:   false,
			Permission: publicPermission,
		},
		Content: []byte("This is a directory index page!"),
	}

	err = dirIndexPage.save(e)
	if err != nil {
		panic(err)
	}

	// private page
	privatePage := &wiki{
		Title:    "private",
		Filename: "private",
		Frontmatter: frontmatter{
			Title:      "private info",
			Tags:       []string{"secrets"},
			Favorite:   false,
			Permission: privatePermission,
		},
		Content: []byte("This is a private page!"),
	}

	err = privatePage.save(e)
	if err != nil {
		panic(err)
	}

	// admin page
	adminPage := &wiki{
		Title:    "admin",
		Filename: "admin-only",
		Frontmatter: frontmatter{
			Title:      "admin only",
			Tags:       []string{"admins", "super secrets"},
			Favorite:   false,
			Permission: adminPermission,
		},
		Content: []byte("This is a very private, admin only page!"),
	}

	err = adminPage.save(e)
	if err != nil {
		panic(err)
	}

	randContent, err := httputils.RandKey(32)
	if err != nil {
		panic(err)
	}
	randTitle, err := httputils.RandKey(8)
	if err != nil {
		panic(err)
	}
	// another page
	anotherPage := &wiki{
		Title:    randTitle,
		Filename: "random",
		Frontmatter: frontmatter{
			Title:      randTitle,
			Tags:       []string{"yeah", "omg"},
			Favorite:   false,
			Permission: "public",
		},
		Content: []byte(randContent),
	}

	err = anotherPage.save(e)
	if err != nil {
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

func testEnv(authState *auth.State) *wikiEnv {
	log.SetOutput(io.Discard)

	gitPath, err := exec.LookPath("git")
	if err != nil {
		log.Println(err, gitPath)
		//log.Fatalln(exec.Command("which","git").Run())
		log.Fatalln("Git executable was not found in PATH. Git must be installed.")
	}
	//gitPath := ""

	return &wikiEnv{
		cfg: config{
			DataDir:        tempDataDir,
			WikiDir:        filepath.Join(tempDataDir, "wikidata"),
			GitPath:        gitPath,
			CacheEnabled:   true,
			GitCommitEmail: "test@test.com",
			GitCommitName:  "gowiki-tests",
			Prometheus:     false,
		},
		authState: *authState,
		cache: wikiCache{
			Tags: make(map[string][]string),
			Favs: make(map[string]struct{}),
		},
		templates:     tmplInit(),
		pageWriteLock: sync.Mutex{},
		tags:          newTagsMap(),
		favs:          newFavsMap(),
		pool:          bpool.NewBufferPool(64),
		testing:       true,
	}
}

func testEnvInit() (string, *wikiEnv) {
	tmpdb := tempfile()
	//defer os.Remove(tmpdb)
	authState := auth.NewAuthState(tmpdb)
	e := testEnv(authState)
	return tmpdb.DbPath, e
}

func TestAuthInit(t *testing.T) {
	tmpdb := tempfile()
	defer os.Remove(tmpdb.DbPath)
	authState := auth.NewAuthState(tmpdb)
	_, err := authState.Userlist()
	checkT(err, t)
}

func TestTmplInit(t *testing.T) {
	tmpdb := tempfile()
	defer os.Remove(tmpdb.DbPath)
	authState := auth.NewAuthState(tmpdb)
	testEnv(authState)
}

func TestWikiInit(t *testing.T) {
	tmpdb := tempfile()
	defer os.Remove(tmpdb.DbPath)
	authState := auth.NewAuthState(tmpdb)
	e := testEnv(authState)

	err := initWikiDir(e.cfg)
	checkT(err, t)

	initTestWikiDir(e)
}

// TestNewWikiPage tests if viewing a non-existent article, as a logged in user, properly redirects to /edit/page_name with a 404
func TestNewWikiPage(t *testing.T) {

	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	e.authState.NewAdmin("admin", "admin")

	randPage, err := httputils.RandKey(8)
	if err != nil {
		t.Fail()
	}

	r := httptest.NewRequest("GET", "/"+randPage, nil)

	w := httptest.NewRecorder()

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.authState.Login("admin", r)
	})

	handler := e.authState.LoadAndSave(testHandler)
	handler.ServeHTTP(w, r)

	r.Header = http.Header{"Cookie": w.HeaderMap["Set-Cookie"]}
	w2 := httptest.NewRecorder()

	router(e).ServeHTTP(w2, r)

	if status := w2.Code; status != http.StatusNotFound {
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
	r := httptest.NewRequest("GET", "/"+randPage, nil)

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
	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()

	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	router(e).ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

	expected := `.`
	if rr.Body.String() != expected {
		t.Errorf("handler returned unexpected body: got %v want %v",
			rr.Body.String(), expected)
	}
}

func TestNewHandler(t *testing.T) {
	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	//err := e.gitPull()
	//checkT(err, t)

	e.authState.NewAdmin("admin", "admin")

	r := httptest.NewRequest("GET", "/afefwdwdef/dwwafefe/fegegrgr", reader)
	w := httptest.NewRecorder()

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.authState.Login("admin", r)
	})

	handler := e.authState.LoadAndSave(testHandler)
	handler.ServeHTTP(w, r)

	r.Header = http.Header{"Cookie": w.HeaderMap["Set-Cookie"]}

	w2 := httptest.NewRecorder()

	router(e).ServeHTTP(w2, r)

	if status := w2.Code; status != http.StatusNotFound {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusNotFound)
	}
}

// TestIndex tests if viewing the index page, as a logged in user, properly returns a 200
func TestIndexPage(t *testing.T) {
	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	//err := e.gitPull()
	//checkT(err, t)

	e.authState.NewAdmin("admin", "admin")

	r := httptest.NewRequest("GET", "/", nil)

	w := httptest.NewRecorder()

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.authState.Login("admin", r)
	})

	handler := e.authState.LoadAndSave(testHandler)
	handler.ServeHTTP(w, r)

	r.Header = http.Header{"Cookie": w.HeaderMap["Set-Cookie"]}

	w2 := httptest.NewRecorder()

	router(e).ServeHTTP(w2, r)

	if status := w2.Code; status != http.StatusSeeOther {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusSeeOther)
	}
}

// TestIndexHistoryPage tests if viewing the history of the index page, as a logged in user, properly returns a 200
func TestIndexHistoryPage(t *testing.T) {
	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	//err := e.gitPull()
	//checkT(err, t)

	e.authState.NewAdmin("admin", "admin")

	r := httptest.NewRequest("GET", "/history/index", nil)

	w := httptest.NewRecorder()

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.authState.Login("admin", r)
	})

	handler := e.authState.LoadAndSave(testHandler)
	handler.ServeHTTP(w, r)

	r.Header = http.Header{"Cookie": w.HeaderMap["Set-Cookie"]}

	w2 := httptest.NewRecorder()

	router(e).ServeHTTP(w2, r)

	if status := w.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}
}

// TestIndexEditPage tests if trying to edit the index page, as a logged in user, properly returns a 200
func TestIndexEditPage(t *testing.T) {
	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	//err := e.gitPull()
	//checkT(err, t)
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

	r := httptest.NewRequest("GET", "/edit/index", nil)

	w := httptest.NewRecorder()

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.authState.Login("admin", r)
	})

	handler := e.authState.LoadAndSave(testHandler)
	handler.ServeHTTP(w, r)

	w2 := httptest.NewRecorder()

	r.Header = http.Header{"Cookie": w.HeaderMap["Set-Cookie"]}

	router(e).ServeHTTP(w2, r)

	if status := w2.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

}

// TestDirBaseHandler tests if trying to create a file 'inside' a file fails
func TestDirBaseHandler(t *testing.T) {
	//setup()
	// Create a request to pass to our handler.
	r := httptest.NewRequest("GET", "/index/what/omg", nil)

	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	//err = e.gitPull()
	//checkT(err, t)

	e.authState.NewAdmin("admin", "admin")

	//req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w := httptest.NewRecorder()

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.authState.Login("admin", r)
	})

	handler := e.authState.LoadAndSave(testHandler)
	handler.ServeHTTP(w, r)

	r.Header = http.Header{"Cookie": w.HeaderMap["Set-Cookie"]}

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	//router := httptreemux.NewContextMux()
	//router.GET("/*name", e.wikiMiddle(e.viewHandler))

	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.
	//router.DefaultContext = ctx
	w2 := httptest.NewRecorder()
	router(e).ServeHTTP(w2, r)

	// Check the status code is what we expect.
	if status := w2.Code; status != http.StatusInternalServerError {
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

	//err := e.gitPull()
	//checkT(err, t)

	e.authState.NewAdmin("admin", "admin")

	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	r := httptest.NewRequest("GET", "/recent", nil)

	//router := httptreemux.NewContextMux()
	//router.GET("/recent", e.authState.AuthMiddle(e.recentHandler))

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	w := httptest.NewRecorder()

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.authState.Login("admin", r)
	})

	handler := e.authState.LoadAndSave(testHandler)
	handler.ServeHTTP(w, r)

	r.Header = http.Header{"Cookie": w.HeaderMap["Set-Cookie"]}

	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.
	//router.DefaultContext = ctx
	router(e).ServeHTTP(w, r)

	// Check the status code is what we expect.
	if status := w.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusSeeOther)
	}
}

func TestListPage(t *testing.T) {
	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	//err := e.gitPull()
	//checkT(err, t)

	e.authState.NewAdmin("admin", "admin")

	e.buildCache()
	err := os.Remove(filepath.Join(e.cfg.DataDir, "cache.gob"))
	if err != nil {
		t.Error(err)
	}

	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	r := httptest.NewRequest("GET", "/list", nil)

	//router := httptreemux.NewContextMux()
	//router.GET("/list", e.listHandler)

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	w := httptest.NewRecorder()

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.authState.Login("admin", r)
	})

	handler := e.authState.LoadAndSave(testHandler)
	handler.ServeHTTP(w, r)

	r.Header = http.Header{"Cookie": w.HeaderMap["Set-Cookie"]}

	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.
	//router.DefaultContext = ctx
	router(e).ServeHTTP(w, r)

	// Check the status code is what we expect.
	if status := w.Code; status != http.StatusOK {
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

	//req := httptest.NewRequest("GET", ts.URL+"/", nil)
	res, err := http.Get(ts.URL + "/")
	checkT(err, t)
	t.Log(res.Request.Referer(), res.Request.URL)

}
*/

func TestPrivatePageNotLoggedIn(t *testing.T) {
	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	//err := e.gitPull()
	//checkT(err, t)

	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	req := httptest.NewRequest("GET", "/private", nil)

	//router := httptreemux.NewContextMux()
	//router.GET(`/*name`, e.wikiMiddle(e.viewHandler))

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	rr := httptest.NewRecorder()
	ctx := context.Background()
	ctx = context.WithValue(ctx, wikiNameKey, "private")
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

	//err := e.gitPull()
	//checkT(err, t)

	e.authState.NewAdmin("admin", "admin")

	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	r := httptest.NewRequest("GET", "/private", nil)

	//router := httptreemux.NewContextMux()
	//router.GET(`/*name`, e.wikiMiddle(e.viewHandler))

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	w := httptest.NewRecorder()

	// Login
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.authState.Login("admin", r)
	})
	handler := e.authState.LoadAndSave(testHandler)
	handler.ServeHTTP(w, r)

	r.Header = http.Header{"Cookie": w.HeaderMap["Set-Cookie"]}

	ctx := context.Background()
	ctx = context.WithValue(ctx, wikiNameKey, "private")
	ctx = context.WithValue(ctx, wikiExistsKey, true)
	/*
		params := make(map[string]string)
		params["name"] = "sites.page"
		ctx = context.WithValue(ctx, httptreemux.ParamsContextKey, params)
	*/
	r = r.WithContext(ctx)

	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.
	//router.DefaultContext = ctx
	w2 := httptest.NewRecorder()
	router(e).ServeHTTP(w2, r)
	//t.Log(rr.Body.String())

	// Check the status code is what we expect.
	if status := w2.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}
}

func TestSearchPage(t *testing.T) {
	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	//err := e.gitPull()
	//checkT(err, t)

	e.loadCache()

	if e.cache.Cache == nil {
		t.Error("cache is empty")
	}

	e.authState.NewAdmin("admin", "admin")

	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	r := httptest.NewRequest("GET", "/search/omg", nil)

	//router := httptreemux.NewContextMux()
	//router.GET("/search/*name", e.search)

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	w := httptest.NewRecorder()

	// Login
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.authState.Login("admin", r)
	})
	handler := e.authState.LoadAndSave(testHandler)
	handler.ServeHTTP(w, r)
	r.Header = http.Header{"Cookie": w.HeaderMap["Set-Cookie"]}

	/*
		params := make(map[string]string)
		params["name"] = "omg"
		ctx = context.WithValue(ctx, httptreemux.ParamsContextKey, params)
	*/

	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.
	//router.DefaultContext = ctx
	router(e).ServeHTTP(w, r)

	// Check the status code is what we expect.
	if status := w.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusSeeOther)
	}
}

// Test that we are unable to access Git repo data (wikidata/.git)
func TestDotGit(t *testing.T) {
	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	//err := e.gitPull()
	//checkT(err, t)

	e.loadCache()
	if e.cache.Cache == nil {
		t.Error("cache is empty")
	}

	e.authState.NewAdmin("admin", "admin")

	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	r := httptest.NewRequest("GET", "/.git/index", nil)

	//router := httptreemux.NewContextMux()
	//router.GET("/*name", e.wikiMiddle(e.viewHandler))

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	w := httptest.NewRecorder()

	// Login
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.authState.Login("admin", r)
	})
	handler := e.authState.LoadAndSave(testHandler)
	handler.ServeHTTP(w, r)
	r.Header = http.Header{"Cookie": w.HeaderMap["Set-Cookie"]}

	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.
	//router.DefaultContext = ctx
	w2 := httptest.NewRecorder()
	router(e).ServeHTTP(w2, r)

	// Check the status code is what we expect.
	if status := w2.Code; status != http.StatusUnauthorized {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusUnauthorized)
	}
}

// Test that we are unable to access stuff outside the wikidir
func TestWikiDirEscape(t *testing.T) {
	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	//err := e.gitPull()
	//checkT(err, t)

	e.loadCache()

	if e.cache.Cache == nil {
		t.Error("cache is empty")
	}

	e.authState.NewAdmin("admin", "admin")

	r := httptest.NewRequest("GET", "/../../test.md", nil)
	w := httptest.NewRecorder()

	// Login
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.authState.Login("admin", r)
	})
	handler := e.authState.LoadAndSave(testHandler)
	handler.ServeHTTP(w, r)

	r.Header = http.Header{"Cookie": w.HeaderMap["Set-Cookie"]}

	w2 := httptest.NewRecorder()

	router(e).ServeHTTP(w2, r)

	// Check the status code is what we expect.
	if status := w2.Code; status != http.StatusNotFound {
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
	r := httptest.NewRequest("GET", "/history/"+randPage, nil)

	w := httptest.NewRecorder()

	// Login
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.authState.Login("admin", r)
	})
	handler := e.authState.LoadAndSave(testHandler)
	handler.ServeHTTP(w, r)
	r.Header = http.Header{"Cookie": w.HeaderMap["Set-Cookie"]}

	w2 := httptest.NewRecorder()

	router(e).ServeHTTP(w2, r)

	if status := w2.Code; status != http.StatusSeeOther {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusSeeOther)
	}

	// Transform/normalize the randPage name
	e.checkName(&randPage)

	expected := `/` + randPage
	if w2.Header().Get("Location") != expected {
		t.Errorf("handler returned unexpected body: got %v want %v",
			w2.Header().Get("Location"), expected)
	}
}

// TestWikiDirIndex tests if trying to view /dir/ properly redirects to /dir/index when it exists
func TestWikiDirIndex(t *testing.T) {

	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	e.authState.NewAdmin("admin", "admin")

	r := httptest.NewRequest("GET", "/omg", nil)

	w := httptest.NewRecorder()

	// Login
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.authState.Login("admin", r)
	})
	handler := e.authState.LoadAndSave(testHandler)
	handler.ServeHTTP(w, r)
	r.Header = http.Header{"Cookie": w.HeaderMap["Set-Cookie"]}

	w2 := httptest.NewRecorder()

	router(e).ServeHTTP(w2, r)

	if status := w2.Code; status != http.StatusFound {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusFound)
	}

	expected := `/omg/index`
	if w2.Header().Get("Location") != expected {
		t.Errorf("handler returned unexpected body: got %v want %v",
			w2.Header().Get("Location"), expected)
	}
}

func TestCache(t *testing.T) {
	tmpdb, e := testEnvInit()
	defer os.Remove(tmpdb)

	theCache := e.loadCache()

	if &theCache == nil {
		t.Error("cache is empty")
	}

	theCache2 := e.loadCache()
	if &theCache2 == nil {
		t.Error("cache is empty after loadCache()")
	}
	err := os.Remove(filepath.Join(e.cfg.DataDir, "cache.gob"))
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

	// Turn off all logging
	log.SetOutput(io.Discard)

	tmpdb := tempfile()
	//defer os.Remove(tmpdb)
	authState := auth.NewAuthState(tmpdb)
	env := &wikiEnv{
		authState: *authState,
		cache: wikiCache{
			Tags: make(map[string][]string),
			Favs: make(map[string]struct{}),
		},
		templates:     tmplInit(),
		pageWriteLock: sync.Mutex{},
		tags:          newTagsMap(),
		favs:          newFavsMap(),
		testing:       true,
	}
	defer os.Remove(tmpdb.DbPath)

	req := httptest.NewRequest("GET", "/", nil)

	//router := httptreemux.NewContextMux()
	//router.GET("/", indexHandler)

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	rr := httptest.NewRecorder()

	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.
	//router.DefaultContext = ctx

	for n := 0; n < b.N; n++ {
		log.SetOutput(io.Discard)
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

	//err := e.gitPull()
	//checkT(err, t)

	var wg sync.WaitGroup
	wg.Add(50)

	for w := 0; w < 50; w++ {
		go func() {
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
