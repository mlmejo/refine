// Package gitsync fetches the branch Claude reads in each repo: develop when
// origin has it, else origin's default branch. Claude reads the fetched
// commit in a worktree, so the developer's checkout is never touched.
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

// Preferred is the branch Claude reads when origin has it.
const Preferred = "develop"

// fetchTimeout bounds each call to origin, so an unreachable origin fails
// fast instead of holding up the refinement.
const fetchTimeout = 20 * time.Second

// Status is a repo fetched for reading: the branch, the commit Claude reads,
// and how many commits the fetch brought in. Fetch leaves Error empty; the
// caller sets it when it stores a failed fetch, and a repo with an error is
// not used.
type Status struct {
	Name   string `json:"name"`
	Dir    string `json:"dir"`
	Branch string `json:"branch"`
	Commit string `json:"commit"`
	New    int    `json:"new"`
	Error  string `json:"error,omitempty"`
}

// Fetch picks the repo's branch and fetches it from origin. On error the
// status holds what was learnt before the failure.
func Fetch(ctx context.Context, name, dir string) (Status, error) {
	s := Status{Name: name, Dir: dir}
	b, err := pickBranch(ctx, dir)
	if err != nil {
		return s, err
	}
	s.Branch = b
	upstream := "refs/remotes/origin/" + b
	before, _ := git(ctx, dir, "rev-parse", "--verify", "--quiet", upstream+"^{commit}")

	fetchCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	if _, err := git(fetchCtx, dir, "fetch", "--quiet", "origin", "+refs/heads/"+b+":"+upstream); err != nil {
		return s, fmt.Errorf("could not fetch %s from origin: %w", b, err)
	}
	if s.Commit, err = git(ctx, dir, "rev-parse", "--verify", "--quiet", upstream+"^{commit}"); err != nil {
		return s, fmt.Errorf("origin has no %s branch: %w", b, err)
	}
	if before != "" && before != s.Commit {
		if count, err := git(ctx, dir, "rev-list", "--count", before+".."+s.Commit); err == nil {
			if s.New, err = strconv.Atoi(count); err != nil {
				return s, fmt.Errorf("could not count the new commits on %s: %w", b, err)
			}
		}
	}
	return s, nil
}

// pickBranch is develop when origin has it, else origin's default branch.
func pickBranch(ctx context.Context, dir string) (string, error) {
	listCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	out, err := git(listCtx, dir, "ls-remote", "--symref", "origin", "HEAD", "refs/heads/"+Preferred)
	if err != nil {
		return "", fmt.Errorf("could not reach origin: %w", err)
	}
	var fallback string
	for line := range strings.SplitSeq(out, "\n") {
		fields := strings.Fields(line)
		switch {
		case len(fields) == 2 && fields[1] == "refs/heads/"+Preferred:
			return Preferred, nil
		case len(fields) == 3 && fields[0] == "ref:" && fields[2] == "HEAD":
			fallback = strings.TrimPrefix(fields[1], "refs/heads/")
		}
	}
	if fallback == "" {
		return "", fmt.Errorf("origin has no %s branch and no default branch", Preferred)
	}
	return fallback, nil
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	// No one can answer a password prompt, so git must fail instead.
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
