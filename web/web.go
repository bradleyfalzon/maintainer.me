package web

import (
	"context"
	"html/template"
	"log"
	"net/http"

	"golang.org/x/oauth2"

	"github.com/alexedwards/scs/engine/memstore"
	"github.com/alexedwards/scs/session"
	"github.com/bradleyfalzon/maintainer.me/db"
	"github.com/bradleyfalzon/maintainer.me/events"
	"github.com/go-chi/chi/middleware"
	"github.com/google/go-github/github"
	"github.com/google/uuid"
	"github.com/pressly/chi"
)

type Web struct {
	db          db.DB
	cache       http.RoundTripper
	templates   *template.Template
	ghoauthConf *oauth2.Config
}

// NewWeb returns a new web instance.
func NewWeb(db db.DB, cache http.RoundTripper, router chi.Router, ghoauthConf *oauth2.Config) error {
	templates, err := template.ParseGlob("web/templates/*.tmpl")
	if err != nil {
		return err
	}

	web := &Web{
		db:          db,
		cache:       cache,
		templates:   templates,
		ghoauthConf: ghoauthConf,
	}

	sh := memstore.New(0)
	router.Use(session.Manage(sh))

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
			http.Error(w, err.Error(), http.StatusInternalServerError)
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

// HomeHandler is the handler to view the console page.
func (web *Web) HomeHandler(w http.ResponseWriter, r *http.Request) {
	if err := web.templates.ExecuteTemplate(w, "home.tmpl", nil); err != nil {
		log.Println(err)
	}
}

// LoginHandler is the handler to view the console page.
func (web *Web) LoginHandler(w http.ResponseWriter, r *http.Request) {
	uuid := uuid.New()
	session.PutString(r, "ghOAuthState", uuid.String())

	url := web.ghoauthConf.AuthCodeURL(uuid.String(), oauth2.AccessTypeOnline)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

// LoginCallbackHandler is the handler used by the login page during a callback
// after the user has logged into service.
func (web *Web) LoginCallbackHandler(w http.ResponseWriter, r *http.Request) {
	// Get and *remove* state stored in session.
	sessionState, err := session.PopString(r, "ghOAuthState")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		log.Println(err)
		// TODO log
		return
	}

	if r.FormValue("state") != sessionState {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		log.Println(err)
		// TODO log
		return
	}

	token, err := web.ghoauthConf.Exchange(r.Context(), r.FormValue("code"))
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		log.Println(err)
		// TODO log
		return
	}

	// TODO store this token against a user in persistent DB, (along with their email?)
	// We'll need it for the poller to access user's details

	// Create oauth client

	hClient := http.DefaultClient
	hClient.Transport = web.cache

	ctx := context.WithValue(r.Context(), oauth2.HTTPClient, hClient)
	oauthClient := web.ghoauthConf.Client(ctx, token)

	client := github.NewClient(oauthClient)

	// Get GitHub ID
	ghUser, _, err := client.Users.Get(r.Context(), "")
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		log.Println(err)
		// TODO log
		return
	}

	if ghUser.GetID() == 0 {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		log.Println("error id is zero")
		// TODO log
		return
	}
	log.Println("logged in with ID:", ghUser.GetID())

	// Create or Update user's account with GitHub ID
	userID, err := web.db.GitHubLogin(r.Context(), ghUser.GetID(), token)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		log.Println(err)
		// TODO log
		return
	}

	// Set our UserID in session

	log.Println("got token", token)
	log.Println("got userID", userID)

	session.PutInt(r, "userID", userID)

	http.Redirect(w, r, "/console", http.StatusSeeOther)
}

// ConsoleHomeHandler is the handler to view the console page.
func (web *Web) ConsoleHomeHandler(w http.ResponseWriter, r *http.Request) {
	user, err := session.GetString(r, "username")
	if err != nil {
		log.Println(err)
		return // TODO show error
	}
	log.Println("console home user:", user)
	if err := web.templates.ExecuteTemplate(w, "console-home.tmpl", nil); err != nil {
		log.Println(err)
	}
}

// ConsoleEventsHandler is a handler to view events that have been filtered.
func (web *Web) ConsoleEventsHandler(w http.ResponseWriter, r *http.Request) {
	users, err := web.db.Users()
	if err != nil {
		log.Println(err)
		return
	}
	user := users[0]

	filters, err := web.db.UsersFilters(user.ID)
	if err != nil {
		log.Println(err)
		return
	}

	httpClient := &http.Client{
		Transport: web.cache,
	}
	client := github.NewClient(httpClient)

	allEvents, _, err := events.ListNewEvents(r.Context(), client, user.GitHubUser, user.EventLastCreatedAt)
	if err != nil {
		log.Println(err)
		return
	}
	allEvents.Filter(filters)

	page := struct {
		Title  string
		Events events.Events
	}{"Maintainer.Me", allEvents}

	if err := web.templates.ExecuteTemplate(w, "home.tmpl", page); err != nil {
		log.Println(err)
	}
}
