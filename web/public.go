package web

import (
	"bytes"
	"html/template"
	"io"
	"net/http"

	"github.com/Sirupsen/logrus"
)

type Public struct {
	logger    *logrus.Logger
	templates *template.Template
}

// NewPublic returns a new web instance.
func NewPublic(logger *logrus.Logger) (*Public, error) {
	templates, err := template.ParseGlob("web/templates/public-*.tmpl")
	if err != nil {
		return nil, err
	}

	return &Public{
		logger:    logger,
		templates: templates,
	}, nil
}

func (p *Public) render(w http.ResponseWriter, logger *logrus.Entry, template string, data interface{}) {
	buf := &bytes.Buffer{}
	if err := p.templates.ExecuteTemplate(buf, template, data); err != nil {
		logger.WithField("template", template).WithError(err).Error("could not execute template")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	io.Copy(w, buf)
}

// Home is the handler to view the console page.
func (p *Public) Home(w http.ResponseWriter, r *http.Request) {
	logger := p.logger.WithField("requestURI", r.RequestURI)
	p.render(w, logger, "public-home.tmpl", nil)
}
