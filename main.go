package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/bradleyfalzon/maintainer.me/db"
	"github.com/bradleyfalzon/maintainer.me/notifier"
	"github.com/bradleyfalzon/maintainer.me/poller"
)

func main() {
	fmt.Println("Starting...")

	if err := run(); err != nil {
		log.Fatal(err)
	}
	log.Println("Terminating")
}

func run() error {

	ctx := context.Background()

	notifier := &notifier.Writer{Writer: os.Stdout}
	db := db.NewSQLDB()

	poller := poller.NewPoller(db, notifier)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		err := poller.Poll(ctx, 5*time.Second)
		if err != nil {
			log.Println("Poller exited with error:", err)
		}
		wg.Done()
	}()

	wg.Wait()
	return nil
}
