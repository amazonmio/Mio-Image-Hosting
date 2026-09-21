package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"
)

// Returning from Serve is not the end of Shutdown: active handlers may still
// be running. The caller may close the database only after this function returns.
func serveUntilStopped(ctx context.Context, srv *http.Server, listener net.Listener, grace time.Duration) error {
	serving := make(chan error, 1)
	go func() { serving <- srv.Serve(listener) }()
	select {
	case err := <-serving:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	shutdownErr := srv.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		shutdownErr = errors.Join(shutdownErr, srv.Close())
	}
	serveErr := <-serving
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}
	return errors.Join(shutdownErr, serveErr)
}
