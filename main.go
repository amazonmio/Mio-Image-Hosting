package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/amazonmio/mio-image-hosting/internal/server"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	app, err := server.New(env("DATA_DIR", "data"), os.Getenv("ADMIN_TOKEN"), os.Getenv("PUBLIC_BASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer app.Close()
	srv := server.NewHTTPServer(env("ADDR", "127.0.0.1:8080"), app.Handler())
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdown); err != nil {
			log.Print(err)
		}
	}()
	log.Printf("Mio Image Hosting listening on http://%s", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
