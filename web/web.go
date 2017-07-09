package web

import (
	"html/template"
	"log"
	"net/http"

	"github.com/alexedwards/scs/engine/memstore"
	"github.com/alexedwards/scs/session"
	"github.com/bradleyfalzon/maintainer.me/db"
	"github.com/bradleyfalzon/maintainer.me/events"
	"github.com/go-chi/chi/middleware"
	"github.com/google/go-github/github"
	"github.com/pressly/chi"
)

type Web struct {
	db        db.DB
	rt        http.RoundTripper
	templates *template.Template
}

func AddRoutes(db db.DB, rt http.RoundTripper, router *chi.Mux) error {
	templates, err := template.ParseGlob("web/templates/*.tmpl")
	if err != nil {
		return err
	}

	web := &Web{
		db:        db,
		rt:        rt,
		templates: templates,
	}

	sh := memstore.New(0)
	router.Use(session.Manage(sh))

	router.Use(middleware.DefaultCompress)
	router.Use(middleware.Recoverer)
	router.Use(middleware.NoCache)
	router.Get("/", web.HomeHandler)
	router.Get("/login", web.LoginHandler)
	//router.Get("/logout", web.LogoutHandler)
	router.Route("/console", func(router chi.Router) {
		router.Use(RequireLogin)
		router.Get("/", web.ConsoleHomeHandler)
		router.Get("/events", web.ConsoleEventsHandler)
	})
	return nil
}

// RequireLogin is middleware that loads a user's session and they
// are logged in, and with a valid account. If not, the user is redirected
// to /login. If an error occurs, a HTTP Internal Server Error is displayed.
func RequireLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		loggedIn, err := session.Exists(r, "username")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !loggedIn {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
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
	err := session.PutString(r, "username", "bradleyfalzon")
	if err != nil {
		log.Println(err)
		return // TODO show error to user
	}

	http.Redirect(w, r, "/console", http.StatusSeeOther)

	if err := web.templates.ExecuteTemplate(w, "home.tmpl", nil); err != nil {
		log.Println(err)
	}
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
		Transport: web.rt,
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
