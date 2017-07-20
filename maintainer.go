package maintainer

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"golang.org/x/oauth2"
	ghoauth "golang.org/x/oauth2/github"

	"github.com/Sirupsen/logrus"
	"github.com/bradleyfalzon/maintainer.me/db"
	_ "github.com/go-sql-driver/mysql"
	"github.com/gregjones/httpcache"
	"github.com/gregjones/httpcache/diskcache"
	"github.com/pkg/errors"
	migrate "github.com/rubenv/sql-migrate"
)

// Maintainer is a configuration struct for the maintainer.me application.
type Maintainer struct {
	Logger        *logrus.Entry
	DB            db.DB
	DBConn        *sql.DB
	Cache         http.RoundTripper
	GHOAuthConfig *oauth2.Config
}

// NewMaintainer initialises the application and returns a configuration or an
// error.
func NewMaintainer() (*Maintainer, error) {
	// Logging
	log := logrus.New()
	log.Level = logrus.DebugLevel
	logger := log.WithField("cmd", filepath.Base(os.Args[0]))

	// DB
	dsn := fmt.Sprintf(`%s:%s@tcp(%s:%s)/%s?charset=utf8&collation=utf8_unicode_ci&timeout=6s&time_zone='%%2B00:00'&parseTime=true`,
		os.Getenv("DB_USERNAME"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_HOST"), os.Getenv("DB_PORT"), os.Getenv("DB_DATABASE"),
	)
	dbConn, err := sql.Open(os.Getenv("DB_DRIVER"), dsn)
	if err != nil {
		return nil, errors.Wrap(err, "error setting up DB")
	}
	if err := dbConn.Ping(); err != nil {
		return nil, errors.Wrapf(err, "error pinging %q db name: %q, username: %q, host: %q, port: %q",
			os.Getenv("DB_DRIVER"), os.Getenv("DB_DATABASE"), os.Getenv("DB_USERNAME"), os.Getenv("DB_HOST"), os.Getenv("DB_PORT"),
		)
	}
	db := db.NewSQLDB(os.Getenv("DB_DRIVER"), dbConn)

	// Migrations
	// TODO down direction
	migrations := &migrate.FileMigrationSource{Dir: "migrations"}
	n, err := migrate.ExecMax(dbConn, os.Getenv("DB_DRIVER"), migrations, migrate.Up, 0)
	if err != nil {
		return nil, errors.Wrap(err, "error running SQL migrations")
	}
	logger.Debugf("Executed %v migrations", n)

	// HTTP Cache
	cache := httpcache.NewTransport(diskcache.New("/tmp"))

	// GitHub OAuth Client
	switch {
	case os.Getenv("GITHUB_OAUTH_CLIENT_ID") == "":
		return nil, errors.New("environment GITHUB_OAUTH_CLIENT_ID not set")
	case os.Getenv("GITHUB_OAUTH_CLIENT_SECRET") == "":
		return nil, errors.New("environment GITHUB_OAUTH_CLIENT_SECRET not set")
	}

	ghoauthConfig := &oauth2.Config{
		ClientID:     os.Getenv("GITHUB_OAUTH_CLIENT_ID"),
		ClientSecret: os.Getenv("GITHUB_OAUTH_CLIENT_SECRET"),
		Endpoint:     ghoauth.Endpoint,
		Scopes:       []string{"user:email"},
	}

	return &Maintainer{
		DB:            db,
		DBConn:        dbConn,
		Logger:        logger,
		Cache:         cache,
		GHOAuthConfig: ghoauthConfig,
	}, nil
}
