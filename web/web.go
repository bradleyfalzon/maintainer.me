package web

import (
	"bytes"
	"context"
	"html/template"
	"io"
	"net/http"

	"golang.org/x/oauth2"

	"github.com/Sirupsen/logrus"
	"github.com/alexedwards/scs/session"
	"github.com/bradleyfalzon/maintainer.me/db"
	"github.com/google/go-github/github"
	"github.com/google/uuid"
)

type Web struct {
	logger      *logrus.Entry
	db          db.DB
	cache       http.RoundTripper
	templates   *template.Template
	ghoauthConf *oauth2.Config
}

// NewWeb returns a new web instance.
func NewWeb(logger *logrus.Entry, db db.DB, cache http.RoundTripper, ghoauthConf *oauth2.Config) (*Web, error) {
	templates, err := template.ParseGlob("web/templates/*.tmpl")
	if err != nil {
		return nil, err
	}

	return &Web{
		logger:      logger,
		db:          db,
		cache:       cache,
		templates:   templates,
		ghoauthConf: ghoauthConf,
	}, nil
}

func (web *Web) render(w http.ResponseWriter, logger *logrus.Entry, template string, data interface{}) {
	buf := &bytes.Buffer{}
	if err := web.templates.ExecuteTemplate(buf, template, data); err != nil {
		logger.WithField("template", template).WithError(err).Error("could not execute template")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	io.Copy(w, buf)
}

// Home is the handler to view the console page.
func (web *Web) Home(w http.ResponseWriter, r *http.Request) {
	logger := web.logger.WithField("requestURI", r.RequestURI)
	web.render(w, logger, "home.tmpl", nil)
}

const ghOAuthStateKey = "ghOAuthState"

// Login is the handler to view the console page.
func (web *Web) Login(w http.ResponseWriter, r *http.Request) {
	uuid := uuid.New()
	session.PutString(r, ghOAuthStateKey, uuid.String())

	url := web.ghoauthConf.AuthCodeURL(uuid.String(), oauth2.AccessTypeOnline)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

// LoginCallback is the handler used by the login page during a callback
// after the user has logged into service.
func (web *Web) LoginCallback(w http.ResponseWriter, r *http.Request) {
	logger := web.logger.WithField("requestURI", r.RequestURI)
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

	token, err := web.ghoauthConf.Exchange(r.Context(), r.FormValue("code"))
	if err != nil {
		logger.WithError(err).Errorf("could not exchange oauth code %q for token", r.FormValue("code"))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	// Create oauth client
	client := web.githubClient(r.Context(), token)

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
	userID, err := web.db.GitHubLogin(r.Context(), ghUser.GetEmail(), ghUser.GetID(), ghUser.GetLogin(), token)
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

func (web *Web) githubClient(ctx context.Context, token *oauth2.Token) *github.Client {
	hClient := http.DefaultClient
	hClient.Transport = web.cache

	ctx = context.WithValue(ctx, oauth2.HTTPClient, hClient)
	oauthClient := web.ghoauthConf.Client(ctx, token)

	return github.NewClient(oauthClient)
}
