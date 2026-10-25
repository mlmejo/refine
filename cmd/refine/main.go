// Command refine serves the desktop app. refine serve listens on localhost,
// prints {"port":…,"token":…} as its first line, and exits when its stdin
// closes. refine version prints the build.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"

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

func run(args []string) error {
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
		defer a.Close()
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

	httpSrv := &http.Server{Handler: a.Server.Handler()}
	go func() {
		_, _ = io.Copy(io.Discard, os.Stdin)
		_ = httpSrv.Shutdown(context.Background())
	}()
	if err := httpSrv.Serve(ln); err != http.ErrServerClosed {
		return err
	}
	return nil
}
