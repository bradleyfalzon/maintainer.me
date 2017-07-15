package events

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/bradleyfalzon/maintainer.me/db"
	"github.com/google/go-github/github"
	"github.com/pkg/errors"
)

type Events []*Event

func ListNewEvents(ctx context.Context, logger *logrus.Entry, client *github.Client, githubUser string, lastCreatedAt time.Time) (events Events, pollInterval time.Duration, err error) {
	opt := github.ListOptions{Page: 1}

ListEvents:
	for {
		start := time.Now()
		pagedEvents, response, err := client.Activity.ListEventsReceivedByUser(ctx, githubUser, false, &opt)
		if err != nil {
			return nil, 0, errors.Wrapf(err, "could not get GitHub events for user %q", githubUser)
		}
		logger.Debugf("polled events page %v in %v", opt.Page, time.Since(start))

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

func (e Events) Filter(filters []db.Filter) {
	for _, event := range e {
		event.Filter(filters)
	}
}

type Event struct {
	RawEvent  *github.Event
	CreatedAt time.Time // CreatedAt is the time the event was created.
	Type      string    // Type such as "CommitCommentEvent".
	Public    bool      // Public is whether GitHub event was public.

	// Discarded is true when an event has been filtered and should be ignored.
	Discarded bool

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
		e.Title = fmt.Sprintf("[%s] %s %s on %s", ghe.Repo.GetName(), e.Actor, e.Action, e.Subject)
	case *github.CreateEvent:
		e.Actor = ghe.Actor.GetLogin()
		e.Action = "created " + p.GetRefType()
		e.Subject = p.GetRef()
		if e.Subject == "" {
			e.Subject = ghe.Repo.GetName()
		}
		e.Title = fmt.Sprintf("[%s] %s %s %s", ghe.Repo.GetName(), e.Actor, e.Action, e.Subject)
	case *github.DeleteEvent:
		// TODO
	case *github.DeploymentEvent:
		// TODO
	case *github.DeploymentStatusEvent:
		// TODO
	case *github.ForkEvent:
		e.Actor = ghe.Actor.GetLogin()
		e.Action = "forked"
		e.Subject = ghe.Repo.GetName()
		e.Title = fmt.Sprintf("[%s] %s %s repository", ghe.Repo.GetName(), e.Actor, e.Action)
	case *github.GollumEvent:
		e.Actor = ghe.Actor.GetLogin()
		e.Action = "edited"
		e.Subject = ghe.Repo.GetName()
		//e.Body = p.Issue.GetBody()
		e.Title = fmt.Sprintf("[%s] %s %s wiki on %s", ghe.Repo.GetName(), e.Actor, e.Action, e.Subject)
	case *github.InstallationEvent:
		// TODO
	case *github.InstallationRepositoriesEvent:
		// TODO
	case *github.IssueCommentEvent:
		e.Actor = ghe.Actor.GetLogin()
		e.Action = p.GetAction()
		var verb string
		switch p.GetAction() {
		case "created":
			verb = "commented on"
		case "edited comment in":
			verb = "edited comment in"
		case "deleted":
			verb = "deleted comment in"
		default:
		}
		e.Subject = fmt.Sprintf("%s (#%d)", p.Issue.GetTitle(), p.Issue.GetNumber())
		e.Body = p.Comment.GetBody()
		e.Title = fmt.Sprintf("[%s] %s %s %s", ghe.Repo.GetName(), e.Actor, verb, e.Subject)
	case *github.IssuesEvent:
		e.Actor = ghe.Actor.GetLogin()
		e.Action = p.GetAction()
		e.Subject = fmt.Sprintf("%s (#%d)", p.Issue.GetTitle(), p.Issue.GetNumber())
		e.Body = p.Issue.GetBody()
		e.Title = fmt.Sprintf("[%s] %s %s %s", ghe.Repo.GetName(), e.Actor, e.Action, e.Subject)
	case *github.LabelEvent:
		// TODO
	case *github.MemberEvent:
		// TODO
	case *github.MembershipEvent:
		// TODO
	case *github.MilestoneEvent:
		// TODO
	case *github.OrganizationEvent:
		// TODO
	case *github.OrgBlockEvent:
		// TODO
	case *github.PageBuildEvent:
		// TODO
	case *github.PingEvent:
		// TODO
	case *github.ProjectEvent:
		// TODO
	case *github.ProjectCardEvent:
		// TODO
	case *github.ProjectColumnEvent:
		// TODO
	case *github.PublicEvent:
		// TODO
	case *github.PullRequestEvent:
		e.Actor = ghe.Actor.GetLogin()
		e.Action = p.GetAction()
		e.Subject = fmt.Sprintf("%s (#%d)", p.PullRequest.GetTitle(), p.PullRequest.GetNumber())
		e.Body = p.PullRequest.GetBody()
		e.Title = fmt.Sprintf("[%s] %s %s %s", ghe.Repo.GetName(), e.Actor, e.Action, e.Subject)
	case *github.PullRequestReviewEvent:
		// TODO
	case *github.PullRequestReviewCommentEvent:
		// TODO
	case *github.PushEvent:
		e.Actor = ghe.Actor.GetLogin()
		e.Action = "pushed"
		e.Subject = fmt.Sprintf("%s %s", ghe.Repo.GetName(), p.GetRef())
		//e.Body = p.Issue.GetBody()
		e.Title = fmt.Sprintf("[%s] %s %s %d commits to %s", ghe.Repo.GetName(), e.Actor, e.Action, len(p.Commits), e.Subject)
	case *github.ReleaseEvent:
		// TODO
	case *github.RepositoryEvent:
		// TODO
	case *github.StatusEvent:
		// TODO
	case *github.TeamEvent:
		// TODO
	case *github.TeamAddEvent:
		// TODO
	case *github.WatchEvent:
		// TODO
	}
	return e, nil
}

func (e *Event) String() string {
	return e.Title
}

func (e *Event) Filter(filters []db.Filter) {
	for _, filter := range filters {
		if filter.Matches(e.RawEvent) {
			e.Discarded = filter.OnMatchDiscard
			return
		}
	}
	e.Discarded = true // Event did not match a filter.
}
