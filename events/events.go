package events

import (
	"context"
	"log"
	"strconv"
	"time"

	"github.com/bradleyfalzon/ghfilter"
	"github.com/google/go-github/github"
	"github.com/pkg/errors"
)

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

func filterEvents(filters []ghfilter.Filter, events []*github.Event) []*github.Event {
	var filtered []*github.Event
	for _, event := range events {
		for _, filter := range filters {
			if filter.Matches(event) {
				filtered = append(filtered, event)
				break
			}
		}
	}
	return filtered
}
