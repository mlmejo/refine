// Package gitsync tells whether a repo's checked-out branch is behind origin,
// so Claude does not read stale code, and fast-forwards it on request.
package gitsync

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// fetchTimeout bounds the fetch, so an unreachable origin reads as offline
// instead of holding up the draft.
const fetchTimeout = 15 * time.Second

// Status is how a repo's branch compares with origin. Behind is set only when
// it can be pulled; Skip says why it was not compared or cannot be pulled.
type Status struct {
	Name   string
	Dir    string
	Branch string
	Behind int
	// Origin is the commit origin/<Branch> is at, so a declined pull is not
	// asked about again until origin moves.
	Origin string
	Skip   string
}

// Check fetches origin and compares the repo with origin/<branch> when branch
// is checked out. A repo on another branch comes back as zero, since Claude
// reads that branch on purpose.
func Check(ctx context.Context, name, dir, branch string) Status {
	s := Status{Name: name, Dir: dir, Branch: branch}
	current, err := git(ctx, dir, "branch", "--show-current")
	if err != nil || current != branch {
		return s
	}

	fetchCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	if _, err := git(fetchCtx, dir, "fetch", "--quiet", "origin", branch); err != nil {
		s.Skip = fmt.Sprintf("could not fetch origin (offline?), so %s is read as it is", branch)
		return s
	}

	upstream := "refs/remotes/origin/" + branch
	s.Origin, err = git(ctx, dir, "rev-parse", "--verify", "--quiet", upstream+"^{commit}")
	if err != nil {
		s.Skip = "origin has no " + branch + " branch"
		return s
	}
	counts, err := git(ctx, dir, "rev-list", "--left-right", "--count", "HEAD..."+upstream)
	if err != nil {
		s.Skip = err.Error()
		return s
	}
	ahead, behind, err := parseCounts(counts)
	if err != nil || behind == 0 {
		return s
	}

	switch {
	case ahead > 0:
		s.Skip = fmt.Sprintf("%s has %d local commits and is %d behind origin, so it cannot fast-forward; reading it as it is", branch, ahead, behind)
	case dirty(ctx, dir):
		s.Skip = fmt.Sprintf("%s is %d commits behind origin but has uncommitted changes, so it is read as it is", branch, behind)
	default:
		s.Behind = behind
	}
	return s
}

// Pull fast-forwards the checked-out branch to the origin commit Check saw.
func Pull(ctx context.Context, s Status) error {
	_, err := git(ctx, s.Dir, "merge", "--ff-only", "--quiet", s.Origin)
	return err
}

func parseCounts(out string) (int, int, error) {
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("unexpected rev-list output %q", out)
	}
	ahead, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, err
	}
	behind, err := strconv.Atoi(fields[1])
	return ahead, behind, err
}

// dirty reports tracked changes; untracked files do not stop a fast-forward
// unless origin adds the same path, and then the merge says so.
func dirty(ctx context.Context, dir string) bool {
	out, err := git(ctx, dir, "status", "--porcelain", "--untracked-files=no")
	return err != nil || out != ""
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	// The TUI owns the terminal, so git must fail rather than ask for a password.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if os.Getenv("GIT_SSH_COMMAND") == "" {
		cmd.Env = append(cmd.Env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s in %s: %w: %s", args[0], filepath.Base(dir), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
