package auth

// **Currently using plain "context" package included in Go 1.7, so not backwards compatible**

//Auth functions
// Currently handles the following:
//  User Auth:
//   - User sign-up, stored in a Boltdb named auth.db
//   - User roles, currently hard-coded to two, "User" and "Admin", probably case-sensitive
//   - User authentication against Boltdb and optionally LDAP
//       - Cookie-powered
//       - With gorilla/context to help pass around the user info
//   - Boltdb powered, using Users and Roles buckets
//   - Success/failure is delivered via a redirect and a flash message
//
//  XSRF:
//   - Cross-site Request Forgery protection, using the same concept I use for auth functions above

// Required URLs:
// This lib only handles POST requests on the following URLs
// - Login - /login
// - Signup - /signup

// TODO:
//  - Switch to Bolt for storing User info
//      - Mostly working

import (
	//"github.com/gorilla/securecookie"
	"errors"
	"strconv"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strings"
	"crypto/subtle"

	"github.com/boltdb/bolt"
	//"github.com/gorilla/context"
	"github.com/gorilla/sessions"
	//"github.com/mavricknz/ldap"
	//"github.com/spf13/viper"
	//"gopkg.in/hlandau/passlib.v1"
	"context"
	"jba.io/go/utils"
	"golang.org/x/crypto/bcrypt"
	//"github.com/dimfeld/httptreemux"
	//"github.com/julienschmidt/httprouter"
)

type key int

const TokenKey key = 0
const UserKey key = 1
const MsgKey key = 2

// AuthConf: Pass Auth inside config.json
/*
   "AuthConf": {
           "Users": {},
           "LdapEnabled": true,
           "LdapPort": 389,
           "LdapUrl": "frink.es.gy",
           "LdapUn": "uid",
           "LdapOu": "People",
           "LdapDn": "dc=jba,dc=io"
   }
*/
// Then decode and populate this struct using code from the main app
type AuthConf struct {
	AdminUser   string
	//LdapEnabled bool
	//LdapConf
}

/*
type LdapConf struct {
	LdapPort uint16 `json:",omitempty"`
	LdapUrl  string `json:",omitempty"`
	LdapDn   string `json:",omitempty"`
	LdapUn   string `json:",omitempty"`
	LdapOu   string `json:",omitempty"`
}
*/

type User struct {
	Username string
	IsAdmin  bool
}

type Flash struct {
	Msg	 string
}

type Token   string

var Authcfg = AuthConf{}

var Authdb *bolt.DB

//var sCookieHandler = securecookie.New(
//	securecookie.GenerateRandomKey(64),
//	securecookie.GenerateRandomKey(32))

var CookieHandler = sessions.NewCookieStore(
	[]byte("5CO4mHhkuV4BVDZT72pfkNxVhxOMHMN9lTZjGihKJoNWOUQf5j32NF2nx8RQypUh"),
	[]byte("YuBmqpu4I40ObfPHw0gl7jeF88bk4eT4"),
)

func Open(path string) *bolt.DB {
	var err error
	Authdb, err = bolt.Open(path, 0600, nil)
	if err != nil {
		log.Println(err)
	}
	return Authdb
}

// HashPassword generates a bcrypt hash of the password using work factor 14.
func HashPassword(password []byte) ([]byte, error) {
	return bcrypt.GenerateFromPassword(password, 14)
}

// CheckPassword securely compares a bcrypt hashed password with its possible
// plaintext equivalent.  Returns nil on success, or an error on failure.
func CheckPasswordHash(hash, password []byte) error {
	return bcrypt.CompareHashAndPassword(hash, password)
}

func newUserContext(c context.Context, u *User) context.Context {
	return context.WithValue(c, UserKey, u)
}

func fromUserContext(c context.Context) (*User, bool) {
	u, ok := c.Value(UserKey).(*User)
	return u, ok
}

func newFlashContext(c context.Context, f *Flash) context.Context {
	return context.WithValue(c, MsgKey, f)
}

func fromFlashContext(c context.Context) (*Flash, bool) {
	f, ok := c.Value(MsgKey).(*Flash)
	return f, ok
}

func newTokenContext(c context.Context, t string) context.Context {
	return context.WithValue(c, TokenKey, t)
}

func fromTokenContext(c context.Context) (string, bool) {
	t, ok := c.Value(TokenKey).(string)
	return t, ok
}

// SetSession Takes a key, and a value to store inside a cookie
// Currently used for username and CSRF tokens
func SetSession(key, val string, w http.ResponseWriter, r *http.Request) {
	//defer timeTrack(time.Now(), "SetSession")
	session, err := CookieHandler.Get(r, "session")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	session.Options = &sessions.Options{
		Path:     "/",
		HttpOnly: true,
		Secure:   false,
	}
	session.Values[key] = val
	session.Save(r, w)
}

// SetFlash sets a flash message inside a cookie, which, combined with the UserEnvMiddle
//   middleware, pushes the message into context and then template
func SetFlash(msg string, w http.ResponseWriter, r *http.Request) {
	SetSession("flash", msg, w, r)
}

// ClearSession currently only clearing the user value
// The CSRF token should always be around due to the login form and such
func ClearSession(w http.ResponseWriter, r *http.Request) {
	s, err := CookieHandler.Get(r, "session")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	_, ok := s.Values["user"].(string)
	if ok {
		delete(s.Values, "user")
		s.Save(r, w)
	}
}

func clearFlash(w http.ResponseWriter, r *http.Request) {
	s, err := CookieHandler.Get(r, "session")
	if err != nil {
		return
	}
	_, ok := s.Values["flash"].(string)
	if ok {
		utils.Debugln("flash cleared")
		delete(s.Values, "flash")
		s.Save(r, w)
	}
}

func getUsernameFromCookie(r *http.Request) (username string) {
	s, _ := CookieHandler.Get(r, "session")
	userC, ok := s.Values["user"].(string)
	if !ok {
		userC = ""
	}

	return userC
}

func getFlashFromCookie(r *http.Request) (message string) {
	s, _ := CookieHandler.Get(r, "session")

	messageC, ok := s.Values["flash"].(string)
	if !ok {
		messageC = ""
	}

	return messageC
}

// Retrieve a token
func getTokenFromCookie(r *http.Request) (token string) {
	s, _ := CookieHandler.Get(r, "session")
	token, ok := s.Values["token"].(string)
	if !ok {

	}
	return token
}

// GetUsername retrieves username, and admin bool from context
func GetUsername(c context.Context) (username string, isAdmin bool) {
	//defer timeTrack(time.Now(), "GetUsername")
	userC, ok := fromUserContext(c)
	if !ok {
		utils.Debugln("No username in context.")
		userC = &User{}
	}
	if ok {
		username = userC.Username
		isAdmin = userC.IsAdmin
	}

	return username, isAdmin
}

// GetToken retrieves token from context
func GetFlash(c context.Context) string {
	//defer timeTrack(time.Now(), "GetUsername")
	var flash string
	t, ok := fromFlashContext(c)
	if !ok {
		utils.Debugln("No token in context.")
		flash = ""
	}
	if ok {
		flash = t.Msg
	}
	return flash
}

// GetToken retrieves token from context
func GetToken(c context.Context) string {
	//defer timeTrack(time.Now(), "GetUsername")
	t, ok := fromTokenContext(c)
	if !ok {
		utils.Debugln("No token in context.")
		t = ""
	}
	return t
}

func genToken(w http.ResponseWriter, r *http.Request) string {
	token := utils.RandKey(32)
	SetSession("token", token, w, r)
	utils.Debugln("genToken: " + token)
	return token
}

// Only set a new token if one doesn't already exist
func setToken(w http.ResponseWriter, r *http.Request) (context.Context, string) {
	s, _ := CookieHandler.Get(r, "session")
	token, ok := s.Values["token"].(string)
	if !ok {
		token = utils.RandKey(32)
		SetSession("token", token, w, r)
		utils.Debugln("new token generated")
	}
	utils.Debugln("Cookie Token: " + token)
	return newTokenContext(r.Context(), token), token
}

// Given an http.Request with a token input, compare it to the token in the session cookie
func CheckToken(w http.ResponseWriter, r *http.Request) error {
	flashToken := GetToken(r.Context())
	tmplToken := r.FormValue("token")
	if tmplToken == "" {
		//http.Error(w, "CSRF Blank.", 500)
		utils.Debugln("**CSRF blank**")
		return fmt.Errorf("CSRF Blank! flashToken: %s tmplToken: %s", flashToken, tmplToken)
	}
	if !verifyToken(tmplToken, flashToken) {
		//http.Error(w, "CSRF error!", 500)
		utils.Debugln("**CSRF mismatch!**")
		return fmt.Errorf("CSRF Mismatch! flashToken: %s tmplToken: %s", flashToken, tmplToken)
	}
	// Generate a new CSRF token after this one has been used
	newToken := utils.RandKey(32)
	SetSession("token", newToken, w, r)
	utils.Debugln("newToken: " + newToken)
	return nil
}

//UserSignupPostHandler only handles POST requests, using forms named "username" and "password"
// Signing up users as necessary, inside the AuthConf
func UserSignupPostHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
	case "POST":
		username := template.HTMLEscapeString(r.FormValue("username"))
		password := template.HTMLEscapeString(r.FormValue("password"))
		err := newUser(username, password)
		if err != nil {
			utils.Debugln(err)
			panic(err)
		}

		SetSession("flash", "Successfully added '"+username+"' user.", w, r)
		postRedir(w, r, r.Referer())

	case "PUT":
		// Update an existing record.
	case "DELETE":
		// Remove the record.
	default:
		// Give an error message.
	}
}

//AdminUserPassChangePostHandler only handles POST requests, using forms named "username" and "password"
// Signing up users as necessary, inside the AuthConf
func AdminUserPassChangePostHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
	case "POST":
		username := template.HTMLEscapeString(r.FormValue("username"))
		password := template.HTMLEscapeString(r.FormValue("password"))
		// Hash password now so if it fails we catch it before touching Bolt
		//hash, err := passlib.Hash(password)
		hash, err := HashPassword([]byte(password))
		if err != nil {
			// couldn't hash password for some reason
			log.Fatalln(err)
			return
		}

		err = updatePass(username, hash)
		if err != nil {
			utils.Debugln(err)
			panic(err)
		}
		SetSession("flash", "Successfully changed '"+username+"' users password.", w, r)
		postRedir(w, r, r.Referer())

	case "PUT":
		// Update an existing record.
	case "DELETE":
		// Remove the record.
	default:
		// Give an error message.
	}
}

//AdminUserDeletePostHandler only handles POST requests, using forms named "username" and "password"
// Signing up users as necessary, inside the AuthConf
func AdminUserDeletePostHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
	case "POST":
		username := template.HTMLEscapeString(r.FormValue("username"))

		err := deleteUser(username)
		if err != nil {
			utils.Debugln(err)
			panic(err)
		}
		SetSession("flash", "Successfully changed '"+username+"' users password.", w, r)
		postRedir(w, r, r.Referer())

	case "PUT":
		// Update an existing record.
	case "DELETE":
		// Remove the record.
	default:
		// Give an error message.
	}
}

//SignupPostHandler only handles POST requests, using forms named "username" and "password"
// Signing up users as necessary, inside the AuthConf
func SignupPostHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
	case "POST":
		username := template.HTMLEscapeString(r.FormValue("username"))
		password := template.HTMLEscapeString(r.FormValue("password"))
		err := newUser(username, password)
		if err != nil {
			utils.Debugln(err)
			SetSession("flash", "User registration failed.", w, r)
			postRedir(w, r, "/signup")
			return
		}

		SetSession("flash", "Successful user registration.", w, r)
		postRedir(w, r, "/login")

		return

	case "PUT":
		// Update an existing record.
	case "DELETE":
		// Remove the record.
	default:
		// Give an error message.
	}
}

//LoginPostHandler only handles POST requests, verifying forms named "username" and "password"
// Comparing values with LDAP or configured username/password combos
func LoginPostHandler(w http.ResponseWriter, r *http.Request) {

	switch r.Method {
	case "GET":
		// This should be handled in a separate function inside your app
		/*
			// Serve login page, replacing loginPageHandler
			defer timeTrack(time.Now(), "loginPageHandler")
			title := "login"
			user := GetUsername(r)
			//p, err := loadPage(title, r)
			data := struct {
				UN  string
				Title string
			}{
				user,
				title,
			}
			err := renderTemplate(w, "login.tmpl", data)
			if err != nil {
				log.Println(err)
				return
			}
		*/
	case "POST":

		// Handle login POST request
		username := template.HTMLEscapeString(r.FormValue("username"))
		password := template.HTMLEscapeString(r.FormValue("password"))
		referer, err := url.Parse(r.Referer())
		if err != nil {
			utils.Debugln(err)
		}

		// Check if we have a ?url= query string, from AuthMiddle
		// Otherwise, just use the referrer
		var r2 string
		r2 = referer.Query().Get("url")
		if r2 == "" {
			utils.Debugln("referer is blank")
			r2 = r.Referer()
			// if r.Referer is blank, just redirect to index
			if r.Referer() == "" || referer.RequestURI() == "/login" {
				r2 = "/"
			}
		}

		// Login authentication
		if auth(username, password) {
			SetSession("user", username, w, r)
			utils.Debugln(username + " successfully logged in.")
			SetSession("flash", "User '"+username+"' successfully logged in.", w, r)
			postRedir(w, r, r2)
			return
		}

		SetSession("flash", "User '"+username+"' failed to login. <br> Please check your credentials and try again.", w, r)
		postRedir(w, r, "/login")

		return

	case "PUT":
		// Update an existing record.
	case "DELETE":
		// Remove the record.
	default:
		// Give an error message.
	}

}

/*
func ldapAuth(un, pw string) bool {
	//Build DN: uid=admin,ou=People,dc=example,dc=com
	dn := Authcfg.LdapUn + "=" + un + ",ou=" + Authcfg.LdapConf.LdapOu + "," + Authcfg.LdapConf.LdapDn
	l := ldap.NewLDAPConnection(Authcfg.LdapConf.LdapUrl, Authcfg.LdapConf.LdapPort)
	err := l.Connect()
	if err != nil {
		utils.Debugln(dn)
		fmt.Printf("LDAP connection error: %v", err)
		return false
	}
	defer l.Close()
	err = l.Bind(dn, pw)
	if err != nil {
		utils.Debugln(dn)
		fmt.Printf("error: %v", err)
		return false
	}
	utils.Debugln("Authenticated via LDAP")
	return true
}
*/

// Bundle of all auth functions, checking which are enabled
func auth(username, password string) bool {
	/*
	if Authcfg.LdapEnabled {
		if ldapAuth(username, password) || boltAuth(username, password) {
			return true
		}
	}
	*/
	if boltAuth(username, password) {
		return true
	}
	return false
}

func boltAuth(username, password string) bool {
	var hashedUserPassByte []byte
	// Grab given user's password from Bolt
	Authdb.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("Users"))
		v := b.Get([]byte(username))
		if v == nil {
			err := errors.New("User does not exist")
			log.Println(err)
			return err
		}
		hashedUserPassByte = v
		return nil
	})
	//hashedUserPass := string(hashedUserPassByte)

	//bcryptPass, err := HashPassword([]byte(password))

	//log.Println(hashedUserPass)
	//log.Println(string(bcryptPass))

	// newHash and err should be blank/nil on success
	//newHash, err := passlib.Verify(password, hashedUserPass)
	err := CheckPasswordHash(hashedUserPassByte, []byte(password))
	if err != nil {
		// Incorrect password, malformed hash, etc.
		log.Println("error verifying password")
		utils.Debugln(err)
		return false
	}

	utils.Debugln("Authenticated via Boltdb")
	return true

}

// Check if user actually exists
func doesUserExist(username string) bool {
	err := Authdb.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("Users"))
		v := b.Get([]byte(username))
		if v == nil {
			err := errors.New("User does not exist")
			return err
		}
		return nil
	})
	if err == nil {
		return true
	} 
	return false
}

func LogoutHandler(w http.ResponseWriter, r *http.Request) {
	ClearSession(w, r)
	utils.Debugln("Logout")
	http.Redirect(w, r, r.Referer(), 302)
}

// Redirect back to given page after successful login or signup.
func postRedir(w http.ResponseWriter, r *http.Request, name string) {
	http.Redirect(w, r, name, http.StatusSeeOther)
}

// Taken from nosurf: https://github.com/justinas/nosurf/blob/master/token.go
func verifyToken(realToken, sentToken string) bool {
	return subtle.ConstantTimeCompare([]byte(realToken), []byte(sentToken)) == 1
}

// Dedicated function to create new users, taking plaintext username, password, and role
//  Hashing done in this function, no need to do it before
func newUser(username, password string) error {

	// Hash password now so if it fails we catch it before touching Bolt
	//hash, err := passlib.Hash(password)
	hash, err := HashPassword([]byte(password))
	if err != nil {
		// couldn't hash password for some reason
		log.Fatalln(err)
		return err
	}

	// If no existing user, store username and hash
	viewerr := Authdb.View(func(tx *bolt.Tx) error {
		userbucket := tx.Bucket([]byte("Users"))

		userbucketUser := userbucket.Get([]byte(username))

		// userbucketUser should be nil if user doesn't exist
		if userbucketUser != nil {
			err := errors.New("User already exists")
			log.Println(err)
			return err
		}
		return nil
	})
	if viewerr != nil {
		return viewerr
	}

	//var vb []byte
	adderr := Authdb.Update(func(tx *bolt.Tx) error {
		userbucket := tx.Bucket([]byte("Users"))

		userbucketUser := userbucket.Get([]byte(username))

		// userbucketUser should be nil if user doesn't exist
		if userbucketUser != nil {
			err := errors.New("User already exists")
			log.Println(err)
			return err
		}

		err = userbucket.Put([]byte(username), []byte(hash))
		if err != nil {
			log.Println(err)
			return err
		}

		return nil
	})

	if adderr != nil {
		return adderr
	}

	return nil
}

func Userlist() ([]string, error) {
	userList := []string{}
	err := Authdb.View(func(tx *bolt.Tx) error {
		userbucket := tx.Bucket([]byte("Users"))
		err := userbucket.ForEach(func(key, value []byte) error {
			//fmt.Printf("A %s is %s.\n", key, value)
			userList = append(userList, string(key))
			return nil
		})
		if err != nil {
			return err
		}
		return nil
	})
	return userList, err
}

func deleteUser(username string) error {
	err := Authdb.Update(func(tx *bolt.Tx) error {
		log.Println(username + " has been deleted")
		return tx.Bucket([]byte("Users")).Delete([]byte(username))
	})
	if err != nil {
		log.Println(err)
		return err
	}
	return err
}

func updatePass(username string, hash []byte) error {

	// Update password only if user exists
	Authdb.Update(func(tx *bolt.Tx) error {
		userbucket := tx.Bucket([]byte("Users"))
		userbucketUser := userbucket.Get([]byte(username))

		// userbucketUser should be nil if user doesn't exist
		if userbucketUser == nil {
			err := errors.New("User does not exist")
			log.Println(err)
			return err
		}
		err := userbucket.Put([]byte(username), hash)
		if err != nil {
			return err
		}
		log.Println("User " + username + " has changed their password.")
		return nil
	})
	return nil
}

//XsrfMiddle is a middleware that tries (no guarantees) to protect against Cross-Site Request Forgery
// On GET requests, it takes a 
func XsrfMiddle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if there's an existing xsrf
		// If not, generate one in the cookie
		reqcx, xsrftoken := setToken(w, r)
		switch r.Method {
		case "GET":
			// If this is a GET request, go ahead and serve the next, with reqcx
			next.ServeHTTP(w, r.WithContext(reqcx))
		case "POST", "PUT", "DELETE":
			// Currently doing CLI checking by user-agent, only excluding curl
			// TODO: Probably a more secure way to do this..special header set in config maybe?
			// This should mean this is a request from the command line, so don't check CSRF
			if strings.HasPrefix(r.UserAgent(), "curl") {
				next.ServeHTTP(w, r)
				return
			}
			tmplToken := r.FormValue("token")
			utils.Debugln("POST: flashToken: " + xsrftoken)
			utils.Debugln("POST: tmplToken: " + tmplToken)
			// Actually check CSRF token, since this is a POST request
			if tmplToken == "" {
				http.Error(w, "CSRF Token Blank.", 500)
				utils.Debugln("**CSRF Token Blank**")
				return
			}
			if !verifyToken(tmplToken, xsrftoken) {
				http.Error(w, "CSRF Token Error!", 500)
				utils.Debugln("**CSRF Token Mismatch!**")
				return
			}

			// If this is a POST request, and the tokens match, generate a new one
			newToken := utils.RandKey(32)
			SetSession("token", newToken, w, r)
			utils.Debugln("newToken: " + newToken)

			next.ServeHTTP(w, r)
		default:

			next.ServeHTTP(w, r)
		}

	})
}

func AuthMiddle(next http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		//username := getUsernameFromCookie(r)
		username, isAdmin := GetUsername(r.Context())
		if username == "" {
			rurl := r.URL.String()
			utils.Debugln("AuthMiddleware mitigating: " + r.Host + rurl)

			// Detect if we're in an endless loop, if so, just panic
			if strings.HasPrefix(rurl, "login?url=/login") {
				panic("AuthMiddle is in an endless redirect loop")
				return
			}
			http.Redirect(w, r, "http://"+r.Host+"/login"+"?url="+rurl, 302)
			return
		}

		utils.Debugln(username + " (is Admin: " + strconv.FormatBool(isAdmin) + ") is visiting " + r.Referer())
		next.ServeHTTP(w, r)
	})
}

func AuthAdminMiddle(next http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, isAdmin := GetUsername(r.Context())
		if username == "" {
			rurl := r.URL.String()
			utils.Debugln("AuthAdminMiddleware mitigating: " + r.Host + rurl)

			// Detect if we're in an endless loop, if so, just panic
			if strings.HasPrefix(rurl, "login?url=/login") {
				panic("AuthAdminMiddle is in an endless redirect loop")
			}
			http.Redirect(w, r, "http://"+r.Host+"/login"+"?url="+rurl, 302)
			return
		}
		//If user is not an Admin, just redirect to index
		if !isAdmin {
			log.Println(username + " attempting to access restricted URL.")
			SetSession("flash", "Sorry, you are not allowed to see that.", w, r)
			postRedir(w, r, "/")
			return
		}

		utils.Debugln(username + " (is Admin: " + strconv.FormatBool(isAdmin) + ") is visiting " + r.Referer())
		next.ServeHTTP(w, r)
	})
}

//UserEnvMiddle grabs username, role, and flash message from cookie,
// tosses it into the context for use in various other middlewares
func UserEnvMiddle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username := getUsernameFromCookie(r)
		message := getFlashFromCookie(r)
		// Check if user actually exists before setting username
		// If user does not exist, clear the session because something fishy is going on
		if !doesUserExist(username) {
			username = ""
			ClearSession(w, r)
		}
		// Delete flash after pushing to context
		clearFlash(w, r)
		// If username is the configured AdminUser, set context to reflect this
		isAdmin := false
		if username == Authcfg.AdminUser {
			utils.Debugln("Setting isAdmin to true due to "+ Authcfg.AdminUser)
			isAdmin = true
		}
		u := &User{
			Username: username,
			IsAdmin: isAdmin,
		}
		f := &Flash{
			Msg: message,
		}
		newc := newUserContext(r.Context(), u)
		newc = newFlashContext(newc, f)
		next.ServeHTTP(w, r.WithContext(newc))
	})
}

func AuthCookieMiddle(next http.HandlerFunc) http.HandlerFunc {
	handler := func(w http.ResponseWriter, r *http.Request) {
		username := getUsernameFromCookie(r)
		if username == "" {
			utils.Debugln("AuthMiddleware mitigating: " + r.Host + r.URL.String())
			http.Redirect(w, r, "http://"+r.Host+"/login"+"?url="+r.URL.String(), 302)
			return
		}
		utils.Debugln(username + " is visiting " + r.Referer())
		next.ServeHTTP(w, r)
	}
	return http.HandlerFunc(handler)
}

func AuthDbInit() error {

	return Authdb.Update(func(tx *bolt.Tx) error {
		userbucket, err := tx.CreateBucketIfNotExists([]byte("Users"))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}

		adminUser := Authcfg.AdminUser
		if adminUser == "" {
			adminUser = "admin"
		}

		userbucketUser := userbucket.Get([]byte(adminUser))
		if userbucketUser == nil {
			fmt.Println("Admin Boltdb user "+ adminUser +" does not exist, creating it.")
			//hash, err := passlib.Hash("admin")
			hash, err := HashPassword([]byte("admin"))
			if err != nil {
				// couldn't hash password for some reason
				log.Fatalln(err)
				return err
			}
			err = userbucket.Put([]byte(adminUser), []byte(hash))
			if err != nil {
				log.Println(err)
				return err
			}

			fmt.Println("***DEFAULT USER CREDENTIALS:***")
			fmt.Println("Username: "+ adminUser)
			fmt.Println("Password: admin")
			return nil
		}
		return nil
	})
}

/*
func Csrf(next http.HandlerFunc) http.HandlerFunc {
	handler := func(w http.ResponseWriter, r *http.Request) {
        r.ParseForm()
		flashToken := GetToken(r)
		tmplToken := r.FormValue("token")
        log.Println("flashToken: "+flashToken)
        log.Println("tmplToken: "+tmplToken)
		next.ServeHTTP(w, r)
	}
	return http.HandlerFunc(handler)
}
*/