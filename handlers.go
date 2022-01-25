package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"git.jba.io/go/auth/v2"
	"git.jba.io/go/httputils"
	"github.com/go-chi/chi/v5"
	fuzzy2 "github.com/renstrom/fuzzysearch/fuzzy"
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
)

func (env *wikiEnv) setFavoriteHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "setFavoriteHandler")
	name := chi.URLParam(r, "*")

	if !wikiExistsFromContext(r.Context()) {
		http.Redirect(w, r, "/"+name, http.StatusFound)
		return
	}
	p := env.loadWikiPage(r, name)
	if p.Wiki.Frontmatter.Favorite {
		p.Wiki.Frontmatter.Favorite = false
		env.authState.SetFlash(name+" has been un-favorited.", w)
		log.Println(name + " page un-favorited!")
	} else {
		p.Wiki.Frontmatter.Favorite = true
		env.authState.SetFlash(name+" has been favorited.", w)
		log.Println(name + " page favorited!")
	}

	err := p.Wiki.save(env)
	if err != nil {
		log.WithFields(logrus.Fields{
			"page":  name,
			"error": err,
		}).Errorln("error saving wiki page")
		http.Error(w, "error saving wiki page. check logs for more information", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/"+name, http.StatusSeeOther)

}

func (env *wikiEnv) deleteHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "deleteHandler")
	name := chi.URLParam(r, "*")

	if !wikiExistsFromContext(r.Context()) {
		http.Redirect(w, r, "/"+name, http.StatusFound)
		return
	}
	err := env.gitRmFilepath(name)
	if err != nil {
		log.WithFields(logrus.Fields{
			"page":  name,
			"error": err,
		}).Errorln("error deleting file from git repo")
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}

	err = env.gitCommitWithMessage(name + " has been removed from git repo.")
	if err != nil {
		log.WithFields(logrus.Fields{
			"page":  name,
			"error": err,
		}).Errorln("error commiting to git repo,")
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}

	env.authState.SetFlash(name+" page successfully deleted.", w)
	http.Redirect(w, r, "/", http.StatusSeeOther)

}

type result struct {
	Name   string
	Result string
}

type searchPage struct {
	page
	Results []result
}

func (env *wikiEnv) searchHandler(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "*")

	// If this is a POST request, and searchwiki form is not blank,
	//  redirect to /search/$(searchform)
	if r.Method == "POST" {
		r.ParseForm()
		if r.PostFormValue("searchwiki") != "" {
			http.Redirect(w, r, "/search/"+r.PostFormValue("searchwiki"), http.StatusSeeOther)
			return
			//name = r.PostFormValue("searchwiki")
		}
	}

	p := make(chan page, 1)
	go env.loadPage(r, p)

	user := env.authState.GetUser(r)

	var fileList string

	theCache := env.loadCache()

	for _, v := range theCache.Cache {
		if env.authState.IsLoggedIn(r) {
			if v.Permission == privatePermission {
				//log.Println("priv", v.Filename)
				fileList = fileList + " " + `"` + v.Filename + `"`
			}
			if user.IsAdmin() {
				if v.Permission == adminPermission {
					//log.Println("admin", v.Filename)
					fileList = fileList + " " + `"` + v.Filename + `"`
				}
			}
		}

		if v.Permission == publicPermission {
			//log.Println("pubic", v.Filename)
			fileList = fileList + " " + `"` + v.Filename + `"`
		}
	}

	//log.Println(fileList)

	results := env.gitSearch(name, strings.TrimSpace(fileList))

	s := &searchPage{
		page:    <-p,
		Results: results,
	}
	renderTemplate(r.Context(), env, w, "search_results.tmpl", s)
}

func (env *wikiEnv) loginPageHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "loginPageHandler")

	title := "login"
	p := make(chan page, 1)
	go env.loadPage(r, p)

	gp := &genPage{
		<-p,
		title,
	}
	renderTemplate(r.Context(), env, w, "login.tmpl", gp)
}

func (env *wikiEnv) signupPageHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "signupPageHandler")

	title := "signup"
	p := make(chan page, 1)
	go env.loadPage(r, p)

	anyUsers := env.authState.AnyUsers()

	gp := struct {
		page
		Title    string
		AnyUsers bool
	}{
		<-p,
		title,
		anyUsers,
	}
	renderTemplate(r.Context(), env, w, "signup.tmpl", gp)

}

func (env *wikiEnv) adminUsersHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "adminUsersHandler")

	title := "admin-users"
	p := make(chan page, 1)
	go env.loadPage(r, p)

	userlist, err := env.authState.Userlist()
	if err != nil {
		panic(err)
	}

	data := struct {
		page
		Title string
		Users []string
	}{
		<-p,
		title,
		userlist,
	}
	/*gp := &genPage{
		p,
		title,
	}*/
	renderTemplate(r.Context(), env, w, "admin_users.tmpl", data)

}

func (env *wikiEnv) adminUserHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "adminUserHandler")

	title := "admin-user"
	p := make(chan page, 1)
	go env.loadPage(r, p)

	userlist, err := env.authState.Userlist()
	if err != nil {
		panic(err)
	}

	//ctx := r.Context()
	selectedUser := chi.URLParam(r, "username")

	data := struct {
		page
		Title string
		Users []string
		User  string
	}{
		<-p,
		title,
		userlist,
		selectedUser,
	}
	/*gp := &genPage{
		p,
		title,
	}*/
	renderTemplate(r.Context(), env, w, "admin_user.tmpl", data)
}

// Function to take a <select><option> value and redirect to a URL based on it
func adminUserPostHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	selectedUser := r.FormValue("user")
	http.Redirect(w, r, "/admin/user/"+selectedUser, http.StatusSeeOther)
}

// Function to take a <select><option> value and redirect to a URL based on it
func (env *wikiEnv) adminGeneratePostHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	selectedRole := r.FormValue("role")
	registerToken := env.authState.GenerateRegisterToken(selectedRole)

	/*
		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		data := struct {
			RegistrationToken string
			SelectedRole      string
		}{
			registerToken,
			selectedRole,
		}

		newTokenTmpl := `
		<html>
			<head>
				<title>Registration Token</title>
				<meta http-equiv="Content-Type" content="text/html; charset=utf-8">
			</head>
			<body>
				<p>Registration token: {{ .RegistrationToken }}</p>
				<p>Role: {{ .SelectedRole }}</p>
			</body>
		</html>`

		tpl := template.Must(template.New("NewTokenPage").Parse(newTokenTmpl))
		tpl.Execute(w, data)
	*/
	env.authState.SetFlash("Token:"+registerToken+" |Role: "+selectedRole, w)
	http.Redirect(w, r, r.Referer(), http.StatusSeeOther)
}

func (env *wikiEnv) adminMainHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "adminMainHandler")

	title := "admin-main"
	p := make(chan page, 1)
	go env.loadPage(r, p)

	data := struct {
		page
		Title     string
		GitSha1   string
		BuildDate string
	}{
		<-p,
		title,
		sha1ver,
		buildTime,
	}

	renderTemplate(r.Context(), env, w, "admin_main.tmpl", data)
}

func (env *wikiEnv) gitCheckinHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "gitCheckinHandler")

	title := "Git Checkin"
	p := make(chan page, 1)
	go env.loadPage(r, p)

	var s string

	if r.URL.Query().Get("file") != "" {
		file := r.URL.Query().Get("file")
		s = file
	} else {
		err := env.gitIsClean()
		s = err.Error()
		/*
			if err != nil && err != ErrGitDirty {
				panic(err)
			}
		*/
		//owithnewlines = bytes.Replace(o, []byte{0}, []byte(" <br>"), -1)
	}

	gp := &gitPage{
		<-p,
		title,
		s,
		env.cfg.RemoteGitRepo,
	}
	renderTemplate(r.Context(), env, w, "git_checkin.tmpl", gp)
}

func (env *wikiEnv) gitCheckinPostHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "gitCheckinPostHandler")

	var path string

	if r.URL.Query().Get("file") != "" {
		//file := r.URL.Query().Get("file")
		//log.Println(action)
		path = r.URL.Query().Get("file")
	} else {
		path = "."
	}

	err := env.gitAddFilepath(path)
	if err != nil {
		panic(err)
	}
	err = env.gitCommitEmpty()
	if err != nil {
		panic(err)
	}
	if path != "." {
		http.Redirect(w, r, "/"+path, http.StatusSeeOther)
	} else {
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}

}

func (env *wikiEnv) gitPushPostHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "gitPushPostHandler")

	err := env.gitPush()
	if err != nil {
		panic(err)
	}

	http.Redirect(w, r, r.Referer(), http.StatusSeeOther)

}

func (env *wikiEnv) gitPullPostHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "gitPullPostHandler")

	err := env.gitPull()
	if err != nil {
		panic(err)
	}

	http.Redirect(w, r, r.Referer(), http.StatusSeeOther)

}

func (env *wikiEnv) adminGitHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "adminGitHandler")

	title := "Git Management"
	p := make(chan page, 1)
	go env.loadPage(r, p)

	//var owithnewlines []byte

	err := env.gitIsClean()
	if err == nil {
		err = errors.New("Git repo is clean")
	}

	/*
		if err != nil && err != ErrGitDirty {
			panic(err)
		}

		owithnewlines = bytes.Replace(o, []byte{0}, []byte(" <br>"), -1)
	*/

	gp := &gitPage{
		<-p,
		title,
		err.Error(),
		env.cfg.RemoteGitRepo,
	}
	renderTemplate(r.Context(), env, w, "admin_git.tmpl", gp)
}

type tagMapPage struct {
	page
	TagKeys map[string][]string
}

func (env *wikiEnv) tagMapHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "tagMapHandler")

	p := make(chan page, 1)
	go env.loadPage(r, p)

	list := env.tags.GetAll()

	tagpage := &tagMapPage{
		page:    <-p,
		TagKeys: list,
	}

	renderTemplate(r.Context(), env, w, "tag_list.tmpl", tagpage)
}

type tagPage struct {
	page
	TagName string
	Results []string
}

func (env *wikiEnv) tagHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "tagHandler")

	name := chi.URLParam(r, "*")

	p := make(chan page, 1)
	go env.loadPage(r, p)

	results := env.tags.GetOne(name)

	tagpage := &tagPage{
		page:    <-p,
		TagName: name,
		Results: results,
	}
	renderTemplate(r.Context(), env, w, "tag_view.tmpl", tagpage)
}

func (env *wikiEnv) createWiki(w http.ResponseWriter, r *http.Request, name string) {

	w.WriteHeader(404)
	//title := "Create " + name + "?"
	p := make(chan page, 1)
	go env.loadPage(r, p)

	wp := &wikiPage{
		page: <-p,
		Wiki: wiki{
			Title:    name,
			Filename: name,
			Frontmatter: frontmatter{
				Title: name,
			},
		},
	}
	renderTemplate(r.Context(), env, w, "wiki_create.tmpl", wp)
	return

}

func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	// A very simple health check.
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")

	// In the future we could report back on the status of our DB, or our cache
	// (e.g. Redis) by performing a simple PING, and include them in the response.
	io.WriteString(w, `{"alive": true}`)
}

func (env *wikiEnv) editHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "editHandler")
	name := chi.URLParam(r, "*")

	p := env.loadWikiPage(r, name)
	renderTemplate(r.Context(), env, w, "wiki_edit.tmpl", p)
}

func (env *wikiEnv) saveHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "saveHandler")

	name := chi.URLParam(r, "*")

	err := r.ParseForm()
	if err != nil {
		log.WithFields(logrus.Fields{
			"page":  name,
			"error": err,
		}).Errorln("error parsing request form")
		http.Error(w, "error parsing form. check logs for more information", http.StatusInternalServerError)
		return
	}

	content := r.FormValue("editor")

	/*
		// Strip out CRLF here,
		// as I cannot figure out if it's the browser or what inserting them...
		if strings.Contains(content, "\r\n") {
			log.Println("crlf detected in saveHandler; replacing with just newlines.")
			content = strings.Replace(content, "\r\n", "\n", -1)
			//log.Println(strings.Contains(content, "\r\n"))
		}
	*/

	// Check for and install required YAML frontmatter
	title := r.FormValue("title")
	// This is the separate input that tagdog.js throws new tags into
	tags := r.FormValue("tags_all")
	favorite := r.FormValue("favorite")
	permission := r.FormValue("permission")

	favoritebool := false
	if favorite == "on" {
		favoritebool = true
	}

	if title == "" {
		title = name
	}

	var tagsA []string
	if tags != "" {
		tagsA = strings.Split(tags, ",")
	}

	fm := frontmatter{
		Title:      title,
		Tags:       tagsA,
		Favorite:   favoritebool,
		Permission: permission,
	}

	thewiki := &wiki{
		Title:       title,
		Filename:    name,
		Frontmatter: fm,
		Content:     []byte(content),
	}

	err = thewiki.save(env)
	if err != nil {
		log.WithFields(logrus.Fields{
			"page":  name,
			"error": err,
		}).Errorln("error saving wiki page")
		http.Error(w, "error saving wiki page. check logs for more information", http.StatusInternalServerError)
		return
	}

	env.authState.SetFlash("Wiki page successfully saved.", w)
	http.Redirect(w, r, "/"+name, http.StatusSeeOther)
	log.Println(name + " page saved!")
}

func (env *wikiEnv) indexHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "indexHandler")
	if !env.authState.AnyUsers() {
		log.Println("Need to signup...")
		env.authState.SetFlash("Welcome! Sign up to start creating and editing pages.", w)
		http.Redirect(w, r, "/signup", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/index", http.StatusSeeOther)
	//viewHandler(w, r, "index")
}

func (env *wikiEnv) viewHandler(w http.ResponseWriter, r *http.Request) {
	defer httputils.TimeTrack(time.Now(), "viewHandler")

	name := chi.URLParam(r, "*")
	/*
		nameStat, err := os.Stat(filepath.Join(dataDir, "wikidata", name))
		if err != nil {
			log.Println("viewHandler error reading", name, err)
		}
		if err == nil {
			if nameStat.IsDir() {
				// Check if name/index exists, and if it does, serve it
				_, err := os.Stat(filepath.Join(dataDir, "wikidata", name, "index"))
				if err == nil {
					http.Redirect(w, r, "/"+filepath.Join(name, "index"), http.StatusFound)
					return
				}
				if os.IsNotExist(err) {
					// TODO: List directory
					log.Println("TODO: List directory")
				}
			}
		}
	*/

	wikiExists := wikiExistsFromContext(r.Context())
	if !wikiExists {
		httputils.Debugln("wikiExists false: No such file...creating one.")
		//http.Redirect(w, r, "/edit/"+name, http.StatusTemporaryRedirect)
		env.createWiki(w, r, name)
		return
	}

	// If this is a commit, pass along the SHA1 to that function
	if r.URL.Query().Get("commit") != "" {
		// Only allow logged in users to view past pages, in case information had to be redacted on a now-public page
		if env.authState.IsLoggedIn(r) {
			commit := r.URL.Query().Get("commit")
			//utils.Debugln(r.URL.Query().Get("commit"))
			env.viewCommitHandler(w, r, commit, name)
			return
		}
		mitigateWiki(true, env, r, w)
		return
	}

	if !isWiki(filepath.Join(env.cfg.WikiDir, name)) {
		http.ServeFile(w, r, filepath.Join(env.cfg.WikiDir, name))
		return
	}

	// Get Wiki
	p := env.loadWikiPage(r, name)

	// Build a list of filenames to be fed to closestmatch, for similarity matching
	var filelist []string
	user := env.authState.GetUser(r)

	theCache := env.loadCache()

	// TODO: Replace this with a call to listDir() somehow
	for _, v := range theCache.Cache {
		if v.Permission == publicPermission {
			//log.Println("pubic", v.Filename)
			filelist = append(filelist, v.Filename)
		}
		if env.authState.IsLoggedIn(r) {
			if v.Permission == privatePermission {
				//log.Println("priv", v.Filename)
				filelist = append(filelist, v.Filename)
			}
		}
		if user.IsAdmin() {
			if v.Permission == adminPermission {
				//log.Println("admin", v.Filename)
				filelist = append(filelist, v.Filename)
			}
		}
	}

	// Check for similar filenames
	/*
		var similarPages []string
		for _, match := range fuzzy.Find(name, filelist) {
			similarPages = append(similarPages, match.Str)
		}
	*/

	similarPages := fuzzy2.FindFold(name, filelist)
	p.SimilarPages = similarPages

	renderTemplate(r.Context(), env, w, "wiki_view.tmpl", p)
	return

	/*
		var html template.HTML
		if strings.Contains(fileType, "image") {
			html = template.HTML(`<img src="/` + name + `">`)
		}
		p := loadPage(env, r)
		data := struct {
			*page
			Title   string
			TheHTML template.HTML
		}{
			p,
			name,
			html,
		}
		renderTemplate(r.Context(), env, w, "file_view.tmpl", data)
	*/
}

type historyPage struct {
	page
	Wiki        wiki
	Filename    string
	FileHistory []commitLog
}

func (env *wikiEnv) historyHandler(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "*")

	if !wikiExistsFromContext(r.Context()) {
		http.Redirect(w, r, "/"+name, http.StatusSeeOther)
		return
	}

	wikip := env.loadWikiPage(r, name)

	history, err := env.gitGetFileLog(name)
	if err != nil {
		panic(err)
	}
	hp := &historyPage{
		wikip.page,
		wikip.Wiki,
		name,
		history,
	}
	renderTemplate(r.Context(), env, w, "wiki_history.tmpl", hp)
}

// Need to get content of the file at specified commit
// > git show [commit sha1]:[filename]
// As well as the date
// > git log -1 --format=%at [commit sha1]
// TODO: need to find a way to detect sha1s
type commitPage struct {
	page
	Wiki     wiki
	Commit   string
	Rendered string
	Diff     string
}

func (env *wikiEnv) viewCommitHandler(w http.ResponseWriter, r *http.Request, commit, name string) {
	var fm frontmatter
	var pageContent string

	//commit := vars["commit"]

	p := make(chan page, 1)
	go env.loadPage(r, p)

	body, err := env.gitGetFileCommit(name, commit)
	if err != nil {
		panic(err)
	}
	ctime, err := env.gitGetCtime(name)
	if err != nil && err != errNotInGit {
		panic(err)
	}
	mtime, err := env.gitGetFileCommitMtime(commit)
	if err != nil {
		panic(err)
	}
	diff, err := env.gitGetFileCommitDiff(name, commit)
	if err != nil {
		panic(err)
	}

	// Read YAML frontmatter into fm
	reader := bytes.NewReader(body)
	fm, content := readWikiPage(reader)
	if err != nil {
		panic(err)
	}

	// Render remaining content after frontmatter
	md := markdownRender(content)
	//md := commonmarkRender(content)

	pagetitle := setPageTitle(fm.Title, name)

	diffstring := string(diff)

	pageContent = md

	cp := &commitPage{
		page: <-p,
		Wiki: wiki{
			Title:       pagetitle,
			Filename:    name,
			Frontmatter: fm,
			Content:     content,
			CreateTime:  ctime,
			ModTime:     mtime,
		},
		Commit:   commit,
		Rendered: pageContent,
		Diff:     diffstring,
	}

	renderTemplate(r.Context(), env, w, "wiki_commit.tmpl", cp)

}

type recent struct {
	Date      int64
	Commit    string
	Filenames []string
}

type recentsPage struct {
	page
	Recents []recent
}

// TODO: Fix this
func (env *wikiEnv) recentHandler(w http.ResponseWriter, r *http.Request) {

	p := make(chan page, 1)
	go env.loadPage(r, p)

	gh, err := env.gitHistory()
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Errorln("error getting git history")
		http.Error(w, "Unable to fetch git history", 500)
		return
	}

	/*
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(200)
	*/
	var split []string
	var split2 []string
	var recents []recent

	for _, v := range gh {
		split = strings.Split(strings.TrimSpace(v), " ")
		date, err := strconv.ParseInt(split[0], 0, 64)
		if err != nil {
			panic(err)
		}

		// If there is a filename (initial one will not have it)...
		split2 = strings.Split(split[1], "\n")
		if len(split2) >= 2 {

			r := recent{
				Date:      date,
				Commit:    split2[0],
				Filenames: strings.Split(split2[1], "\n"),
			}
			//w.Write([]byte(v + "<br>"))
			recents = append(recents, r)
		}
	}

	s := recentsPage{
		page:    <-p,
		Recents: recents,
	}
	renderTemplate(r.Context(), env, w, "recents.tmpl", s)

}

type listPage struct {
	page
	Wikis []gitDirList
}

func (env *wikiEnv) listHandler(w http.ResponseWriter, r *http.Request) {

	p := make(chan page, 1)
	go env.loadPage(r, p)

	var list []gitDirList

	user := env.authState.GetUser(r)

	theCache := env.loadCache()

	for _, v := range theCache.Cache {
		if v.Permission == publicPermission {
			//log.Println("pubic", v.Filename)
			list = append(list, v)
		}
		if env.authState.IsLoggedIn(r) {
			if v.Permission == privatePermission {
				//log.Println("priv", v.Filename)
				list = append(list, v)
			}
		}
		if user.IsAdmin() {
			if v.Permission == adminPermission {
				//log.Println("admin", v.Filename)
				list = append(list, v)
			}
		}
	}

	l := listPage{
		page:  <-p,
		Wikis: list,
	}
	renderTemplate(r.Context(), env, w, "list.tmpl", l)
}

func (env *wikiEnv) securityCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Security check; ensure we are not serving any files from wikidata/.git
		// If so, toss them to the index, no hints given
		if strings.Contains(r.URL.EscapedPath(), ".git") {
			http.Error(w, "unable to access that", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (env *wikiEnv) wikiMiddle(next http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "*")
		user := env.authState.GetUser(r)

		pageExists, relErr := env.checkName(&name)
		fullfilename := filepath.Join(env.cfg.WikiDir, name)

		//wikiDir := filepath.Join(dataDir, "wikidata")

		if relErr != nil {
			if relErr == errBaseNotDir {
				http.Error(w, "Cannot create subdir of a file.", 500)
				return
			}

			// If we have a directory, do some stuff:
			if relErr == errIsDir {
				// If we have a ?list query string, just list it
				if r.URL.Query().Get("list") != "" {
					env.listDirHandler(name, w, r)
					return
				}

				// If the given name is a directory, and URL is just /name/, check for /name/index
				//    If name/index exists, redirect to it
				if r.URL.Path[:len("/"+name)] == "/"+name {
					// Check if name/index exists, and if it does, serve it
					_, err := os.Stat(filepath.Join(env.cfg.WikiDir, name, "index"))
					if err == nil {
						http.Redirect(w, r, "/"+path.Join(name, "index"), http.StatusFound)
						return
					}
				}

				// Otherwise fallback to just listing it
				env.listDirHandler(name, w, r)
				return
			}

			httpErrorHandler(w, r, relErr)
			return
		}

		nameCtx := newNameContext(r.Context(), name)
		ctx := newWikiExistsContext(nameCtx, pageExists)
		r = r.WithContext(ctx)

		if wikiRejected(fullfilename, pageExists, user.IsAdmin(), env.authState.IsLoggedIn(r)) {
			mitigateWiki(true, env, r, w)
		} else {
			next.ServeHTTP(w, r)
			return
		}

	})
}

/*
// wikiHandler wraps around all wiki page handlers
// Currently it retrieves the page name from params, checks for file existence, and checks for private pages
func wikiHandler(fn wHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Here we will extract the page title from the Request,
		// and call the provided handler 'fn'

		params := httptreemux.ContextParams(r.Context())
		name := params["name"]
		username, isAdmin := auth.GetUsername(r.Context())

		// Check if file exists before doing anything else
		name, feErr := checkName(name)
		fullname := filepath.Join(viper.GetString("WikiDir"), name)

		isWikiPage(fullname)

		if name != "" && feErr == errNoFile {
			//log.Println(r.URL.RequestURI())

			// If editing or saving, bypass create page
			if r.URL.RequestURI() == "/edit/"+name {
				fn(w, r, name)
				return
			}
			if r.URL.RequestURI() == "/save/"+name {
				fn(w, r, name)
				return
			}
			createWiki(w, r, name)
			return
		} else if feErr != nil {
			httpErrorHandler(w, r, feErr)
			return
		}

		// Detect filetypes

		//	filetype := checkFiletype(fullname)
		//	if filetype != "text/plain; charset=utf-8" {

		//		http.ServeFile(w, r, fullname)
		//	}


		// Read YAML frontmatter into fm
		// If err, just return, as file should not contain frontmatter
		f, err := os.Open(fullname)
		checkErr("wikiHandler()/Open", err)
		defer f.Close()

		fm, fmberr := readFront(f)
		checkErr("wikiHandler()/readFront", fmberr)

		// If user is logged in, check if wiki git repo is clean, then continue
		//if username != "" {
		if auth.IsLoggedIn(r.Context()) {
			err := gitIsClean()
			if err != nil {
				log.Println(err)
				auth.SetFlash(err.Error(), w, r)
				http.Redirect(w, r, "/admin/git", http.StatusSeeOther)
			}
			fn(w, r, name)
			return
		}

		// If this is a public page, just serve it
		if fm.Public {
			fn(w, r, name)
			return
		}
		// If this is an admin page, check if user is admin before serving
		if fm.Admin && !isAdmin {
			log.Println(username + " attempting to access restricted URL.")
			auth.SetFlash("Sorry, you are not allowed to see that.", w, r)
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		// If not logged in, mitigate, as the page is presumed private
		if !auth.IsLoggedIn(r.Context()) {
			rurl := r.URL.String()
			httputils.Debugln("wikiHandler mitigating: " + r.Host + rurl)
			//w.Write([]byte("OMG"))

			// Detect if we're in an endless loop, if so, just panic
			if strings.HasPrefix(rurl, "login?url=/login") {
				panic("AuthMiddle is in an endless redirect loop")
			}
			auth.SetFlash("Please login to view that page.", w, r)
			http.Redirect(w, r, "http://"+r.Host+"/login"+"?url="+rurl, http.StatusSeeOther)
			return
		}

	}
}
*/

func (env *wikiEnv) listDirHandler(dir string, w http.ResponseWriter, r *http.Request) {

	p := make(chan page, 1)
	go env.loadPage(r, p)

	var list []gitDirList

	user := env.authState.GetUser(r)

	theCache := env.loadCache()

	for _, v := range theCache.Cache {
		if filepath.Dir(v.Filename) == dir {
			if v.Permission == publicPermission {
				//log.Println("pubic", v.Filename)
				list = append(list, v)
			}
			if env.authState.IsLoggedIn(r) {
				if v.Permission == privatePermission {
					//log.Println("priv", v.Filename)
					list = append(list, v)
				}
			}
			if user.IsAdmin() {
				if v.Permission == adminPermission {
					//log.Println("admin", v.Filename)
					list = append(list, v)
				}
			}
		}
	}

	l := listPage{
		page:  <-p,
		Wikis: list,
	}
	renderTemplate(r.Context(), env, w, "list.tmpl", l)
}

func (env *wikiEnv) LoginPostHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
	case "POST":
		// Handle login POST request
		username := r.FormValue("username")
		password := r.FormValue("password")

		// Login authentication
		if env.authState.Auth(username, password) {
			env.authState.Login(username, w)
			env.authState.SetFlash("User '"+username+"' successfully logged in.", w)
			// Check if we have a redirect URL in the cookie, if so redirect to it
			redirURL := env.authState.GetRedirect(r, w)
			if redirURL != "" {
				http.Redirect(w, r, redirURL, http.StatusSeeOther)
				return
			}
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		env.authState.SetFlash("User '"+username+"' failed to login. Please check your credentials and try again.", w)
		http.Redirect(w, r, env.authState.Cfg.LoginPath, http.StatusSeeOther)
		return
	case "PUT":
		// Update an existing record.
	case "DELETE":
		// Remove the record.
	default:
		// Give an error message.
	}
}

func (e *wikiEnv) UserSignupTokenPostHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
	case "POST":
		username := r.FormValue("username")
		password := r.FormValue("password")
		givenToken := r.FormValue("register_key")

		isValid, userRole := e.authState.ValidateRegisterToken(givenToken)

		if isValid {

			// Delete the token so it cannot be reused if the token is not blank
			// The first user can signup without a token and is granted admin rights
			if givenToken != "" {
				e.authState.DeleteRegisterToken(givenToken)
			}

			if userRole == auth.RoleAdmin {
				err := e.authState.NewAdmin(username, password)
				if err != nil {
					log.Println("Error adding admin:", err)
					e.authState.SetFlash("Error adding user. Check logs.", w)
					http.Redirect(w, r, r.Referer(), http.StatusInternalServerError)
					return
				}
			} else if userRole == auth.RoleUser {
				err := e.authState.NewUser(username, password)
				if err != nil {
					log.Println("Error adding user:", err)
					e.authState.SetFlash("Error adding user. Check logs.", w)
					http.Redirect(w, r, r.Referer(), http.StatusInternalServerError)
					return
				}
			}

			// Login the recently added user
			if e.authState.Auth(username, password) {
				e.authState.Login(username, w)
			}

			e.authState.SetFlash("Successfully added '"+username+"' user.", w)
			http.Redirect(w, r, "/", http.StatusSeeOther)
		} else {
			e.authState.SetFlash("Registration token is invalid.", w)
			http.Redirect(w, r, "/", http.StatusInternalServerError)
		}

	case "PUT":
		// Update an existing record.
	case "DELETE":
		// Remove the record.
	default:
		// Give an error message.
	}
}
