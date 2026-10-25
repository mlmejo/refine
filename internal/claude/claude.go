// Package claude runs headless Claude Code for one refinement step: a prompt,
// the step's rules and output schema, and read-only access to the repos. It
// reports what Claude reads as it goes, so the app can show live progress.
package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Model is the model every run uses, pinned so a refinement does not change
// with the developer's Claude Code default.
const Model = "claude-opus-5-5"

// isolation keeps the developer's own Claude Code setup out of refine's
// runs: user settings bring their hooks and plugins (a style hook would
// rewrite every answer). Project and local settings, the repos' CLAUDE.md and
// AGENTS.md, stay: they are knowledge about the code.
var isolation = []string{"--model", Model, "--setting-sources", "project,local", "--strict-mcp-config"}

// Call is one run: the prompt, the step's rules and the JSON schema its
// answer must match. Repos are the directories Claude may read, by name;
// none means Claude gets no tools and judges from the prompt alone. Session
// resumes an earlier run, which keeps the code it read.
type Call struct {
	Prompt  string
	Rules   string
	Schema  string
	Repos   map[string]string
	Session string
}

// Run is what one invocation cost, and the session it left.
type Run struct {
	SessionID string  `json:"session_id"`
	CostUSD   float64 `json:"cost_usd"`
	Turns     int     `json:"turns"`
}

// Activity is one thing Claude did while it worked, such as reading a file.
type Activity struct {
	Verb string    `json:"verb"`
	What string    `json:"what"`
	At   time.Time `json:"at"`
}

// Runner runs the claude binary in Dir.
type Runner struct {
	// Bin is the claude binary; "" means claude on the PATH.
	Bin string
	Dir string
}

// Do runs the call and decodes Claude's answer into into. Each tool Claude
// uses is passed to progress as it happens; progress may be nil.
func (r Runner) Do(ctx context.Context, call Call, into any, progress func(Activity)) (Run, error) {
	bin := r.Bin
	if bin == "" {
		bin = "claude"
	}
	cmd := exec.CommandContext(ctx, bin, args(call)...)
	cmd.Dir = r.Dir
	// Load each repo's CLAUDE.md, which --add-dir skips by default: it says
	// what the repo is, such as a rewrite whose original is the spec.
	cmd.Env = append(os.Environ(), "CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=1")
	cmd.WaitDelay = 5 * time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Run{}, err
	}
	if err := cmd.Start(); err != nil {
		return Run{}, fmt.Errorf("claude: %w", err)
	}
	result, readErr := read(stdout, call.Repos, progress)
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return Run{}, ctx.Err()
	}
	if result == nil {
		if readErr == nil {
			readErr = waitErr
		}
		return Run{}, fmt.Errorf("claude: %v: %s", readErr, strings.TrimSpace(stderr.String()))
	}
	return decode(*result, into)
}

func args(call Call) []string {
	var out []string
	if call.Session != "" {
		out = append(out, "--resume", call.Session)
	}
	out = append(out,
		"-p", call.Prompt,
		"--append-system-prompt", call.Rules,
		"--json-schema", call.Schema,
		"--output-format", "stream-json", "--verbose",
	)
	if len(call.Repos) == 0 {
		out = append(out, "--tools", "")
	} else {
		out = append(out, "--allowedTools", "Read", "Grep", "Glob")
		for _, name := range names(call.Repos) {
			out = append(out, "--add-dir", call.Repos[name])
		}
	}
	return append(out, isolation...)
}

// event is one line of claude's stream-json output, with the fields refine
// reads.
type event struct {
	Type    string `json:"type"`
	Message struct {
		Content []struct {
			Type  string          `json:"type"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
	} `json:"message"`
	result
}

// result is the stream's last event: the answer and what it cost.
type result struct {
	Subtype          string          `json:"subtype"`
	IsError          bool            `json:"is_error"`
	Result           string          `json:"result"`
	SessionID        string          `json:"session_id"`
	TotalCostUSD     float64         `json:"total_cost_usd"`
	NumTurns         int             `json:"num_turns"`
	StructuredOutput json.RawMessage `json:"structured_output"`
}

// read follows the stream to its result, reporting each tool use.
func read(stream io.Reader, repos map[string]string, progress func(Activity)) (*result, error) {
	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 0, 1<<20), 64<<20)
	var last *result
	for scanner.Scan() {
		var e event
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			continue
		}
		switch e.Type {
		case "assistant":
			for _, c := range e.Message.Content {
				if c.Type != "tool_use" || progress == nil {
					continue
				}
				if a, ok := describe(c.Name, c.Input, repos); ok {
					a.At = time.Now()
					progress(a)
				}
			}
		case "result":
			r := e.result
			last = &r
		}
	}
	return last, scanner.Err()
}

// describe says what a tool use did in a few words, with paths shown
// relative to their repo.
func describe(tool string, input json.RawMessage, repos map[string]string) (Activity, bool) {
	var in struct {
		FilePath string `json:"file_path"`
		Pattern  string `json:"pattern"`
		Path     string `json:"path"`
	}
	_ = json.Unmarshal(input, &in)
	switch tool {
	case "Read":
		return Activity{Verb: "Read", What: relative(in.FilePath, repos)}, true
	case "Grep":
		what := fmt.Sprintf("%q", in.Pattern)
		if in.Path != "" {
			what += " in " + relative(in.Path, repos)
		}
		return Activity{Verb: "Searched", What: what}, true
	case "Glob":
		what := in.Pattern
		if in.Path != "" {
			what = relative(in.Path, repos) + " " + what
		}
		return Activity{Verb: "Listed", What: what}, true
	case "StructuredOutput":
		return Activity{Verb: "Writing", What: "the answer"}, true
	}
	return Activity{}, false
}

// relative shows path as <repo>/<path inside it> when it is in a repo.
func relative(path string, repos map[string]string) string {
	for _, name := range names(repos) {
		dir := filepath.Clean(repos[name])
		if path == dir {
			return name
		}
		if rest, ok := strings.CutPrefix(path, dir+string(filepath.Separator)); ok {
			return name + "/" + rest
		}
	}
	return path
}

// decode reads the result's structured answer into into.
func decode(r result, into any) (Run, error) {
	if r.IsError || len(r.StructuredOutput) == 0 || string(r.StructuredOutput) == "null" {
		msg := strings.TrimSpace(r.Result)
		if msg == "" {
			msg = "no structured output returned"
		}
		return Run{}, errors.New("claude " + r.Subtype + ": " + msg)
	}
	if err := json.Unmarshal(r.StructuredOutput, into); err != nil {
		return Run{}, fmt.Errorf("claude structured output: %w", err)
	}
	return Run{SessionID: r.SessionID, CostUSD: r.TotalCostUSD, Turns: r.NumTurns}, nil
}

func names(repos map[string]string) []string {
	out := make([]string, 0, len(repos))
	for name := range repos {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}
