package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestGracefulStopWaitsForActiveRequest(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	entered, release, stopping := make(chan struct{}), make(chan struct{}), make(chan struct{})
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; w.Write([]byte("finished")) })}
	srv.RegisterOnShutdown(func() { close(stopping) })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- serveUntilStopped(ctx, srv, listener, 2*time.Second) }()
	response := make(chan string, 1)
	go func() {
		res, e := http.Get("http://" + listener.Addr().String())
		if e != nil {
			response <- e.Error()
			return
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		response <- string(raw)
	}()
	<-entered
	cancel()
	<-stopping
	select {
	case err := <-stopped:
		close(release)
		t.Fatalf("returned before active request finished: %v", err)
	case <-time.After(75 * time.Millisecond):
	}
	close(release)
	if body := <-response; body != "finished" {
		t.Fatal(body)
	}
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
}
func TestStopDeadlineClosesStalledConnections(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	entered, disconnected := make(chan struct{}), make(chan struct{})
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(disconnected)
	})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- serveUntilStopped(ctx, srv, listener, 50*time.Millisecond) }()
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		res, e := http.Get("http://" + listener.Addr().String())
		if e == nil {
			res.Body.Close()
		}
	}()
	<-entered
	cancel()
	if err := <-stopped; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected timeout, got %v", err)
	}
	select {
	case <-disconnected:
	case <-time.After(2 * time.Second):
		t.Fatal("active connection was not closed")
	}
	<-clientDone
}
func TestServeFailureReturnsWithoutWaitingForSignal(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener.Close()
	result := make(chan error, 1)
	go func() { result <- serveUntilStopped(context.Background(), &http.Server{}, listener, time.Second) }()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("expected listener error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("startup error hung")
	}
}
