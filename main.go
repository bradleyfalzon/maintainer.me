package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/bradleyfalzon/maintainer.me/db"
	"github.com/bradleyfalzon/maintainer.me/events"
	"github.com/bradleyfalzon/maintainer.me/notifier"
	"github.com/bradleyfalzon/maintainer.me/web"
	"github.com/gregjones/httpcache"
	"github.com/gregjones/httpcache/diskcache"
	"github.com/pressly/chi"
	"github.com/pressly/chi/middleware"
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

	rt := httpcache.NewTransport(diskcache.New("/tmp"))

	poller := events.NewPoller(db, notifier, rt)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		err := poller.Poll(ctx, 60*time.Second)
		if err != nil {
			log.Println("Poller exited with error:", err)
		}
		log.Println("Poller exited")
		wg.Done()
	}()

	r := chi.NewRouter()
	r.Use(middleware.DefaultCompress)
	r.Use(middleware.Recoverer)
	r.Use(middleware.NoCache)

	web, err := web.NewWeb(db, rt)
	if err != nil {
		return err
	}

	r.Get("/", web.HomeHandler)

	srv := &http.Server{
		Addr:    ":3001",
		Handler: r,
	}
	wg.Add(1)
	go func() {
		log.Println("Listening on", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Println("main: http server error:", err)
		}
		log.Println("Server shut down")
		wg.Done()
	}()

	go func() {
		<-ctx.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		srv.Shutdown(ctx)
		cancel()
	}()

	wg.Wait()
	return nil
}
