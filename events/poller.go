package events

import (
	"context"
	"net/http"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/bradleyfalzon/maintainer.me/db"
	"github.com/google/go-github/github"
	"github.com/pkg/errors"
)

// githubBaseURL is the baseURL for github.com/google/go-github/github
// client, variable to easily change in tests.
var githubBaseURL = "https://api.github.com/"

type Poller struct {
	logger   *logrus.Entry
	db       db.DB
	notifier Notifier
	rt       http.RoundTripper
}

// Notifier sends a notification about a GitHub Event.
type Notifier interface {
	Notify(event *Event) error
}

func NewPoller(logger *logrus.Entry, db db.DB, notifier Notifier, rt http.RoundTripper) *Poller {
	return &Poller{
		logger:   logger,
		db:       db,
		notifier: notifier,
		rt:       rt,
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
			p.logger.Debug("Polling...")
			err := p.PollUsers(ctx)
			if err != nil {
				p.logger.WithError(err).Error("error polling users")
			}
		case <-ctx.Done():
			p.logger.Error("poller finishing")
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

	var errorCount int
	for _, user := range users {
		logger := p.logger.WithField("userID", user.ID)
		err := p.PollUser(ctx, logger, user)
		if err != nil {
			errorCount++
			logger.WithError(err).Errorf("could not poll user")
		}
		if errorCount > 5 {
			return errors.WithMessage(err, "too many errors")
		}
	}
	return nil
}

func (p *Poller) PollUser(ctx context.Context, logger *logrus.Entry, user db.User) error {
	logger.Debugf("polling user")
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
	httpClient := &http.Client{
		Transport: p.rt,
	}
	client := github.NewClient(httpClient)

	events, pollInterval, err := ListNewEvents(ctx, logger, client, user.GitHubLogin, user.EventLastCreatedAt)
	if err != nil {
		return errors.Wrap(err, "could not list new events for user")
	}

	if len(events) > 0 {
		// Mark all events as read from here.
		err := p.db.SetUsersPollResult(user.ID, events[0].CreatedAt, time.Now().Add(pollInterval))
		if err != nil {
			return err
		}
	}

	//events.Filter(db.GHFilters(filters))
	events.Filter(filters, user.FilterDefaultDiscard)

	// Send notifications.
	for _, event := range events {
		if event.Discarded {
			continue
		}
		if err = p.notifier.Notify(event); err != nil {
			return err
		}
	}

	return nil
}
