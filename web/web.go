package web

import (
	"bytes"
	"context"
	"html/template"
	"io"
	"net/http"
	"time"

	"golang.org/x/oauth2"

	"github.com/Sirupsen/logrus"
	"github.com/alexedwards/scs/session"
	"github.com/bradleyfalzon/maintainer.me/db"
	"github.com/bradleyfalzon/maintainer.me/events"
	"github.com/go-chi/chi/middleware"
	"github.com/google/go-github/github"
	"github.com/google/uuid"
	"github.com/pressly/chi"
)

type Web struct {
	logger      *logrus.Entry
	db          db.DB
	cache       http.RoundTripper
	templates   *template.Template
	ghoauthConf *oauth2.Config
}

// NewWeb returns a new web instance.
func NewWeb(logger *logrus.Entry, db db.DB, cache http.RoundTripper, router chi.Router, sessionEngine session.Engine, ghoauthConf *oauth2.Config) error {
	templates, err := template.ParseGlob("web/templates/*.tmpl")
	if err != nil {
		return err
	}

	web := &Web{
		logger:      logger,
		db:          db,
		cache:       cache,
		templates:   templates,
		ghoauthConf: ghoauthConf,
	}

	sessionManager := session.Manage(
		sessionEngine,
		session.Lifetime(365*24*time.Hour),
		session.Persist(true),
		session.Secure(true),
		session.HttpOnly(true),
		session.ErrorFunc(func(w http.ResponseWriter, r *http.Request, err error) {
			web.logger.WithError(err).Error("session handling error")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}),
	)

	router.Use(sessionManager)

	router.Use(middleware.DefaultCompress)
	router.Use(middleware.Recoverer)
	router.Use(middleware.NoCache)
	router.Get("/", web.HomeHandler)
	router.Get("/login", web.LoginHandler)
	router.Get("/login/callback", web.LoginCallbackHandler)
	//router.Get("/logout", web.LogoutHandler)
	router.Route("/console", func(router chi.Router) {
		router.Use(web.RequireLogin)
		router.Get("/", web.ConsoleHomeHandler)
		router.Get("/events", web.ConsoleEventsHandler)
	})

	return nil
}

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

func (web *Web) render(w http.ResponseWriter, template string, data interface{}) {
	buf := &bytes.Buffer{}
	if err := web.templates.ExecuteTemplate(buf, template, data); err != nil {
		web.logger.WithField("template", template).WithError(err).Error("could not execute template")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	io.Copy(w, buf)
}

// HomeHandler is the handler to view the console page.
func (web *Web) HomeHandler(w http.ResponseWriter, r *http.Request) {
	web.render(w, "home.tmpl", nil)
}

const ghOAuthStateKey = "ghOAuthState"

// LoginHandler is the handler to view the console page.
func (web *Web) LoginHandler(w http.ResponseWriter, r *http.Request) {
	uuid := uuid.New()
	session.PutString(r, ghOAuthStateKey, uuid.String())

	url := web.ghoauthConf.AuthCodeURL(uuid.String(), oauth2.AccessTypeOnline)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

// LoginCallbackHandler is the handler used by the login page during a callback
// after the user has logged into service.
func (web *Web) LoginCallbackHandler(w http.ResponseWriter, r *http.Request) {
	// Get and *remove* state stored in session.
	sessionState, err := session.PopString(r, ghOAuthStateKey)
	if err != nil {
		web.logger.WithError(err).Errorf("could not get session's %v", ghOAuthStateKey)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if r.FormValue("state") != sessionState {
		web.logger.WithError(err).Errorf("received state %q does not match session state %q", r.FormValue("state"), sessionState)
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	token, err := web.ghoauthConf.Exchange(r.Context(), r.FormValue("code"))
	if err != nil {
		web.logger.WithError(err).Errorf("could not exchange oauth code %q for token", r.FormValue("code"))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	// Create oauth client
	client := web.githubClient(r.Context(), token)

	// Get GitHub ID
	ghUser, _, err := client.Users.Get(r.Context(), "")
	if err != nil {
		web.logger.WithError(err).Error("could not get github authenticated user details")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if ghUser.GetID() == 0 {
		web.logger.WithError(err).Errorf("github authenticated user's ID is %d", ghUser.GetID())
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	// Create or Update user's account with GitHub ID
	userID, err := web.db.GitHubLogin(r.Context(), ghUser.GetEmail(), ghUser.GetID(), ghUser.GetLogin(), token)
	if err != nil {
		web.logger.WithError(err).Error("could not user's ID")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	web.logger.WithFields(logrus.Fields{
		"userID":      userID,
		"githubID":    ghUser.GetID(),
		"githubLogin": ghUser.GetLogin(),
	}).Info("User logged in")

	// Set our UserID in session
	session.PutInt(r, "userID", userID)

	http.Redirect(w, r, "/console", http.StatusSeeOther)
}

func (web *Web) githubClient(ctx context.Context, token *oauth2.Token) *github.Client {
	hClient := http.DefaultClient
	hClient.Transport = web.cache

	ctx = context.WithValue(ctx, oauth2.HTTPClient, hClient)
	oauthClient := web.ghoauthConf.Client(ctx, token)

	return github.NewClient(oauthClient)
}

// ConsoleHomeHandler is the handler to view the console page.
func (web *Web) ConsoleHomeHandler(w http.ResponseWriter, r *http.Request) {
	_, err := session.GetString(r, "username")
	if err != nil {
		web.logger.WithError(err).Error("could not get session")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	web.render(w, "console-home.tmpl", nil)
}

// ConsoleEventsHandler is a handler to view events that have been filtered.
func (web *Web) ConsoleEventsHandler(w http.ResponseWriter, r *http.Request) {
	userID, err := session.GetInt(r, "userID")
	if err != nil {
		web.logger.WithError(err).Error("could not userID from session")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	user, err := web.db.User(userID)
	if err != nil {
		web.logger.WithError(err).Error("could not get user")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	filters, err := web.db.UsersFilters(user.ID)
	if err != nil {
		web.logger.WithError(err).Error("could not get user's filters")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	client := web.githubClient(r.Context(), user.GitHubToken)

	allEvents, _, err := events.ListNewEvents(r.Context(), client, user.GitHubLogin, user.EventLastCreatedAt)
	if err != nil {
		web.logger.WithError(err).Error("could not list new events")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	allEvents.Filter(filters)

	page := struct {
		Title  string
		Events events.Events
	}{"Maintainer.Me", allEvents}

	web.render(w, "console-events.tmpl", page)
}
