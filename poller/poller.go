package poller

import (
	"context"
	"log"
	"strconv"
	"time"

	"github.com/bradleyfalzon/maintainer.me/db"
	"github.com/bradleyfalzon/maintainer.me/notifier"
	"github.com/google/go-github/github"
	"github.com/pkg/errors"
)

// githubBaseURL is the baseURL for github.com/google/go-github/github
// client, variable to easily change in tests.
var githubBaseURL = "https://api.github.com/"

type Poller struct {
	db       db.DB
	notifier notifier.Notifier
}

func NewPoller(db db.DB, notifier notifier.Notifier) *Poller {
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
				time.Sleep(interval * 2)
			}
		case <-ctx.Done():
			log.Println("Finishing...")
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

	if user.EventNextUpdate.After(time.Now()) {
		log.Printf("now: %v, next update: %v, skipping...", time.Now(), user.EventNextUpdate)
		return nil
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

	newEvents, pollInterval, err := listNewEvents(ctx, client, user.GitHubUser, user.EventLastCreatedAt)
	if err != nil {
		return err
	}

	if len(newEvents) > 0 {
		// Mark all events as read from here.
		err := p.db.SetUsersPollResult(user.ID, newEvents[0].GetCreatedAt(), time.Now().Add(pollInterval))
		if err != nil {
			return err
		}
	}

	// Copy matched events to notifyEvents
	var notifyEvents []*github.Event
	for _, event := range newEvents[:5] { // TODO remove :5
		for _, filter := range filters {
			if filter.Matches(event) {
				notifyEvents = append(notifyEvents, event)
				break
			}
		}
	}

	// Send notifications.
	for _, event := range notifyEvents {
		if err = p.notifier.Notify(event); err != nil {
			return err
		}
	}

	return nil
}

func listNewEvents(ctx context.Context, client *github.Client, githubUser string, lastCreatedAt time.Time) (events []*github.Event, pollInterval time.Duration, err error) {
	opt := github.ListOptions{Page: 1}

ListEvents:
	for {
		log.Printf("getting events for user %q page %v", githubUser, opt.Page)
		pagedEvents, response, err := client.Activity.ListEventsReceivedByUser(ctx, githubUser, false, &opt)
		if err != nil {
			return nil, 0, errors.Wrapf(err, "could not get GitHub events for user %q", githubUser)
		}

		// Use the etag and poll from last page
		pollInt, err := strconv.ParseInt(response.Response.Header.Get("X-Poll-Interval"), 10, 32)
		if err != nil {
			return nil, 0, errors.Wrap(err, "could not parse GitHub's X-Poll-Interval header")
		}
		pollInterval = time.Duration(pollInt) * time.Second

		for _, event := range pagedEvents {
			// I'm worried we could lose events here, I need get only new events
			// but if two events happen in the same second, but we poll after
			// the first event happens, but before the second, and then filter out
			// all events that happen before or at that second - we'll skip that
			// second event. Better skipping an event than duplicating it.
			//
			// Alternatively, we could use the Event's ID and cast to int.
			if haveObserved(lastCreatedAt, event.GetCreatedAt()) {
				// We've already observed this event, assume the list is sorted
				// in reverse chronological order and therefore all remaining events
				// have also been observed.
				break ListEvents
			}
			events = append(events, event)
		}

		if response.NextPage == 0 {
			break
		}
		opt.Page = response.NextPage
	}
	return events, pollInterval, err
}

func haveObserved(observed, query time.Time) bool {
	return !observed.IsZero() && (query.Before(observed) || query.Equal(observed))
}
