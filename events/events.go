package events

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/bradleyfalzon/ghfilter"
	"github.com/google/go-github/github"
	"github.com/pkg/errors"
)

type Events []*Event

func ListNewEvents(ctx context.Context, client *github.Client, githubUser string, lastCreatedAt time.Time) (events Events, pollInterval time.Duration, err error) {
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

			parsedEvent, err := ParseEvent(event)
			if err != nil {
				return nil, 0, err
			}
			events = append(events, parsedEvent)
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

func (e Events) Filter(filters []ghfilter.Filter) {
	for _, event := range e {
		event.Filter(filters)
	}
}

type Event struct {
	RawEvent  *github.Event
	CreatedAt time.Time // CreatedAt is the time the event was created.
	Type      string    // Type such as "CommitCommentEvent".
	Public    bool      // Public is whether GitHub event was public.

	// Excluded is true when an event has been filtered and should be excluded.
	Excluded bool

	Actor   string // Actor is the person who did an action, such as "bradleyfalzon".
	Action  string // Action is the action performed on a subject, such as "commented".
	Subject string // Subject is something that an action was performed on, such as "golang/go".

	Title string // Title is a short description of the event, such as "[golang/go] bradleyfalzon commented on abcdef1234"
	Body  string // Body contains more context and may be blank.
}

func ParseEvent(ghe *github.Event) (*Event, error) {
	payload, err := ghe.ParsePayload()
	if err != nil {
		return nil, errors.Wrap(err, "could not parse event payload")
	}

	e := &Event{
		RawEvent:  ghe,
		CreatedAt: ghe.GetCreatedAt(),
		Type:      ghe.GetType(),
		Public:    ghe.GetPublic(),
	}
	switch p := payload.(type) {
	case *github.CommitCommentEvent:
		e.Actor = ghe.Actor.GetLogin()
		e.Action = "commented"
		e.Subject = p.Comment.GetCommitID()
		e.Body = p.Comment.GetBody()
		e.Title = fmt.Sprintf("[%s] %s %s on: %s", ghe.Repo.GetName(), e.Actor, e.Action, e.Subject)
	case *github.CreateEvent:
	case *github.DeleteEvent:
	case *github.DeploymentEvent:
	case *github.DeploymentStatusEvent:
	case *github.ForkEvent:
	case *github.GollumEvent:
		e.Actor = ghe.Actor.GetLogin()
		e.Action = "edited"
		e.Subject = ghe.Repo.GetName()
		//e.Body = p.Issue.GetBody()
		e.Title = fmt.Sprintf("[%s] %s %s wiki on: %s", ghe.Repo.GetName(), e.Actor, e.Action, e.Subject)
	case *github.InstallationEvent:
	case *github.InstallationRepositoriesEvent:
	case *github.IssueCommentEvent:
		e.Actor = ghe.Actor.GetLogin()
		e.Action = "commented"
		e.Subject = p.Issue.GetTitle()
		e.Body = p.Comment.GetBody()
		e.Title = fmt.Sprintf("[%s] %s %s on: %s", ghe.Repo.GetName(), e.Actor, e.Action, e.Subject)
	case *github.IssuesEvent:
		e.Actor = ghe.Actor.GetLogin()
		e.Action = "opened"
		e.Subject = p.Issue.GetTitle()
		e.Body = p.Issue.GetBody()
		e.Title = fmt.Sprintf("[%s] %s %s: %s", ghe.Repo.GetName(), e.Actor, e.Action, e.Subject)
	case *github.LabelEvent:
	case *github.MemberEvent:
	case *github.MembershipEvent:
	case *github.MilestoneEvent:
	case *github.OrganizationEvent:
	case *github.OrgBlockEvent:
	case *github.PageBuildEvent:
	case *github.PingEvent:
	case *github.ProjectEvent:
	case *github.ProjectCardEvent:
	case *github.ProjectColumnEvent:
	case *github.PublicEvent:
	case *github.PullRequestEvent:
		e.Actor = ghe.Actor.GetLogin()
		e.Action = p.GetAction()
		e.Subject = strconv.Itoa(p.GetNumber())
		e.Body = p.PullRequest.GetBody()
		e.Title = fmt.Sprintf("[%s] %s %s #%s: %s", ghe.Repo.GetName(), e.Actor, e.Action, e.Subject, p.PullRequest.GetTitle())
	case *github.PullRequestReviewEvent:
	case *github.PullRequestReviewCommentEvent:
	case *github.PushEvent:
		e.Actor = ghe.Actor.GetLogin()
		e.Action = "pushed"
		e.Subject = fmt.Sprintf("%s %s", ghe.Repo.GetName(), p.GetRef())
		//e.Body = p.Issue.GetBody()
		e.Title = fmt.Sprintf("[%s] %s %s %d commits to %s", ghe.Repo.GetName(), e.Actor, e.Action, len(p.Commits), e.Subject)
	case *github.ReleaseEvent:
	case *github.RepositoryEvent:
	case *github.StatusEvent:
	case *github.TeamEvent:
	case *github.TeamAddEvent:
	case *github.WatchEvent:
	}
	return e, nil
}

func (e *Event) String() string {
	return e.Title
}

func (e *Event) Filter(filters []ghfilter.Filter) {
	for _, filter := range filters {
		log.Println("checking match", e.CreatedAt, e.Type, filter.Matches(e.RawEvent))
		if filter.Matches(e.RawEvent) {
			e.Excluded = false
			return
		}
	}
	e.Excluded = true // Event did not match a filter.
}
