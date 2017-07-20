package main

import (
	"context"
	"log"
	"os"
	"time"

	maintainer "github.com/bradleyfalzon/maintainer.me"
	"github.com/bradleyfalzon/maintainer.me/events"
	"github.com/bradleyfalzon/maintainer.me/notifier"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load() // Ignore errors as .env is optional

	ctx := context.Background()

	m, err := maintainer.NewMaintainer()
	if err != nil {
		log.Fatal(err)
	}

	// Notifiers
	notifier := &notifier.Writer{Writer: os.Stdout}

	// Poller
	poller := events.NewPoller(m.Logger, m.DB, notifier, m.Cache)
	err = poller.Poll(ctx, 60*time.Second) // blocking
	if err != nil {
		m.Logger.WithError(err).Fatalf("Poller failed")
	}
	m.Logger.Info("Poller exiting")
}
