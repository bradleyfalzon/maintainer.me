package web

import (
	"html/template"
	"log"
	"net/http"

	"github.com/bradleyfalzon/maintainer.me/db"
	"github.com/bradleyfalzon/maintainer.me/events"
	"github.com/google/go-github/github"
)

type Web struct {
	db        db.DB
	templates *template.Template
}

func NewWeb(db db.DB) (*Web, error) {
	templates, err := template.ParseGlob("web/templates/*.tmpl")
	if err != nil {
		return nil, err
	}

	return &Web{
		db:        db,
		templates: templates,
	}, nil
}

func (web *Web) HomeHandler(w http.ResponseWriter, r *http.Request) {
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

	client := github.NewClient(nil)

	newEvents, _, err := events.ListNewEvents(r.Context(), client, user.GitHubUser, user.EventLastCreatedAt)
	if err != nil {
		log.Println(err)
		return
	}

	filteredEvents := events.FilterEvents(filters, newEvents)

	page := struct {
		Title    string
		Events   []events.Event
		Filtered []events.Event
	}{"Maintainer.Me", newEvents, filteredEvents}

	if err := web.templates.ExecuteTemplate(w, "home.tmpl", page); err != nil {
		log.Println(err)
	}
}
