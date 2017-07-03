package events

import (
	"context"
	"log"
	"time"

	"github.com/bradleyfalzon/maintainer.me/db"
	"github.com/google/go-github/github"
)

// githubBaseURL is the baseURL for github.com/google/go-github/github
// client, variable to easily change in tests.
var githubBaseURL = "https://api.github.com/"

type Poller struct {
	db       db.DB
	notifier Notifier
}

// Notifier sends a notification about a GitHub Event.
type Notifier interface {
	Notify(event Event) error
}

func NewPoller(db db.DB, notifier Notifier) *Poller {
	return &Poller{
		db:       db,
		notifier: notifier,
	}
}

// Poll calls PollUsers every interval. Blocks until context is cancelled.
func (p *Poller) Poll(ctx context.Context, interval time.Duration) error {

	// Find all active users and their filters

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			log.Println("Polling...")
			err := p.PollUsers(ctx)
			if err != nil {
				log.Println("Polling error:", err)
			}
		case <-ctx.Done():
			log.Println("Poller finishing...")
			return ctx.Err()
		}
	}
}

// PollUsers looks for users and checks their events.
func (p *Poller) PollUsers(ctx context.Context) error {
	users, err := p.db.Users()
	if err != nil {
		return err
	}

	for _, user := range users {
		if err := p.PollUser(ctx, user); err != nil {
			return err
		}
	}
	return nil
}

func (p *Poller) PollUser(ctx context.Context, user db.User) error {
	log.Printf("Polling for user: %+v", user)

	if user.EventLastCreatedAt.IsZero() {
		// This is the first poll, mark all events as read from here
		// TODO, this isn't my responsibility, on signup this value
		// should be set correctly.
		user.EventLastCreatedAt = time.Date(2017, 06, 30, 0, 0, 0, 0, time.FixedZone("Australia/Adelaide", 34200))
	}

	// Get user's filters.
	filters, err := p.db.UsersFilters(user.ID)
	if err != nil {
		return err
	}

	// Get oauth token.
	// TODO do

	//ts := oauth2.StaticTokenSource(
	//&oauth2.Token{AccessToken: string(user.GitHubToken)},
	//)
	//tc := oauth2.NewClient(ctx, ts)
	//client := github.NewClient(tc)

	//newClient returns a github.Client using oauthconf and token.
	//func newClient(ctx context.Context, oauthConf *oauth2.Config, token *oauth2.Token) *github.Client {
	//oauthClient := oauthConf.Client(ctx, token)
	//client := github.NewClient(oauthClient)
	//client.BaseURL, _ = url.Parse(githubBaseURL)
	//return client
	//}

	// TODO add an underlying caching transport
	client := github.NewClient(nil)

	newEvents, pollInterval, err := ListNewEvents(ctx, client, user.GitHubUser, user.EventLastCreatedAt)
	if err != nil {
		return err
	}

	if len(newEvents) > 0 {
		// Mark all events as read from here.
		err := p.db.SetUsersPollResult(user.ID, newEvents[0].CreatedAt, time.Now().Add(pollInterval))
		if err != nil {
			return err
		}
	}

	notifyEvents := FilterEvents(filters, newEvents)

	// Send notifications.
	for _, event := range notifyEvents {
		if err = p.notifier.Notify(event); err != nil {
			return err
		}
	}

	return nil
}
