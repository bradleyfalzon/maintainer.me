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
	"github.com/bradleyfalzon/maintainer.me/db"
	"github.com/bradleyfalzon/maintainer.me/events"
	"github.com/bradleyfalzon/maintainer.me/notifier"
	"github.com/bradleyfalzon/maintainer.me/web"
	_ "github.com/go-sql-driver/mysql"
	"github.com/gregjones/httpcache"
	"github.com/gregjones/httpcache/diskcache"
	"github.com/joho/godotenv"
	"github.com/pkg/errors"
	"github.com/pressly/chi"
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

	// Web
	router := chi.NewRouter()
	err = web.NewWeb(logger.WithField("thread", "web"), db, cache, router, sessionEngine, ghoauthConfig)
	if err != nil {
		return errors.WithMessage(err, "could not instantiate web")
	}

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
