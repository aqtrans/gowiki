package main

import (
    "testing"
    "io/ioutil"
    "io"
    "bytes"
    //"github.com/drewolson/testflight"
    "net/http/httptest"
    "net/http"
    "github.com/gorilla/context"
    //"fmt"
    "log"
    "net/url"
    //"os"
    //"jba.io/go/auth"
    "strings"
    "github.com/gorilla/mux"
)

type key int
const UserKey  key = 1
const RoleKey  key = 2

var (
    server   *httptest.Server
    reader   io.Reader //Ignore this for now
    serverUrl string
    m   *mux.Router
    req *http.Request
    respRec *httptest.ResponseRecorder
)

func setup() {
    //mux router with added question routes
    m = mux.NewRouter()
    m.HandleFunc("/new", newHandler)
    Router(m)

    //The response recorder used to record HTTP responses
    respRec = httptest.NewRecorder()
}

/*

func setup() {
    router := Router() //Creating new server with the user handlers
}



func TestSetUser(t *testing.T) {
    
    userJson := url.Values{"newwiki": {"omg/yeah/stuff"}}
    reader = strings.NewReader(userJson.Encode())
    r, err := http.NewRequest("GET", serverUrl+"/new", reader)
    if err != nil {
        log.Println(err) //Something is wrong while sending request
    }
    
    
    
    r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
    auth.SetContext(r, "admin", "Admin", "")
    
    w, err := http.DefaultClient.Do(r)

    if err != nil {
        log.Println(err) //Something is wrong while sending request
    }
    log.Println(context.GetAll(r))
    
    if w.StatusCode != http.StatusCreated {
        t.Errorf("Success expected: %d %s", w.StatusCode, w.Header) //Uh-oh this means our test failed
    }
    
}

type TestDB struct {
    *auth.DB
}

// NewTestDB returns a TestDB using a temporary path.
func NewTestDB() *TestDB {
    // Retrieve a temporary path.
    f, err := ioutil.TempFile("", "")
    if err != nil {
        panic(err)
    }
    path := f.Name()
    f.Close()
    os.Remove(path)

    // Open the database.
    db, err := auth.Open(path, 0600)
    if err != nil {
        panic(err)
    }

    // Return wrapped type.
    return &TestDB{db}
}

// Close and delete Bolt database.
func (db *TestDB) Close() {
    defer os.Remove(db.Path())
    db.DB.Close()
}


func TestCreateUser(t *testing.T) {
    
    //setUser()
    
    db := NewTestDB()
    defer db.Close()    

    //server = httptest.NewServer(Router())
    //router := Router()
    //w := httptest.NewRecorder()   

    userJson := `{"newwiki": "omg-yeah/omg/omg"}`

    reader = strings.NewReader(userJson) //Convert string to reader
    //log.Println(serverUrl)
    r, err := http.NewRequest("POST", serverUrl+"/new", reader) //Create request with JSON body

    if err != nil {
        t.Error(err) //Something is wrong while sending request
    }

    w, err := http.DefaultClient.Do(r)

    if err != nil {
        t.Error(err) //Something is wrong while sending request
    }
    
    if w.StatusCode != http.StatusCreated {
        log.Println(context.GetAll(r))
        t.Errorf("Success expected: %d %s", w.StatusCode, w.Header) //Uh-oh this means our test failed
    }
    
}
*/

func testUserEnvMiddle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        context.Set(r, UserKey, "admin")
        context.Set(r, RoleKey, "Admin")
        log.Println(context.Get(r, UserKey))
        log.Println(context.Get(r, RoleKey))
        next.ServeHTTP(w, r)
	})
}

func TestNewHandler(t *testing.T) {
    setup()
    
    userJson := url.Values{"newwiki": {"omg/yeah/stuff"}}
    reader = strings.NewReader(userJson.Encode())
    
    req, _ = http.NewRequest("POST", serverUrl+"/new", reader)
    
    req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
    
    m.ServeHTTP(respRec, req)
    log.Println(respRec.Code)
    log.Println(respRec.Header())
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
    if rawmds != rendermds {
        t.Error("Converted Markdown does not equal test")
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
    if rawmds != rendermds {
        t.Error("Converted Markdown does not equal test2")
    }
}

// Below is for testing the difference between just writing the Tags string directly as fed in from the wiki form, or using a []string as the source, but having to write them using a for loop
// Results, seems string is the best bet for now: 
//    BenchmarkBufferString-4    10000            996527 ns/op
//    BenchmarkBufferArray-4      1000           1651610 ns/op


var title string = "YEAH BENCHMARK OMG"
var name string = "WOOHOO"
var tagsarray = []string{"OMG","YEAH","WHAT","ZZZZ","FFFF","EEEE","RRRTRT","GRHTH","GBHFT","QPFLG","MGJHIB","LRIGJB","DJCUDK","WIFJV","GKBIBK","XKSDFM","RUFJS","SLDKF","ZKDJF","WIFKFG","EIFLG","DKFIBJ","WWRKG","SLFIBK","PRIVATE"}
var tagsstring string = "OMG, YEAH, WHAT, ZZZZ, FFFF, EEEE, RRRTRT, GRHTH, GBHFT, QPFLG, MGJHIB, LRIGJB, DJCUDK, WIFJV, GKBIBK, XKSDFM, RUFJS, SLDKF ,ZKDJF, WIFKFG, EIFLG, DKFIBJ, WWRKG, SLFIBK, PRIVATE"
var body string = "WOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOO\n# OMG \n # YEAH"

func BenchmarkBufferString(b *testing.B) {
    for n := 0; n < b.N; n++ {
        var buffer bytes.Buffer
        buffer.WriteString("---\n")
        if title == "" {
            title = name
        }
        buffer.WriteString("title: "+ title)
        buffer.WriteString("\n")
        if tagsstring != "" {
            buffer.WriteString("tags: "+ tagsstring)
            buffer.WriteString("\n")
        }
        buffer.WriteString("---\n")
        buffer.WriteString(body)
        body = buffer.String()
    }
}

func BenchmarkBufferArray(b *testing.B) {
    for n := 0; n < b.N; n++ {
        var buffer bytes.Buffer
        buffer.WriteString("---\n")
        if title == "" {
            title = name
        }
        buffer.WriteString("title: "+ title)
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

//func BenchmarkIsPrivate2(b *testing.B) { benchmarkIsPrivate(2, b) }
//func BenchmarkIsPrivateArray2(b *testing.B) { benchmarkIsPrivateArray(2, b) }

//func BenchmarkIsPrivate10(b *testing.B) { benchmarkIsPrivate(10, b) }
//func BenchmarkIsPrivateArray10(b *testing.B) { benchmarkIsPrivateArray(10, b) }

//func BenchmarkIsPrivate100(b *testing.B) { benchmarkIsPrivate(100, b) }
//func BenchmarkIsPrivateArray100(b *testing.B) { benchmarkIsPrivateArray(100, b) }

//func BenchmarkIsPrivate1000(b *testing.B) { benchmarkIsPrivate(1000, b) }
//func BenchmarkIsPrivateArray1000(b *testing.B) { benchmarkIsPrivateArray(1000, b) }

func BenchmarkIsPrivate10000(b *testing.B) { benchmarkIsPrivate(10000, b) }
func BenchmarkIsPrivateArray10000(b *testing.B) { benchmarkIsPrivateArray(10000, b) }

//func BenchmarkIsPrivate100000(b *testing.B) { benchmarkIsPrivate(100000, b) }
//func BenchmarkIsPrivateArray100000(b *testing.B) { benchmarkIsPrivateArray(100000, b) }


