// Command refine serves the desktop app. refine serve listens on localhost,
// prints {"port":…,"token":…} as its first line, and exits when its stdin
// closes. refine version prints the build.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/mlmejo/refine/internal/app"
	"github.com/mlmejo/refine/internal/refine"
	"github.com/mlmejo/refine/internal/version"
)

const usage = `refine refines Jira tickets with Claude Code, through the desktop app.

Usage:
  refine serve     serve the desktop app (it starts this itself)
  refine version   print the build and prompts version
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "refine:", err)
		os.Exit(1)
	}
}

func run(args []string) (err error) {
	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}
	switch args[0] {
	case "version":
		fmt.Println("build", version.Build())
		fmt.Println("prompts", refine.PromptsVersion())
		return nil
	case "serve":
		a, err := app.Open()
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, a.Close()) }()
		return serve(a)
	}
	fmt.Fprint(os.Stderr, usage)
	return fmt.Errorf("unknown command %q", args[0])
}

// serve answers the desktop app until it closes refine's stdin.
func serve(a *app.App) error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	hello, err := json.Marshal(map[string]any{"port": ln.Addr().(*net.TCPAddr).Port, "token": a.Server.Token})
	if err != nil {
		return err
	}
	fmt.Println(string(hello))

	return serveHTTP(ln, a.Server.Handler(), os.Stdin, shutdownGrace)
}

// shutdownGrace is how long refine waits for requests in flight, such as a
// slow /stamp, before it drops them.
const shutdownGrace = 5 * time.Second

// serveHTTP serves h on ln until stdin closes, then shuts down, giving
// requests in flight grace to finish before it drops them.
func serveHTTP(ln net.Listener, h http.Handler, stdin io.Reader, grace time.Duration) error {
	httpSrv := newHTTPServer(h)
	shutDown := make(chan struct{})
	go func() {
		defer close(shutDown)
		_, _ = io.Copy(io.Discard, stdin)
		ctx, cancel := context.WithTimeout(context.Background(), grace)
		defer cancel()
		if err := httpSrv.Shutdown(ctx); err != nil {
			_ = httpSrv.Close()
		}
	}()
	if err := httpSrv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	// Serve returns as soon as Shutdown starts; wait for requests in flight
	// so the caller doesn't close the store under them.
	<-shutDown
	return nil
}

// newHTTPServer leaves WriteTimeout unset: /stamp makes several network calls
// and a short one would cut it off.
func newHTTPServer(h http.Handler) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
	}
}
