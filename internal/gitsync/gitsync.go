// Package gitsync fetches the branch Claude reads in each repo: develop when
// origin has it, else origin's default branch. Claude reads the fetched
// commit in a worktree, so the developer's checkout is never touched.
package gitsync

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mlmejo/refine/internal/gitcmd"
)

// Preferred is the branch Claude reads when origin has it.
const Preferred = "develop"

// fetchTimeout bounds each call to origin, so an unreachable origin fails
// fast instead of holding up the refinement.
const fetchTimeout = 20 * time.Second

// Status is a repo fetched for reading: the branch, the commit Claude reads,
// and how many commits the fetch brought in. Error says why the repo cannot
// be read; a repo with an error is not used.
type Status struct {
	Name   string `json:"name"`
	Dir    string `json:"dir"`
	Branch string `json:"branch"`
	Commit string `json:"commit"`
	New    int    `json:"new"`
	Error  string `json:"error,omitempty"`
}

// Fetch picks the repo's branch and fetches it from origin.
func Fetch(ctx context.Context, name, dir string) Status {
	s := Status{Name: name, Dir: dir}
	branch, err := branch(ctx, dir)
	if err != nil {
		s.Error = err.Error()
		return s
	}
	s.Branch = branch
	upstream := "refs/remotes/origin/" + branch
	before, _ := gitcmd.Run(ctx, dir, "rev-parse", "--verify", "--quiet", upstream+"^{commit}")

	fetchCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	if _, err := gitcmd.Run(fetchCtx, dir, "fetch", "--quiet", "origin", "+refs/heads/"+branch+":"+upstream); err != nil {
		s.Error = "could not fetch " + branch + " from origin: " + err.Error()
		return s
	}
	if s.Commit, err = gitcmd.Run(ctx, dir, "rev-parse", "--verify", "--quiet", upstream+"^{commit}"); err != nil {
		s.Error = "origin has no " + branch + " branch"
		return s
	}
	if before != "" && before != s.Commit {
		if count, err := gitcmd.Run(ctx, dir, "rev-list", "--count", before+".."+s.Commit); err == nil {
			s.New, _ = strconv.Atoi(count)
		}
	}
	return s
}

// branch is develop when origin has it, else origin's default branch.
func branch(ctx context.Context, dir string) (string, error) {
	listCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	out, err := gitcmd.Run(listCtx, dir, "ls-remote", "--symref", "origin", "HEAD", "refs/heads/"+Preferred)
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
