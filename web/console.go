package web

import (
	"bytes"
	"context"
	"html/template"
	"io"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/oauth2"

	"github.com/Sirupsen/logrus"
	"github.com/alexedwards/scs/session"
	"github.com/bradleyfalzon/maintainer.me/db"
	"github.com/bradleyfalzon/maintainer.me/events"
	"github.com/go-chi/chi"
	"github.com/google/go-github/github"
	"github.com/google/uuid"
	schema "github.com/gorilla/Schema"
)

type Console struct {
	logger      *logrus.Entry
	db          db.DB
	cache       http.RoundTripper
	templates   *template.Template
	ghoauthConf *oauth2.Config
}

// NewConsole returns a new console instance.
func NewConsole(logger *logrus.Entry, db db.DB, cache http.RoundTripper, ghoauthConf *oauth2.Config) (*Console, error) {
	templates, err := template.ParseGlob("web/templates/console-*.tmpl")
	if err != nil {
		return nil, err
	}

	return &Console{
		logger:      logger,
		db:          db,
		cache:       cache,
		templates:   templates,
		ghoauthConf: ghoauthConf,
	}, nil
}

func (c *Console) render(w http.ResponseWriter, logger *logrus.Entry, template string, data interface{}) {
	buf := &bytes.Buffer{}
	if err := c.templates.ExecuteTemplate(buf, template, data); err != nil {
		logger.WithField("template", template).WithError(err).Error("could not execute template")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	io.Copy(w, buf)
}

const ghOAuthStateKey = "ghOAuthState"

// Login is the handler to view the console page.
func (c *Console) Login(w http.ResponseWriter, r *http.Request) {
	uuid := uuid.New()
	session.PutString(r, ghOAuthStateKey, uuid.String())

	url := c.ghoauthConf.AuthCodeURL(uuid.String(), oauth2.AccessTypeOnline)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

// LoginCallback is the handler used by the login page during a callback
// after the user has logged into service.
func (c *Console) LoginCallback(w http.ResponseWriter, r *http.Request) {
	logger := c.logger.WithField("requestURI", r.RequestURI)
	// Get and *remove* state stored in session.
	sessionState, err := session.PopString(r, ghOAuthStateKey)
	if err != nil {
		logger.WithError(err).Errorf("could not get session's %v", ghOAuthStateKey)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if r.FormValue("state") != sessionState {
		logger.WithError(err).Errorf("received state %q does not match session state %q", r.FormValue("state"), sessionState)
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	token, err := c.ghoauthConf.Exchange(r.Context(), r.FormValue("code"))
	if err != nil {
		logger.WithError(err).Errorf("could not exchange oauth code %q for token", r.FormValue("code"))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	// Create oauth client
	client := c.githubClient(r.Context(), token)

	// Get GitHub ID
	ghUser, _, err := client.Users.Get(r.Context(), "")
	if err != nil {
		logger.WithError(err).Error("could not get github authenticated user details")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if ghUser.GetID() == 0 {
		logger.WithError(err).Errorf("github authenticated user's ID is %d", ghUser.GetID())
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	// Create or Update user's account with GitHub ID
	userID, err := c.db.GitHubLogin(r.Context(), ghUser.GetEmail(), ghUser.GetID(), ghUser.GetLogin(), token)
	if err != nil {
		logger.WithError(err).Error("could not user's ID")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	logger.WithFields(logrus.Fields{
		"userID":      userID,
		"githubID":    ghUser.GetID(),
		"githubLogin": ghUser.GetLogin(),
	}).Info("User logged in")

	// Set our UserID in session
	session.PutInt(r, "userID", userID)

	http.Redirect(w, r, "/console", http.StatusSeeOther)
}

func (c *Console) githubClient(ctx context.Context, token *oauth2.Token) *github.Client {
	hClient := http.DefaultClient
	hClient.Transport = c.cache

	ctx = context.WithValue(ctx, oauth2.HTTPClient, hClient)
	oauthClient := c.ghoauthConf.Client(ctx, token)

	return github.NewClient(oauthClient)
}

type userCtxKey struct{}

// userFromContext returns a user from the context. Requires context to have
// the user already added else panics.
func userFromContext(ctx context.Context) *db.User {
	return ctx.Value(userCtxKey{}).(*db.User)
}

// RequireLogin is middleware that loads a user's session and they
// are logged in, and with a valid account. If not, the user is redirected
// to /login. If an error occurs, a HTTP Internal Server Error is displayed.
//
// Also adds db.User type to context.
func (c *Console) RequireLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID, err := session.GetInt(r, "userID")
		if err != nil {
			c.logger.WithError(err).Error("RequireLogin could not get userID from session")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if userID == 0 {
			// Key does not exist
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		user, err := c.db.User(r.Context(), userID)
		if err != nil {
			c.logger.WithError(err).Errorf("RequireLogin could not get userID %v", userID)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if user == nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		r = r.WithContext(context.WithValue(r.Context(), userCtxKey{}, user))

		// TODO check if oauth credential is still valid?
		// Too slow to do on every request? But if they unauthorise us
		// how could we know reliably? May be we have another middleware
		// in /console/github/ and any routes in there have an extra middleware
		// that checks oauth each time.

		next.ServeHTTP(w, r)
	})
}

func (c *Console) loggerFromRequest(r *http.Request) *logrus.Entry {
	user := userFromContext(r.Context())
	return c.logger.WithFields(logrus.Fields{
		"requestURI":    r.RequestURI,
		"requestMethod": r.Method,
		"userID":        user.ID,
	})
}

// ConsoleHome is the handler to view the console page.
func (c *Console) Home(w http.ResponseWriter, r *http.Request) {
	logger := c.loggerFromRequest(r)
	_, err := session.GetString(r, "username")
	if err != nil {
		logger.WithError(err).Error("could not get session")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	c.render(w, logger, "console-home.tmpl", nil)
}

// ConsoleEvents is a handler to view events that have been filtered.
func (c *Console) Events(w http.ResponseWriter, r *http.Request) {
	var (
		logger = c.loggerFromRequest(r)
		user   = userFromContext(r.Context())
	)

	filters, err := c.db.UsersFilters(r.Context(), user.ID)
	if err != nil {
		logger.WithError(err).Error("could not get user's filters")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	client := c.githubClient(r.Context(), user.GitHubToken)

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

	c.render(w, logger, "console-events.tmpl", page)
}

// ConsoleFilters is a handler to view user's filters.
func (c *Console) Filters(w http.ResponseWriter, r *http.Request) {
	var (
		logger = c.loggerFromRequest(r)
		user   = userFromContext(r.Context())
	)

	filters, err := c.db.UsersFilters(r.Context(), user.ID)
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

	c.render(w, logger, "console-filters.tmpl", page)
}

// ConsoleFiltersUpdate updates filter list.
func (c *Console) FiltersUpdate(w http.ResponseWriter, r *http.Request) {
	var (
		logger = c.loggerFromRequest(r)
		user   = userFromContext(r.Context())
	)

	user.FilterDefaultDiscard = r.FormValue("filterdefaultdiscard") == "true"

	err := c.db.UserUpdate(r.Context(), user)
	if err != nil {
		logger.WithError(err).Error("could not update user")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	logger.Info("successfully updated filters")

	http.Redirect(w, r, r.Header.Get("referer"), http.StatusFound)
}

// ConsoleFilter is a handler to view a single user's filter.
func (c *Console) Filter(w http.ResponseWriter, r *http.Request) {
	var (
		logger = c.loggerFromRequest(r)
		user   = userFromContext(r.Context())
	)

	logger = logger.WithField("filterID", chi.URLParam(r, "filterID"))

	filterID, err := strconv.ParseInt(chi.URLParam(r, "filterID"), 10, 32)
	if err != nil {
		logger.WithError(err).Error("could not parse filterID from URL")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	filter, err := c.db.Filter(r.Context(), int(filterID))
	if err != nil {
		logger.WithError(err).Errorf("could not get filter %v", filterID)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if filter == nil {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	if filter.UserID != user.ID {
		logger.Infof("filter user ID %d does not match session user ID %d", filter.UserID, user.ID)
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	page := struct {
		Title  string
		Filter *db.Filter
	}{"Filter - Maintainer.Me", filter}

	c.render(w, logger, "console-filter.tmpl", page)
}

// ConsoleConditionDelete deletes a condition.
func (c *Console) ConditionDelete(w http.ResponseWriter, r *http.Request) {
	var (
		logger = c.loggerFromRequest(r)
		user   = userFromContext(r.Context())
	)

	logger = logger.WithField("conditionID", chi.URLParam(r, "conditionID"))

	conditionID, err := strconv.ParseInt(chi.URLParam(r, "conditionID"), 10, 32)
	if err != nil {
		logger.WithError(err).Error("could not parse conditionID from URL")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	err = c.db.ConditionDelete(r.Context(), user.ID, int(conditionID))
	if err != nil {
		logger.WithError(err).Error("could not delete condition")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	logger.Info("successfully deleted condition")
}

// ConsoleConditionCreate deletes a condition.
func (c *Console) ConditionCreate(w http.ResponseWriter, r *http.Request) {
	var (
		logger = c.loggerFromRequest(r)
		user   = userFromContext(r.Context())
	)

	r.ParseForm()
	logger = logger.WithField("filterID", r.FormValue("filterID"))

	filterID, err := strconv.ParseInt(r.FormValue("filterID"), 10, 32)
	if err != nil {
		logger.WithError(err).Error("could not parse filterID from URL")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	// Check permissions

	filter, err := c.db.Filter(r.Context(), int(filterID))
	if err != nil {
		logger.WithError(err).Error("could not get filter")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if filter == nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	if filter.UserID != user.ID {
		logger.Infof("filter user ID %d does not match session user ID %d", filter.UserID, user.ID)
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

	conditionID, err := c.db.ConditionCreate(r.Context(), condition)
	if err != nil {
		logger.WithError(err).Error("could not delete condition")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	logger.WithField("condition", conditionID).Info("successfully added condition")

	http.Redirect(w, r, r.Header.Get("referer"), http.StatusFound)
}

// ConsoleFilterUpdate updates a filter.
func (c *Console) FilterUpdate(w http.ResponseWriter, r *http.Request) {
	var (
		logger = c.loggerFromRequest(r)
		user   = userFromContext(r.Context())
	)

	logger = logger.WithField("filterID", chi.URLParam(r, "filterID"))

	filterID, err := strconv.ParseInt(chi.URLParam(r, "filterID"), 10, 32)
	if err != nil {
		logger.WithError(err).Error("could not parse filterID from URL")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	filter, err := c.db.Filter(r.Context(), int(filterID))
	if err != nil {
		logger.WithError(err).Errorf("could not get filter %v", filterID)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if filter == nil {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	if filter.UserID != user.ID {
		logger.Infof("filter user ID %d does not match session user ID %d", filter.UserID, user.ID)
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	// Update filter

	filter.OnMatchDiscard = r.FormValue("onmatchdiscard") == "true"

	err = c.db.FilterUpdate(r.Context(), filter)
	if err != nil {
		logger.WithError(err).Error("could not update filter")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	logger.Info("successfully updated filter")

	http.Redirect(w, r, r.Header.Get("referer"), http.StatusFound)
}

// Repos is a handler to view all user's repos
func (c *Console) Repos(w http.ResponseWriter, r *http.Request) {
	var (
		logger = c.loggerFromRequest(r)
		user   = userFromContext(r.Context())
	)

	client := c.githubClient(r.Context(), user.GitHubToken)

	opt := &github.RepositoryListOptions{
		ListOptions: github.ListOptions{
			Page:    1,
			PerPage: 100,
		},
	}

	var allRepos []*github.Repository
	for {
		logger.Debugf("list user's repositories page %v", opt.Page)
		repos, resp, err := client.Repositories.List(r.Context(), "", opt)
		if err != nil {
			logger.WithError(err).Error("could not list user's repositories")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		allRepos = append(allRepos, repos...)
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}

	type Repo struct {
		FullName string
		HTMLURL  string
		Topics   []string

		OpenIssuesCount  int
		StargazersCount  int
		Fork             bool
		License          string
		HealthPercentage int
		HasCoC           bool
		HasContributing  bool
		HasLicense       bool
		HasReadme        bool
	}

	var repos []Repo
	for _, lr := range allRepos {
		if lr.GetFork() {
			continue
		}

		repo := Repo{
			FullName:        lr.GetFullName(),
			HTMLURL:         lr.GetHTMLURL(),
			StargazersCount: lr.GetStargazersCount(),
			OpenIssuesCount: lr.GetOpenIssuesCount(),
			Fork:            lr.GetFork(),
			Topics:          lr.Topics,
		}

		health, _, err := client.Repositories.GetCommunityHealthMetrics(r.Context(), lr.Owner.GetLogin(), lr.GetName())
		if err != nil {
			logger.WithError(err).Errorf("could not get community health metrics for %v", lr.GetFullName())
			repos = append(repos, repo)
			continue
		}

		repo.HealthPercentage = health.GetHealthPercentage()

		if health.Files.CodeOfConduct.Key != nil && health.Files.CodeOfConduct.GetKey() != "none" {
			repo.HasCoC = true
		}
		if health.Files.Contributing.URL != nil {
			repo.HasContributing = true
		}
		if health.Files.License.Key != nil {
			repo.License = health.Files.License.GetKey()
			repo.HasLicense = true
		}
		if health.Files.Readme.HTMLURL != nil {
			repo.HasReadme = true
		}

		repos = append(repos, repo)
	}

	page := struct {
		Title string
		Repos []Repo
	}{"Repositories - Maintainer.Me", repos}

	c.render(w, logger, "console-repos.tmpl", page)
}
