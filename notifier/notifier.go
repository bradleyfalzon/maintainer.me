package notifier

import (
	"fmt"
	"io"

	"github.com/google/go-github/github"
)

// Notifier sends a notification about a GitHub Event.
type Notifier interface {
	Notify(event *github.Event) error
}

// Writer is a Notifier that writes the event to the supplied writer.
type Writer struct {
	Writer io.Writer
}

// Notify implements the Notifier interface.
func (w *Writer) Notify(event *github.Event) error {
	_, err := fmt.Fprintf(w.Writer, "NOTIFY: Event Type: %s ID: %s\n", event.GetType(), event.GetID())
	return err
}
