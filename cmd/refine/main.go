// Command refine serves the desktop app. refine serve listens on localhost,
// prints {"port":…,"token":…} as its first line, and exits when its stdin
// closes; it exits with exitNotSetUp when the config lacks the Jira account.
// refine setup reads {"site":…,"email":…,"token":…} from stdin, checks it
// with Jira and writes it to the config. refine version prints the build.
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
	"strings"
	"time"

	"github.com/mlmejo/refine/internal/app"
	"github.com/mlmejo/refine/internal/config"
	"github.com/mlmejo/refine/internal/jira"
	"github.com/mlmejo/refine/internal/refine"
	"github.com/mlmejo/refine/internal/version"
)

const usage = `refine refines Jira tickets with Claude Code, through the desktop app.

Usage:
  refine serve     serve the desktop app (it starts this itself)
  refine setup     save the Jira account read as JSON from stdin
  refine version   print the build and prompts version
`

// exitNotSetUp is refine serve's exit code when the config lacks the Jira
// account, so the desktop app asks for it.
const exitNotSetUp = 3

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "refine:", err)
		if errors.Is(err, config.ErrNotSetUp) {
			os.Exit(exitNotSetUp)
		}
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
	case "setup":
		path, err := config.Path()
		if err != nil {
			return err
		}
		return setup(os.Stdin, os.Stdout, path, func(site, email, token string) (string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			return jira.New(site, email, token).Myself(ctx)
		})
	}
	fmt.Fprint(os.Stderr, usage)
	return fmt.Errorf("unknown command %q", args[0])
}

// setup reads the Jira account from in, checks it with myself, which returns
// the account's display name, and writes it to the config at path. It prints
// {"name":…}.
func setup(in io.Reader, out io.Writer, path string, myself func(site, email, token string) (string, error)) error {
	var account struct {
		Site  string `json:"site"`
		Email string `json:"email"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(in).Decode(&account); err != nil {
		return fmt.Errorf("read the account: %w", err)
	}
	site := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(account.Site), "https://"), "/")
	email, token := strings.TrimSpace(account.Email), strings.TrimSpace(account.Token)
	if site == "" || email == "" || token == "" {
		return errors.New("give the Jira site, email and API token")
	}

	name, err := myself(site, email, token)
	if apiErr := (*jira.APIError)(nil); errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden) {
		return errors.New("that email and API token were turned down by Jira")
	}
	if err != nil {
		return fmt.Errorf("reach Jira at %s: %w", site, err)
	}
	if err := config.SetJira(path, site, email, token); err != nil {
		return err
	}
	hello, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, string(hello))
	return err
}

// serve answers the desktop app until it closes refine's stdin.
func serve(a *app.App) error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	hello, err := json.Marshal(map[string]any{"port": ln.Addr().(*net.TCPAddr).Port, "token": a.Server.Token()})
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
