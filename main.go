package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"golang.org/x/oauth2"
	ghoauth "golang.org/x/oauth2/github"

	"github.com/bradleyfalzon/maintainer.me/db"
	"github.com/bradleyfalzon/maintainer.me/events"
	"github.com/bradleyfalzon/maintainer.me/notifier"
	"github.com/bradleyfalzon/maintainer.me/web"
	_ "github.com/go-sql-driver/mysql"
	"github.com/gregjones/httpcache"
	"github.com/gregjones/httpcache/diskcache"
	"github.com/joho/godotenv"
	"github.com/pressly/chi"
	migrate "github.com/rubenv/sql-migrate"
)

func main() {
	fmt.Println("Starting...")

	_ = godotenv.Load() // Ignore errors as .env is optional

	if err := run(); err != nil {
		log.Fatal(err)
	}
	log.Println("Terminating")
}

func run() error {

	ctx := context.Background()

	notifier := &notifier.Writer{Writer: os.Stdout}

	dsn := fmt.Sprintf(`%s:%s@tcp(%s:%s)/%s?charset=utf8&collation=utf8_unicode_ci&timeout=6s&time_zone='%%2B00:00'&parseTime=true`,
		os.Getenv("DB_USERNAME"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_HOST"), os.Getenv("DB_PORT"), os.Getenv("DB_DATABASE"),
	)
	dbConn, err := sql.Open(os.Getenv("DB_DRIVER"), dsn)
	if err != nil {
		log.Fatal(err)
		//logger.WithError(err).Fatal("Error setting up DB")
	}
	if err := dbConn.Ping(); err != nil {
		log.Fatal(err)
		//logger.WithError(err).Fatalf("Error pinging %q db name: %q, username: %q, host: %q, port: %q",
		//os.Getenv("DB_DRIVER"), os.Getenv("DB_DATABASE"), os.Getenv("DB_USERNAME"), os.Getenv("DB_HOST"), os.Getenv("DB_PORT"),
		//)
	}
	db := db.NewSQLDB(os.Getenv("DB_DRIVER"), dbConn)

	migrations := &migrate.FileMigrationSource{Dir: "migrations"}
	// TODO down direction
	n, err := migrate.ExecMax(dbConn, os.Getenv("DB_DRIVER"), migrations, migrate.Up, 0)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Executed %v migrations", n)

	cache := httpcache.NewTransport(diskcache.New("/tmp"))

	poller := events.NewPoller(db, notifier, cache)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		err := poller.Poll(ctx, 60*time.Second)
		if err != nil {
			log.Println("Poller exited with error:", err)
		}
		log.Println("Poller exited")
		wg.Done()
	}()

	switch {
	case os.Getenv("GITHUB_OAUTH_CLIENT_ID") == "":
		log.Fatal("Environment GITHUB_OAUTH_CLIENT_ID not set")
	case os.Getenv("GITHUB_OAUTH_CLIENT_SECRET") == "":
		log.Fatal("Environment GITHUB_OAUTH_CLIENT_SECRET not set")
	}

	// GitHub OAuth Client
	ghoauthConfig := &oauth2.Config{
		ClientID:     os.Getenv("GITHUB_OAUTH_CLIENT_ID"),
		ClientSecret: os.Getenv("GITHUB_OAUTH_CLIENT_SECRET"),
		Endpoint:     ghoauth.Endpoint,
		Scopes:       []string{"user:email"},
	}

	r := chi.NewRouter()

	err = web.NewWeb(db, cache, r, ghoauthConfig)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:    ":3001",
		Handler: r,
	}
	wg.Add(1)
	go func() {
		log.Println("Listening on", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Println("main: http server error:", err)
		}
		log.Println("Server shut down")
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
