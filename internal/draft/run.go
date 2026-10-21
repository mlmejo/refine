package draft

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Runner runs headless Claude Code with read-only access to the repos.
type Runner struct {
	Bin   string
	Dir   string
	Repos map[string]string
}

// Result is a finished draft with the session it came from.
type Result struct {
	Draft     Draft
	SessionID string
	CostUSD   float64
	Turns     int
	Usage     Usage
}

// Usage is the tokens one run spent.
type Usage struct {
	Input      int `json:"input_tokens"`
	CacheWrite int `json:"cache_creation_input_tokens"`
	CacheRead  int `json:"cache_read_input_tokens"`
	Output     int `json:"output_tokens"`
}

func (r Runner) args(prompt, session string) []string {
	return r.command(prompt, session, questionRules, questionsSchema)
}

func (r Runner) command(prompt, session, system, shape string) []string {
	var args []string
	if session != "" {
		args = append(args, "--resume", session)
	}
	args = append(args,
		"-p", prompt,
		"--append-system-prompt", system,
		"--json-schema", shape,
		"--output-format", "json",
		"--allowedTools", "Read", "Grep", "Glob",
		"--strict-mcp-config",
	)
	for _, name := range repoNames(r.Repos) {
		args = append(args, "--add-dir", r.Repos[name])
	}
	return args
}

// Run drafts a refinement for the prompt.
func (r Runner) Run(ctx context.Context, prompt string) (Result, error) {
	return r.draft(ctx, r.args(prompt, ""))
}

// Resume continues the session that wrote an earlier draft, which already
// holds the code it read.
func (r Runner) Resume(ctx context.Context, session, prompt string) (Result, error) {
	return r.draft(ctx, r.args(prompt, session))
}

// FromBrief drafts in a fork of the brief's session, which already holds the
// code it read, and leaves the brief's own session as it was.
func (r Runner) FromBrief(ctx context.Context, session, prompt string) (Result, error) {
	return r.draft(ctx, append(r.args(withRules(questionRules, prompt), session), "--fork-session"))
}

// withRules puts a step's rules in the prompt. A resumed session keeps the
// system prompt it started with and ignores --append-system-prompt, so a
// session that moves on to another step only sees that step's rules here.
func withRules(rules, prompt string) string {
	return "Your instructions for this step replace any earlier ones:\n\n" + rules + "\n---\n\n" + prompt
}

// Brief explains the area the ticket touches, for a developer new to it.
func (r Runner) Brief(ctx context.Context, prompt string) (BriefResult, error) {
	var brief Brief
	run, err := r.run(ctx, r.command(prompt, "", briefRules, briefSchema), &brief)
	return BriefResult{Brief: brief, Run: run}, err
}

// Triage judges the ticket from Jira alone, with no repos and no tools.
func (r Runner) Triage(ctx context.Context, prompt string) (TriageResult, error) {
	var triage Triage
	args := []string{
		"-p", prompt,
		"--append-system-prompt", triageRules,
		"--json-schema", triageSchema,
		"--output-format", "json",
		"--tools", "",
		"--strict-mcp-config",
	}
	run, err := r.run(ctx, args, &triage)
	return TriageResult{Triage: triage, Run: run}, err
}

// Stamp writes up the ticket in a fork of the questions session, which holds
// the code it read and the answers, and leaves that session as it was.
func (r Runner) Stamp(ctx context.Context, session, prompt string) (Stamp, Run, error) {
	var stamp Stamp
	args := append(r.command(withRules(stampRules, prompt), session, stampRules, stampSchema), "--fork-session")
	run, err := r.run(ctx, args, &stamp)
	return stamp, run, err
}

func (r Runner) draft(ctx context.Context, args []string) (Result, error) {
	var draft Draft
	run, err := r.run(ctx, args, &draft)
	return Result{Draft: draft, SessionID: run.SessionID, CostUSD: run.CostUSD, Turns: run.Turns, Usage: run.Usage}, err
}

// Run is what one claude invocation cost, and the session it left.
type Run struct {
	SessionID string
	CostUSD   float64
	Turns     int
	Usage     Usage
}

func (r Runner) run(ctx context.Context, args []string, into any) (Run, error) {
	bin := r.Bin
	if bin == "" {
		bin = "claude"
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = r.Dir
	// Load each repo's CLAUDE.md, which --add-dir skips by default: it says
	// what the repo is, such as a rewrite whose original is the spec.
	cmd.Env = append(os.Environ(), "CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=1")
	cmd.WaitDelay = 5 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	err := cmd.Run()
	if ctx.Err() != nil {
		return Run{}, ctx.Err()
	}
	if err != nil && stdout.Len() == 0 {
		return Run{}, fmt.Errorf("claude: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return parse(stdout.Bytes(), into)
}

// parse reads claude's JSON output, decoding its structured output into into.
func parse(output []byte, into any) (Run, error) {
	var out struct {
		Subtype          string          `json:"subtype"`
		IsError          bool            `json:"is_error"`
		Result           string          `json:"result"`
		SessionID        string          `json:"session_id"`
		TotalCostUSD     float64         `json:"total_cost_usd"`
		NumTurns         int             `json:"num_turns"`
		Usage            Usage           `json:"usage"`
		StructuredOutput json.RawMessage `json:"structured_output"`
	}
	if err := json.Unmarshal(output, &out); err != nil {
		return Run{}, fmt.Errorf("claude output: %w", err)
	}
	if out.IsError || len(out.StructuredOutput) == 0 || string(out.StructuredOutput) == "null" {
		msg := strings.TrimSpace(out.Result)
		if msg == "" {
			msg = "no structured output returned"
		}
		return Run{}, errors.New("claude " + out.Subtype + ": " + msg)
	}
	if err := json.Unmarshal(out.StructuredOutput, into); err != nil {
		return Run{}, fmt.Errorf("claude structured output: %w", err)
	}
	return Run{SessionID: out.SessionID, CostUSD: out.TotalCostUSD, Turns: out.NumTurns, Usage: out.Usage}, nil
}
