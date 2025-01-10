package main

import (
	"expvar"
	"net/http"
	"net/http/pprof"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/justinas/nosurf"
	chiprometheus "github.com/ppaanngggg/chi-prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func router(env *wikiEnv) http.Handler {

	// HTTP stuff from here on out
	//s := alice.New(httputils.Timer, httputils.Logger, env.authState.CtxMiddle, env.authState.CSRFProtect(csrfSecure))

	//s := alice.New(env.timer, env.authState.CSRFProtect(env.cfg.CsrfTLS), env.securityCheck)

	r := chi.NewRouter()

	r.Use(middleware.Heartbeat("/health"))

	if env.cfg.Prometheus {
		//Prometheus:
		promMiddle := chiprometheus.NewMiddleware("wiki")
		r.Use(promMiddle)
	}

	// Disable logger middleware if testing
	// TODO: wrap this into a Logrus() instance passed through the env
	if !env.testing {
		r.Use(middleware.Logger)
	}

	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.CleanPath)
	r.Use(env.authState.LoadAndSave)

	r.Use(env.timer)
	r.Use(nosurf.NewPure)
	r.Use(env.securityCheck)

	//r.PanicHandler = errorHandler
	r.Handle("/metrics", promhttp.Handler())

	r.Get("/", env.indexHandler)

	r.Get("/tags", env.authState.UsersOnly(env.tagMapHandler))
	r.Get("/tag/*", env.authState.UsersOnly(env.tagHandler))

	r.Get("/login", env.loginPageHandler)
	r.Get("/logout", env.authState.LogoutHandler)
	r.Get("/signup", env.signupPageHandler)
	r.Get("/list", env.listHandler)
	r.Get("/search/*", env.searchHandler)
	r.Post("/search", env.searchHandler)
	r.Get("/recent", env.authState.UsersOnly(env.recentHandler))
	//r.Get("/health", healthCheckHandler)

	r.Route("/admin", func(r chi.Router) {
		r.Use(env.authState.AdminsOnlyH)
		r.Get("/", env.adminMainHandler)
		r.Get("/git", env.adminGitHandler)
		r.Post("/git/push", env.gitPushPostHandler)
		r.Post("/git/checkin", env.gitCheckinPostHandler)
		r.Post("/git/pull", env.gitPullPostHandler)
		r.Get("/users", env.adminUsersHandler)
		r.Post("/user", adminUserPostHandler)
		r.Get("/user/{username}", env.adminUserHandler)
		r.Post("/user/{username}", env.adminUserHandler)

	})

	r.Route("/auth", func(r chi.Router) {
		r.Post("/login", env.LoginPostHandler)
		r.Post("/logout", env.authState.LogoutHandler)
		r.Get("/logout", env.authState.LogoutHandler)
		r.Post("/signup", env.UserSignupTokenPostHandler)
	})

	r.Post("/gitadd", env.authState.UsersOnly(env.gitCheckinPostHandler))
	r.Get("/gitadd", env.authState.UsersOnly(env.gitCheckinHandler))

	r.Post("/md_render", markdownPreview)

	r.Handle("/uploads/*", http.StripPrefix("/uploads/", http.FileServer(http.Dir("uploads"))))

	// Wiki page handlers
	r.Get(`/fav/*`, env.authState.UsersOnly(env.wikiMiddle(env.setFavoriteHandler)))
	r.Get(`/edit/*`, env.authState.UsersOnly(env.wikiMiddle(env.editHandler)))
	r.Post(`/save/*`, env.authState.UsersOnly(env.wikiMiddle(env.saveHandler)))
	r.Get(`/history/*`, env.authState.UsersOnly(env.wikiMiddle(env.historyHandler)))
	r.Post(`/delete/*`, env.authState.UsersOnly(env.wikiMiddle(env.deleteHandler)))

	r.Handle("/debug/vars", expvar.Handler())
	r.Get("/debug/pprof/", http.HandlerFunc(pprof.Index))
	r.Get("/debug/pprof/cmdline", http.HandlerFunc(pprof.Cmdline))
	r.Get("/debug/pprof/profile", http.HandlerFunc(pprof.Profile))
	r.Get("/debug/pprof/symbol", http.HandlerFunc(pprof.Symbol))
	r.Get("/debug/pprof/trace", http.HandlerFunc(pprof.Trace))
	//r.Get("/robots.txt", robots)
	//r.Get("/favicon.ico", faviconICO)
	//r.Get("/favicon.png", faviconPNG)
	r.Handle("/assets/*", http.FileServer(http.FS(assetsfs)))

	r.Get(`/*`, env.wikiMiddle(env.viewHandler))

	return r
}
