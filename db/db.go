package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"golang.org/x/oauth2"

	"github.com/bradleyfalzon/ghfilter"
	"github.com/jmoiron/sqlx"
	"github.com/pkg/errors"
)

// DB represents a database.
type DB interface {
	// Users returns a list of active users that need are scheduled to be polled.
	Users() ([]User, error)
	// UsersFilters returns all filters for a User ID.
	UsersFilters(userID int) ([]ghfilter.Filter, error)
	// SetUsersNextUpdate
	SetUsersPollResult(userID int, lastCreatedAt time.Time, nextUpdate time.Time) error
	// GitHubLogin logs a user in via GitHub, if a user already exists with the same
	// githubID, the user's accessToken is updated, else a new user is created.
	GitHubLogin(ctx context.Context, githubID int, token *oauth2.Token) (userID int, err error)
}

type User struct {
	ID int

	Email       string
	GitHubUser  string
	GitHubID    int
	GitHubToken []byte // nil if none assigned to user
	//Email       string `db:"email"`
	//GitHubID    int    `db:"github_id"`
	//GitHubToken []byte `db:"github_token"` // nil if none assigned to user

	EventLastCreatedAt time.Time // the latest created at event for the customer
	EventNextPoll      time.Time // time when the next update should occur
	//ListEventsETag          string    // list events etag used for caching - actually, i wouldn't need to store it
}

type SQLDB struct {
	sqlx *sqlx.DB
}

var _ DB = &SQLDB{}

func NewSQLDB(driver string, dbConn *sql.DB) *SQLDB {
	return &SQLDB{
		sqlx: sqlx.NewDb(dbConn, driver),
	}
}

// Users implements the DB interface.
func (db *SQLDB) Users() ([]User, error) {
	// TODO only select users where next poll is before now.
	return []User{
		{
			ID:       1,
			Email:    "",
			GitHubID: 1,
			//GitHubToken: []
			GitHubUser:         "bradleyfalzon",
			EventLastCreatedAt: time.Date(2017, 07, 03, 0, 0, 0, 0, time.UTC),
		},
	}, nil
}

// UsersFilters implements the DB interface.
func (db *SQLDB) UsersFilters(userID int) ([]ghfilter.Filter, error) {
	return []ghfilter.Filter{
		{
			Conditions: []ghfilter.Condition{
				{Type: "PullRequestEvent"},
			},
		},
		{
			Conditions: []ghfilter.Condition{
				{Type: "CommitCommentEvent"},
			},
		},
		{
			Conditions: []ghfilter.Condition{
				{Type: "IssuesEvent", PayloadAction: "opened"},
			},
		},
		{
			Conditions: []ghfilter.Condition{
				{Type: "WatchEvent"},
			},
		},
		{
			Conditions: []ghfilter.Condition{
				{Type: "PullRequestReviewCommentEvent"},
			},
		},
	}, nil
}

// SetUsersPollResult implements the DB interface.
func (db *SQLDB) SetUsersPollResult(userID int, lastCreatedAt, nextPoll time.Time) error {
	// TODO do
	return nil
}

// GitHubLogin implements the DB interface.
func (db *SQLDB) GitHubLogin(ctx context.Context, githubID int, token *oauth2.Token) (int, error) {
	jsonToken, err := json.Marshal(token)
	if err != nil {
		return 0, errors.Wrap(err, "could not marshal oauth2.token")
	}

	// Check if user exists
	var userID int
	err = db.sqlx.QueryRow("SELECT id FROM users WHERE github_id = ?", githubID).Scan(&userID)
	switch {
	case err == sql.ErrNoRows:
		// Add token to new user
		res, err := db.sqlx.Exec("INSERT INTO users (github_id, github_token) VALUES (?, ?)", githubID, jsonToken)
		if err != nil {
			return 0, errors.Wrapf(err, "error inserting new githubID %q", githubID)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return 0, errors.Wrap(err, "error in lastInsertId")
		}
		return int(id), nil
	case err != nil:
		return 0, errors.Wrapf(err, "error getting userID for githubID %q", githubID)
	}

	// Add token to existing user and update email
	_, err = db.sqlx.Exec("UPDATE users SET github_token = ? WHERE id = ?", jsonToken, userID)
	if err != nil {
		return 0, errors.Wrapf(err, "could set userID %q github_token", userID)
	}
	return userID, nil
}
