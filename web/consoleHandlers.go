package web

import (
	"net/http"
	"strconv"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/alexedwards/scs/session"
	"github.com/bradleyfalzon/maintainer.me/db"
	"github.com/bradleyfalzon/maintainer.me/events"
	"github.com/go-chi/chi"
	schema "github.com/gorilla/Schema"
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

// ConsoleHome is the handler to view the console page.
func (web *Web) ConsoleHome(w http.ResponseWriter, r *http.Request) {
	logger := web.logger.WithField("requestURI", r.RequestURI)
	_, err := session.GetString(r, "username")
	if err != nil {
		logger.WithError(err).Error("could not get session")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	web.render(w, logger, "console-home.tmpl", nil)
}

// ConsoleEvents is a handler to view events that have been filtered.
func (web *Web) ConsoleEvents(w http.ResponseWriter, r *http.Request) {
	logger := web.logger.WithField("requestURI", r.RequestURI)
	userID, err := session.GetInt(r, "userID")
	if err != nil {
		logger.WithError(err).Error("could not userID from session")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	logger = logger.WithField("userID", userID)

	user, err := web.db.User(r.Context(), userID)
	if err != nil {
		logger.WithError(err).Error("could not get user")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	filters, err := web.db.UsersFilters(r.Context(), user.ID)
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
	allEvents.Filter(filters, user.FilterDefaultDiscard)

	page := struct {
		Title  string
		Events events.Events
		Since  time.Duration
	}{"Events - Maintainer.Me", allEvents, since}

	web.render(w, logger, "console-events.tmpl", page)
}

// ConsoleFilters is a handler to view user's filters.
func (web *Web) ConsoleFilters(w http.ResponseWriter, r *http.Request) {
	logger := web.logger.WithField("requestURI", r.RequestURI)
	userID, err := session.GetInt(r, "userID")
	if err != nil {
		logger.WithError(err).Error("could not userID from session")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	logger = logger.WithField("userID", userID)

	user, err := web.db.User(r.Context(), userID)
	if err != nil {
		logger.WithError(err).Error("could not get user")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	filters, err := web.db.UsersFilters(r.Context(), userID)
	if err != nil {
		logger.WithError(err).Error("could not get user's filters")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	page := struct {
		Title                string
		FilterDefaultDiscard bool
		Filters              []db.Filter
	}{"Filters - Maintainer.Me", user.FilterDefaultDiscard, filters}

	web.render(w, logger, "console-filters.tmpl", page)
}

// ConsoleFiltersUpdate updates filter list.
func (web *Web) ConsoleFiltersUpdate(w http.ResponseWriter, r *http.Request) {
	logger := web.logger.WithField("requestURI", r.RequestURI)
	userID, err := session.GetInt(r, "userID")
	if err != nil {
		logger.WithError(err).Error("could not userID from session")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	logger = logger.WithField("userID", userID)

	user, err := web.db.User(r.Context(), userID)
	if err != nil {
		logger.WithError(err).Error("could not get user")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	user.FilterDefaultDiscard = r.FormValue("filterdefaultdiscard") == "true"

	err = web.db.UserUpdate(r.Context(), user)
	if err != nil {
		logger.WithError(err).Error("could not update user")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	logger.Info("successfully updated filters")

	http.Redirect(w, r, r.Header.Get("referer"), http.StatusFound)
}

// ConsoleFilter is a handler to view a single user's filter.
func (web *Web) ConsoleFilter(w http.ResponseWriter, r *http.Request) {
	logger := web.logger.WithField("requestURI", r.RequestURI)
	userID, err := session.GetInt(r, "userID")
	if err != nil {
		logger.WithError(err).Error("could not userID from session")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	logger = logger.WithFields(logrus.Fields{
		"userID":   userID,
		"filterID": chi.URLParam(r, "filterID"),
	})

	filterID, err := strconv.ParseInt(chi.URLParam(r, "filterID"), 10, 32)
	if err != nil {
		logger.WithError(err).Error("could not parse filterID from URL")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	filter, err := web.db.Filter(r.Context(), int(filterID))
	if err != nil {
		logger.WithError(err).Errorf("could not get filter %v", filterID)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if filter == nil {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	if filter.UserID != userID {
		logger.Infof("filter user ID %d does not match session user ID %d", filter.UserID, userID)
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	page := struct {
		Title  string
		Filter *db.Filter
	}{"Filter - Maintainer.Me", filter}

	web.render(w, logger, "console-filter.tmpl", page)
}

// ConsoleConditionDelete deletes a condition.
func (web *Web) ConsoleConditionDelete(w http.ResponseWriter, r *http.Request) {
	logger := web.logger.WithField("requestURI", r.RequestURI)
	userID, err := session.GetInt(r, "userID")
	if err != nil {
		logger.WithError(err).Error("could not userID from session")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	logger = logger.WithFields(logrus.Fields{
		"userID":      userID,
		"conditionID": chi.URLParam(r, "conditionID"),
	})

	conditionID, err := strconv.ParseInt(chi.URLParam(r, "conditionID"), 10, 32)
	if err != nil {
		logger.WithError(err).Error("could not parse conditionID from URL")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	err = web.db.ConditionDelete(r.Context(), userID, int(conditionID))
	if err != nil {
		logger.WithError(err).Error("could not delete condition")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	logger.Info("successfully deleted condition")
}

// ConsoleConditionCreate deletes a condition.
func (web *Web) ConsoleConditionCreate(w http.ResponseWriter, r *http.Request) {
	logger := web.logger.WithField("requestURI", r.RequestURI)
	userID, err := session.GetInt(r, "userID")
	if err != nil {
		logger.WithError(err).Error("could not userID from session")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	r.ParseForm()
	logger = logger.WithFields(logrus.Fields{
		"userID":   userID,
		"filterID": r.FormValue("filterID"),
	})

	filterID, err := strconv.ParseInt(r.FormValue("filterID"), 10, 32)
	if err != nil {
		logger.WithError(err).Error("could not parse filterID from URL")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	// Check permissions

	filter, err := web.db.Filter(r.Context(), int(filterID))
	if err != nil {
		logger.WithError(err).Error("could not get filter")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if filter == nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	if filter.UserID != userID {
		logger.Infof("filter user ID %d does not match session user ID %d", filter.UserID, userID)
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	// Scan user data into struct

	var (
		condition = &db.Condition{}
		postForm  = map[string][]string{
			"Negate":             []string{r.FormValue("negate")},
			r.FormValue("field"): []string{r.FormValue("value")},
		}
		decoder = schema.NewDecoder()
	)

	err = decoder.Decode(condition, postForm)
	if err != nil {
		logger.WithError(err).Error("could not decode form")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	// Overwrite the condition struct with the filterID we know belongs
	// to the user, else they might have tried to overwrite it.
	condition.FilterID = int(filterID)

	conditionID, err := web.db.ConditionCreate(r.Context(), condition)
	if err != nil {
		logger.WithError(err).Error("could not delete condition")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	logger.WithField("condition", conditionID).Info("successfully added condition")

	http.Redirect(w, r, r.Header.Get("referer"), http.StatusFound)
}

// ConsoleFilterUpdate updates a filter.
func (web *Web) ConsoleFilterUpdate(w http.ResponseWriter, r *http.Request) {
	logger := web.logger.WithField("requestURI", r.RequestURI)
	userID, err := session.GetInt(r, "userID")
	if err != nil {
		logger.WithError(err).Error("could not userID from session")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	logger = logger.WithFields(logrus.Fields{
		"userID":   userID,
		"filterID": chi.URLParam(r, "filterID"),
	})

	filterID, err := strconv.ParseInt(chi.URLParam(r, "filterID"), 10, 32)
	if err != nil {
		logger.WithError(err).Error("could not parse filterID from URL")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	filter, err := web.db.Filter(r.Context(), int(filterID))
	if err != nil {
		logger.WithError(err).Errorf("could not get filter %v", filterID)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if filter == nil {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	if filter.UserID != userID {
		logger.Infof("filter user ID %d does not match session user ID %d", filter.UserID, userID)
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	// Update filter

	filter.OnMatchDiscard = r.FormValue("onmatchdiscard") == "true"

	err = web.db.FilterUpdate(r.Context(), filter)
	if err != nil {
		logger.WithError(err).Error("could not update filter")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	logger.Info("successfully updated filter")

	http.Redirect(w, r, r.Header.Get("referer"), http.StatusFound)
}
