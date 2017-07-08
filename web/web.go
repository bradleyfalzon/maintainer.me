package web

import (
	"html/template"
	"log"
	"net/http"

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

	router.Use(middleware.DefaultCompress)
	router.Use(middleware.Recoverer)
	router.Use(middleware.NoCache)
	//router.Get("/", web.HomeHandler)
	//router.Get("/logout", web.LogoutHandler)
	router.Route("/console", func(r chi.Router) {
		// TODO must be user
		//router.Get("/", web.ConsoleIndexHandler)
		router.Get("/events", web.ConsoleEventsHandler)
	})
	return nil
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
