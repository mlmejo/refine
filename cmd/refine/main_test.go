package main

import (
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServeHTTPShutdownGivesUpOnHungRequest(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
	})
	stdinR, stdinW := io.Pipe()

	done := make(chan error, 1)
	go func() { done <- serveHTTP(ln, h, stdinR, 100*time.Millisecond) }()

	go func() {
		resp, err := http.Get("http://" + ln.Addr().String())
		if err == nil {
			resp.Body.Close()
		}
	}()
	<-entered
	_ = stdinW.Close()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serveHTTP = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serveHTTP still waiting on a hung request after its deadline")
	}
}

func TestNewHTTPServerTimeouts(t *testing.T) {
	s := newHTTPServer(http.NotFoundHandler())
	if s.ReadHeaderTimeout <= 0 {
		t.Error("ReadHeaderTimeout unset")
	}
	if s.WriteTimeout != 0 {
		t.Errorf("WriteTimeout = %v, want unset: it would cut off /stamp", s.WriteTimeout)
	}
}

func TestServeHTTPWaitsForRequestInFlight(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	finished := make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		time.Sleep(200 * time.Millisecond)
		close(finished)
	})
	stdinR, stdinW := io.Pipe()

	done := make(chan error, 1)
	go func() { done <- serveHTTP(ln, h, stdinR, 5*time.Second) }()

	go func() {
		resp, err := http.Get("http://" + ln.Addr().String())
		if err == nil {
			resp.Body.Close()
		}
	}()
	<-entered
	_ = stdinW.Close()

	if err := <-done; err != nil {
		t.Fatalf("serveHTTP = %v, want nil", err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("serveHTTP returned before the request in flight finished")
	}
}
