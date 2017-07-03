package db

import (
	"database/sql"
	"time"

	"github.com/bradleyfalzon/ghfilter"
)

// DB represents a database.
type DB interface {
	// Users returns a list of active users that need are scheduled to be polled.
	Users() ([]User, error)
	// UsersFilters returns all filters for a User ID.
	UsersFilters(userID int) ([]ghfilter.Filter, error)
	// SetUsersNextUpdate
	SetUsersPollResult(userID int, lastCreatedAt time.Time, nextUpdate time.Time) error
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
	db sql.DB
}

var _ DB = &SQLDB{}

func NewSQLDB() *SQLDB {
	return &SQLDB{}
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
				{Type: "IssuesEvent"},
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
