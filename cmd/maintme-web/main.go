package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/alexedwards/scs/engine/mysqlstore"
	"github.com/alexedwards/scs/session"
	maintainer "github.com/bradleyfalzon/maintainer.me"
	"github.com/bradleyfalzon/maintainer.me/web"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load() // Ignore errors as .env is optional

	ctx := context.Background()

	m, err := maintainer.NewMaintainer()
	if err != nil {
		log.Fatal(err)
	}

	// Session Manager
	sessionEngine := mysqlstore.New(m.DBConn, 5*time.Minute)
	defer sessionEngine.StopCleanup()

	sessionManager := session.Manage(
		sessionEngine,
		session.Lifetime(365*24*time.Hour),
		session.Persist(true),
		session.Secure(true),
		session.HttpOnly(true),
		session.ErrorFunc(func(w http.ResponseWriter, r *http.Request, err error) {
			m.Logger.WithError(err).Error("session handling error")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}),
	)

	// Web
	public, err := web.NewPublic(m.Logger)
	if err != nil {
		m.Logger.WithError(err).Fatal("Could not instantiate web.Public")
	}

	console, err := web.NewConsole(m.Logger, m.DB, m.Cache, m.GHOAuthConfig)
	if err != nil {
		m.Logger.WithError(err).Fatal("Could not instantiate web.Console")
	}

	router := chi.NewRouter()
	router.Use(sessionManager)

	router.Use(middleware.DefaultCompress)
	router.Use(middleware.Recoverer)
	router.Use(middleware.NoCache)

	router.Get("/", public.Home)
	router.Get("/login", console.Login)
	router.Get("/login/callback", console.LoginCallback)
	//router.Get("/logout", console.Logout)
	router.Route("/console", func(router chi.Router) {
		router.Use(console.RequireLogin)
		router.Get("/", console.Home)
		router.Get("/repos", console.Repos)
		router.Get("/filters", console.Filters)
		router.Post("/filters", console.FiltersUpdate)
		router.Get("/filters/{filterID}", console.Filter)
		router.Post("/filters/{filterID}", console.FilterUpdate)
		router.Delete("/conditions/{conditionID}", console.ConditionDelete)
		router.Post("/conditions/", console.ConditionCreate)
		router.Get("/events", console.Events)
	})

	// HTTP Server
	srv := &http.Server{
		Addr:    ":3001",
		Handler: router,
	}

	go func() {
		<-ctx.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		srv.Shutdown(ctx)
		cancel()
	}()

	m.Logger.Infof("Listenting on %q", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		m.Logger.WithError(err).Fatal("ListenAndServe exited with an error")
	}
	m.Logger.Info("Web exiting")
}
