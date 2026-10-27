package gitcmd

import (
	"context"
	"strings"
	"testing"
)

// env runs git's env through a shell alias, so it shows the environment git
// runs with.
func env(t *testing.T) string {
	t.Helper()
	out, err := Run(context.Background(), t.TempDir(), "-c", "alias.env=!env", "env")
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRunNeverPrompts(t *testing.T) {
	t.Setenv("GIT_SSH_COMMAND", "")
	out := env(t)
	if !strings.Contains(out, "GIT_TERMINAL_PROMPT=0\n") {
		t.Errorf("no GIT_TERMINAL_PROMPT=0 in\n%s", out)
	}
	if !strings.Contains(out, "GIT_SSH_COMMAND=ssh -o BatchMode=yes\n") {
		t.Errorf("no BatchMode in\n%s", out)
	}
}

func TestRunKeepsTheDevelopersSSHCommand(t *testing.T) {
	t.Setenv("GIT_SSH_COMMAND", "ssh -i key")
	if out := env(t); !strings.Contains(out, "GIT_SSH_COMMAND=ssh -i key\n") {
		t.Errorf("ssh command replaced in\n%s", out)
	}
}

func TestRunTrimsOutputAndReportsStderr(t *testing.T) {
	dir := t.TempDir()
	if out, err := Run(context.Background(), dir, "init", "--quiet"); err != nil || out != "" {
		t.Fatalf("init = %q, %v", out, err)
	}
	if out, err := Run(context.Background(), dir, "rev-parse", "--is-inside-work-tree"); err != nil || out != "true" {
		t.Errorf("rev-parse = %q, %v", out, err)
	}
	_, err := Run(context.Background(), dir, "rev-parse", "--verify", "nope")
	if err == nil || !strings.Contains(err.Error(), "git rev-parse in ") || !strings.Contains(err.Error(), "Needed a single revision") {
		t.Errorf("err = %v", err)
	}
}
