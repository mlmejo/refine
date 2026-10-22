// Command refine helps fill a ticket's refinement fields with Claude Code.
//
// refine runs the TUI, frozen at home.TUIVersion. refine serve serves the
// desktop app instead: it listens on localhost and prints
// {"port":…,"token":…} as its first line, and exits when its stdin closes.
// refine version prints the TUI's version.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mlmejo/refine/internal/app"
	"github.com/mlmejo/refine/internal/draft"
	"github.com/mlmejo/refine/internal/home"
	"github.com/mlmejo/refine/internal/server"
	"github.com/mlmejo/refine/internal/version"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "refine:", err)
		os.Exit(1)
	}
}

func run() error {
	ref := flag.String("ref", "", "read the repos at this branch, e.g. develop, instead of their current checkout")
	flag.Parse()
	if flag.Arg(0) == "version" {
		fmt.Println("refine TUI", home.TUIVersion)
		fmt.Println("build", version.Build())
		fmt.Println("prompts", draft.PromptsVersion())
		return nil
	}

	ui := "tui"
	if flag.Arg(0) == "serve" {
		ui = "gui"
	}
	a, err := app.Open(*ref, ui)
	if err != nil {
		return err
	}
	defer a.Close()

	switch flag.Arg(0) {
	case "":
		title := a.Title + " · TUI v" + home.TUIVersion
		_, err = tea.NewProgram(home.New(title, a.Source, a.Refiner), tea.WithAltScreen()).Run()
		return err
	case "serve":
		return serve(a)
	}
	return fmt.Errorf("unknown command %q", flag.Arg(0))
}

// serve answers the desktop app until it closes refine's stdin.
func serve(a *app.App) error {
	srv := server.New(a.Title, a.Source, a.Refiner)
	srv.Site = a.Site
	srv.Tracking = a.Tracker.On()
	defer srv.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	hello, err := json.Marshal(map[string]any{"port": ln.Addr().(*net.TCPAddr).Port, "token": srv.Token})
	if err != nil {
		return err
	}
	fmt.Println(string(hello))

	httpSrv := &http.Server{Handler: srv.Handler()}
	go func() {
		_, _ = io.Copy(io.Discard, os.Stdin)
		_ = httpSrv.Shutdown(context.Background())
	}()
	if err := httpSrv.Serve(ln); err != http.ErrServerClosed {
		return err
	}
	return nil
}
