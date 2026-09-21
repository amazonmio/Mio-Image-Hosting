package main

import (
	"context"
	"log"
	"net"
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
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	app, err := server.New(env("DATA_DIR", "data"), os.Getenv("ADMIN_TOKEN"), os.Getenv("PUBLIC_BASE_URL"))
	if err != nil {
		return err
	}
	defer app.Close()
	srv := server.NewHTTPServer(env("ADDR", "127.0.0.1:8080"), app.Handler())
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	listener, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return err
	}
	log.Printf("Mio Image Hosting listening on http://%s", srv.Addr)
	return serveUntilStopped(ctx, srv, listener, 15*time.Second)
}
