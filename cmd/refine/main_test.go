package main

import (
	"bytes"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mlmejo/refine/internal/config"
	"github.com/mlmejo/refine/internal/jira"
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

func TestSetupWritesTheCheckedAccount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	var checked []string
	myself := func(site, email, token string) (string, error) {
		checked = []string{site, email, token}
		return "Ada", nil
	}
	var out bytes.Buffer
	in := strings.NewReader(`{"site":" https://geekee.atlassian.net/ ","email":"a@b.c","token":"t "}`)
	if err := setup(in, &out, path, myself); err != nil {
		t.Fatal(err)
	}
	if want := []string{"geekee.atlassian.net", "a@b.c", "t"}; !slices.Equal(checked, want) {
		t.Errorf("checked %v, want %v", checked, want)
	}
	if got := strings.TrimSpace(out.String()); got != `{"name":"Ada"}` {
		t.Errorf("out = %s", got)
	}
	t.Setenv(config.TokenEnv, "")
	if cfg, err := config.Load(path); err != nil || cfg.Jira.Token != "t" {
		t.Errorf("token = %q err = %v", cfg.Jira.Token, err)
	}
}

func TestSetupRefusesWhatJiraTurnsDown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	myself := func(site, email, token string) (string, error) {
		return "", &jira.APIError{StatusCode: http.StatusUnauthorized}
	}
	in := strings.NewReader(`{"site":"s","email":"e","token":"bad"}`)
	if err := setup(in, io.Discard, path, myself); err == nil || !strings.Contains(err.Error(), "turned down") {
		t.Errorf("err = %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("config written for a refused account: %v", err)
	}
}
