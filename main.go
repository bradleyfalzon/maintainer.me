package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"golang.org/x/oauth2"
	ghoauth "golang.org/x/oauth2/github"

	"github.com/Sirupsen/logrus"
	"github.com/alexedwards/scs/engine/mysqlstore"
	"github.com/alexedwards/scs/session"
	"github.com/bradleyfalzon/maintainer.me/db"
	"github.com/bradleyfalzon/maintainer.me/events"
	"github.com/bradleyfalzon/maintainer.me/notifier"
	"github.com/bradleyfalzon/maintainer.me/web"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	_ "github.com/go-sql-driver/mysql"
	"github.com/gregjones/httpcache"
	"github.com/gregjones/httpcache/diskcache"
	"github.com/joho/godotenv"
	"github.com/pkg/errors"
	migrate "github.com/rubenv/sql-migrate"
)

func main() {
	_ = godotenv.Load() // Ignore errors as .env is optional

	// Logger
	logger := logrus.New()
	logger.Level = logrus.DebugLevel
	logger.Info("Starting")

	if err := run(logger); err != nil {
		logger.WithError(err).Fatalf("run failed")
	}
	logger.Info("Terminating")
}

func run(logger *logrus.Logger) error {

	ctx := context.Background()

	// Notifiers
	notifier := &notifier.Writer{Writer: os.Stdout}

	// DB
	dsn := fmt.Sprintf(`%s:%s@tcp(%s:%s)/%s?charset=utf8&collation=utf8_unicode_ci&timeout=6s&time_zone='%%2B00:00'&parseTime=true`,
		os.Getenv("DB_USERNAME"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_HOST"), os.Getenv("DB_PORT"), os.Getenv("DB_DATABASE"),
	)
	dbConn, err := sql.Open(os.Getenv("DB_DRIVER"), dsn)
	if err != nil {
		return errors.Wrap(err, "error setting up DB")
	}
	if err := dbConn.Ping(); err != nil {
		return errors.Wrapf(err, "error pinging %q db name: %q, username: %q, host: %q, port: %q",
			os.Getenv("DB_DRIVER"), os.Getenv("DB_DATABASE"), os.Getenv("DB_USERNAME"), os.Getenv("DB_HOST"), os.Getenv("DB_PORT"),
		)
	}
	db := db.NewSQLDB(os.Getenv("DB_DRIVER"), dbConn)

	// Migrations
	// TODO down direction
	migrations := &migrate.FileMigrationSource{Dir: "migrations"}
	n, err := migrate.ExecMax(dbConn, os.Getenv("DB_DRIVER"), migrations, migrate.Up, 0)
	if err != nil {
		return errors.Wrap(err, "error running SQL migrations")
	}
	logger.Debugf("Executed %v migrations", n)

	// HTTP Cache
	cache := httpcache.NewTransport(diskcache.New("/tmp"))

	// Poller
	poller := events.NewPoller(logger.WithField("thread", "poller"), db, notifier, cache)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		err := poller.Poll(ctx, 60*time.Second)
		if err != nil {
			logger.WithError(err).Fatal("Poller exited with an error")
		}
		logger.Info("Poller exited")
		wg.Done()
	}()

	// GitHub OAuth Client
	switch {
	case os.Getenv("GITHUB_OAUTH_CLIENT_ID") == "":
		return errors.New("environment GITHUB_OAUTH_CLIENT_ID not set")
	case os.Getenv("GITHUB_OAUTH_CLIENT_SECRET") == "":
		return errors.New("environment GITHUB_OAUTH_CLIENT_SECRET not set")
	}

	ghoauthConfig := &oauth2.Config{
		ClientID:     os.Getenv("GITHUB_OAUTH_CLIENT_ID"),
		ClientSecret: os.Getenv("GITHUB_OAUTH_CLIENT_SECRET"),
		Endpoint:     ghoauth.Endpoint,
		Scopes:       []string{"user:email"},
	}

	// Session Manager
	sessionEngine := mysqlstore.New(dbConn, 5*time.Minute)
	defer sessionEngine.StopCleanup()

	webLogger := logger.WithField("thread", "web")
	sessionManager := session.Manage(
		sessionEngine,
		session.Lifetime(365*24*time.Hour),
		session.Persist(true),
		session.Secure(true),
		session.HttpOnly(true),
		session.ErrorFunc(func(w http.ResponseWriter, r *http.Request, err error) {
			webLogger.WithError(err).Error("session handling error")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}),
	)

	// Web
	web, err := web.NewWeb(webLogger, db, cache, ghoauthConfig)
	if err != nil {
		return errors.WithMessage(err, "could not instantiate web")
	}

	router := chi.NewRouter()
	router.Use(sessionManager)

	router.Use(middleware.DefaultCompress)
	router.Use(middleware.Recoverer)
	router.Use(middleware.NoCache)

	// TODO remove Handler from name
	// TODO split web into web and console

	router.Get("/", web.HomeHandler)
	router.Get("/login", web.LoginHandler)
	router.Get("/login/callback", web.LoginCallbackHandler)
	//router.Get("/logout", web.LogoutHandler)
	router.Route("/console", func(router chi.Router) {
		router.Use(web.RequireLogin)
		router.Get("/", web.ConsoleHomeHandler)
		router.Get("/filters", web.ConsoleFiltersHandler)
		router.Post("/filters", web.ConsoleFiltersUpdateHandler)
		router.Get("/filters/{filterID}", web.ConsoleFilterHandler)
		router.Post("/filters/{filterID}", web.ConsoleFilterUpdateHandler)
		router.Delete("/conditions/{conditionID}", web.ConsoleConditionDeleteHandler) // doesn't redirect
		router.Post("/conditions/", web.ConsoleConditionCreateHandler)                // redirects /shrug
		router.Get("/events", web.ConsoleEventsHandler)
	})

	// HTTP Server
	srv := &http.Server{
		Addr:    ":3001",
		Handler: router,
	}
	wg.Add(1)
	go func() {
		logger.Infof("Listenting on %q", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.WithError(err).Fatal("ListenAndServe exited with an error")
		}
		logger.Info("ListenAndServe exited")
		wg.Done()
	}()

	go func() {
		<-ctx.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		srv.Shutdown(ctx)
		cancel()
	}()

	wg.Wait()
	return nil
}
