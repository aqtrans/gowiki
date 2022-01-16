package main

import (
	"expvar"
	"net/http"
	"net/http/pprof"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func router(env *wikiEnv) http.Handler {

	// HTTP stuff from here on out
	//s := alice.New(httputils.Timer, httputils.Logger, env.authState.CtxMiddle, env.authState.CSRFProtect(csrfSecure))

	//s := alice.New(env.timer, env.authState.CSRFProtect(env.cfg.CsrfTLS), env.securityCheck)

	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.CleanPath)
	r.Use(env.timer)
	r.Use(env.authState.CSRFProtect(env.cfg.CsrfTLS))
	r.Use(env.securityCheck)

	//r.PanicHandler = errorHandler

	r.Get("/", env.indexHandler)

	r.Get("/tags", env.authState.AuthMiddle(env.tagMapHandler))
	r.Get("/tag/*", env.authState.AuthMiddle(env.tagHandler))

	r.Get("/login", env.loginPageHandler)
	r.Get("/logout", env.authState.LogoutHandler)
	r.Get("/signup", env.signupPageHandler)
	r.Get("/list", env.listHandler)
	r.Get("/search/*", env.searchHandler)
	r.Post("/search", env.searchHandler)
	r.Get("/recent", env.authState.AuthMiddle(env.recentHandler))
	r.Get("/health", healthCheckHandler)

	r.Route("/admin", func(r chi.Router) {
		r.Use(env.authState.AuthAdminMiddleHandler)
		r.Get("/", env.authState.AuthAdminMiddle(env.adminMainHandler))
		r.Get("/git", env.authState.AuthAdminMiddle(env.adminGitHandler))
		r.Post("/git/push", env.authState.AuthAdminMiddle(env.gitPushPostHandler))
		r.Post("/git/checkin", env.authState.AuthAdminMiddle(env.gitCheckinPostHandler))
		r.Post("/git/pull", env.authState.AuthAdminMiddle(env.gitPullPostHandler))
		r.Get("/users", env.authState.AuthAdminMiddle(env.adminUsersHandler))
		r.Post("/user", env.authState.AuthAdminMiddle(adminUserPostHandler))
		r.Get("/user/{username}", env.authState.AuthAdminMiddle(env.adminUserHandler))
		r.Post("/user/{username}", env.authState.AuthAdminMiddle(env.adminUserHandler))
		r.Post("/user/generate", env.authState.AuthAdminMiddle(env.adminGeneratePostHandler))

	})

	r.Route("/auth", func(r chi.Router) {
		r.Post("/login", env.LoginPostHandler)
		r.Post("/logout", env.authState.LogoutHandler)
		r.Get("/logout", env.authState.LogoutHandler)
		r.Post("/signup", env.UserSignupTokenPostHandler)
	})

	r.Post("/gitadd", env.authState.AuthMiddle(env.gitCheckinPostHandler))
	r.Get("/gitadd", env.authState.AuthMiddle(env.gitCheckinHandler))

	r.Post("/md_render", markdownPreview)

	r.Handle("/uploads/*", http.StripPrefix("/uploads/", http.FileServer(http.Dir("uploads"))))

	// Wiki page handlers
	r.Get(`/fav/*`, env.authState.AuthMiddle(env.wikiMiddle(env.setFavoriteHandler)))
	r.Get(`/edit/*`, env.authState.AuthMiddle(env.wikiMiddle(env.editHandler)))
	r.Post(`/save/*`, env.authState.AuthMiddle(env.wikiMiddle(env.saveHandler)))
	r.Get(`/history/*`, env.authState.AuthMiddle(env.wikiMiddle(env.historyHandler)))
	r.Post(`/delete/*`, env.authState.AuthMiddle(env.wikiMiddle(env.deleteHandler)))
	r.Get(`/*`, env.wikiMiddle(env.viewHandler))

	r.Handle("/debug/vars", expvar.Handler())
	r.Get("/debug/pprof/", env.authState.AuthAdminMiddle(http.HandlerFunc(pprof.Index)))
	r.Get("/debug/pprof/cmdline", env.authState.AuthAdminMiddle(http.HandlerFunc(pprof.Cmdline)))
	r.Get("/debug/pprof/profile", env.authState.AuthAdminMiddle(http.HandlerFunc(pprof.Profile)))
	r.Get("/debug/pprof/symbol", env.authState.AuthAdminMiddle(http.HandlerFunc(pprof.Symbol)))
	r.Get("/debug/pprof/trace", env.authState.AuthAdminMiddle(http.HandlerFunc(pprof.Trace)))
	//r.Get("/robots.txt", robots)
	//r.Get("/favicon.ico", faviconICO)
	//r.Get("/favicon.png", faviconPNG)
	r.Handle("/assets/*", http.FileServer(http.FS(assetsfs)))

	return r
}
