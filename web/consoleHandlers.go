package web

import (
	"net/http"
	"time"

	"github.com/alexedwards/scs/session"
	"github.com/bradleyfalzon/ghfilter"
	"github.com/bradleyfalzon/maintainer.me/events"
)

// RequireLogin is middleware that loads a user's session and they
// are logged in, and with a valid account. If not, the user is redirected
// to /login. If an error occurs, a HTTP Internal Server Error is displayed.
func (web *Web) RequireLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		loggedIn, err := session.Exists(r, "userID")
		if err != nil {
			web.logger.WithError(err).Error("RequireLogin could not check if session exists")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if !loggedIn {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		// TODO check if oauth credential is still valid?
		// Too slow to do on every request? But if they unauthorise us
		// how could we know reliably? May be we have another middleware
		// in /console/github/ and any routes in there have an extra middleware
		// that checks oauth each time.

		// TODO check if user exists in DB
		next.ServeHTTP(w, r)
	})
}

// ConsoleHomeHandler is the handler to view the console page.
func (web *Web) ConsoleHomeHandler(w http.ResponseWriter, r *http.Request) {
	logger := web.logger.WithField("requestURI", r.RequestURI)
	_, err := session.GetString(r, "username")
	if err != nil {
		logger.WithError(err).Error("could not get session")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	web.render(w, logger, "console-home.tmpl", nil)
}

// ConsoleEventsHandler is a handler to view events that have been filtered.
func (web *Web) ConsoleEventsHandler(w http.ResponseWriter, r *http.Request) {
	logger := web.logger.WithField("requestURI", r.RequestURI)
	userID, err := session.GetInt(r, "userID")
	if err != nil {
		logger.WithError(err).Error("could not userID from session")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	logger = logger.WithField("userID", userID)

	user, err := web.db.User(userID)
	if err != nil {
		logger.WithError(err).Error("could not get user")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	filters, err := web.db.UsersFilters(user.ID)
	if err != nil {
		logger.WithError(err).Error("could not get user's filters")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	client := web.githubClient(r.Context(), user.GitHubToken)

	since := -1 * 24 * time.Hour

	allEvents, _, err := events.ListNewEvents(r.Context(), logger, client, user.GitHubLogin, time.Now().Add(since))
	if err != nil {
		logger.WithError(err).Error("could not list new events")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	allEvents.Filter(filters)

	page := struct {
		Title  string
		Events events.Events
		Since  time.Duration
	}{"Events - Maintainer.Me", allEvents, since}

	web.render(w, logger, "console-events.tmpl", page)
}

// ConsoleFiltersHandler is a handler to view user's filters.
func (web *Web) ConsoleFiltersHandler(w http.ResponseWriter, r *http.Request) {
	logger := web.logger.WithField("requestURI", r.RequestURI)
	userID, err := session.GetInt(r, "userID")
	if err != nil {
		logger.WithError(err).Error("could not userID from session")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	logger = logger.WithField("userID", userID)

	filters, err := web.db.UsersFilters(userID)
	if err != nil {
		logger.WithError(err).Error("could not get user's filters")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	page := struct {
		Title   string
		Filters []ghfilter.Filter
	}{"Filters - Maintainer.Me", filters}

	web.render(w, logger, "console-filters.tmpl", page)
}
