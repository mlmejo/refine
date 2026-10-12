package draft

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mlmejo/refine/internal/jira"
	"github.com/mlmejo/refine/internal/outline"
)

func TestPromptDescribesTicketReposAndApproach(t *testing.T) {
	points := 5.0
	ticket := jira.Ticket{
		Issue:       jira.Issue{Key: "GEEK-7", Summary: "SDK", Status: "To Do", StoryPointCriteria: "Total: High"},
		Description: "APIs cannot talk to StarVault",
		Points:      &points,
		Comments:    []jira.Comment{{Author: "PO", Body: "D23 to D25"}},
	}
	repos := map[string]string{"dating": "/r/dating", "dating-backend": "/r/dating-backend"}

	got := Prompt(ticket, repos, nil, "", "  Install the SDK with composer.  ")
	for _, want := range []string{
		"Ticket GEEK-7: SDK",
		"Status: To Do. Story points: 5.",
		"Description:\nAPIs cannot talk to StarVault",
		"Acceptance Criteria: empty.",
		"Story Point Criteria:\nTotal: High",
		`- PO: "D23 to D25"`,
		"- dating: /r/dating\n- dating-backend: /r/dating-backend",
		"Developer's approach, written before seeing yours. It sets the scope:\n\"Install the SDK with composer.\"",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing %q:\n%s", want, got)
		}
	}

	if got := Prompt(ticket, nil, nil, "", " "); !strings.Contains(got, "does not know yet") {
		t.Errorf("empty approach prompt:\n%s", got)
	}
}

func TestPromptIncludesOutlines(t *testing.T) {
	outlines := map[string]outline.Outline{"dating": {Commit: "0123456789abcdef", Text: "plugins/ 18\n"}}
	got := Prompt(jira.Ticket{}, map[string]string{"dating": "/r/dating"}, outlines, "", "")
	if !strings.Contains(got, "Use them instead of listing directories:\n\ndating at 0123456789ab:\nplugins/ 18\n") {
		t.Errorf("prompt:\n%s", got)
	}
}

func TestPromptNamesTheRef(t *testing.T) {
	got := Prompt(jira.Ticket{}, map[string]string{"dating": "/w/dating"}, nil, "develop", "")
	if !strings.Contains(got, "checked out at their develop branch:\n- dating: /w/dating") {
		t.Errorf("prompt:\n%s", got)
	}
}

func TestArgsAreReadOnlyAndAddEveryRepo(t *testing.T) {
	args := strings.Join(Runner{Repos: map[string]string{"b": "/r/b", "a": "/r/a"}}.Args("hi"), " ")
	for _, want := range []string{"-p hi", "--allowedTools Read Grep Glob", "--strict-mcp-config", "--output-format json", "--add-dir /r/a --add-dir /r/b"} {
		if !strings.Contains(args, want) {
			t.Errorf("args missing %q: %s", want, args)
		}
	}
}

// fakeClaude writes a script that records its cwd and prints output.
func fakeClaude(t *testing.T, output string, exit int) (bin, cwdFile string) {
	t.Helper()
	dir := t.TempDir()
	bin = filepath.Join(dir, "claude")
	cwdFile = filepath.Join(dir, "cwd")
	outFile := filepath.Join(dir, "out.json")
	if err := os.WriteFile(outFile, []byte(output), 0o600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\npwd -P > '" + cwdFile + "'\ncat '" + outFile + "'\nexit " + string(rune('0'+exit)) + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return bin, cwdFile
}

func TestRunParsesStructuredOutput(t *testing.T) {
	bin, cwdFile := fakeClaude(t, `{"type":"result","subtype":"success","is_error":false,
		"session_id":"s-1","total_cost_usd":0.87,"num_turns":28,
		"usage":{"input_tokens":24,"cache_creation_input_tokens":68985,"cache_read_input_tokens":712326,"output_tokens":8974},
		"structured_output":{"acceptance_criteria":["QA sees it."],"points":3,
			"technical_implementation":[{"repo":"dating","files":[{"path":"a.vue","new":true}]}],
			"story_point_criteria":{"total":"Medium"},"split":[],
			"approach_review":{"holds":["ok"],"misses":[],"alternatives":[]},"open_questions":["How is it deployed?"]}}`, 0)
	dir := t.TempDir()

	got, err := Runner{Bin: bin, Dir: dir}.Run(context.Background(), "prompt")
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionID != "s-1" || got.CostUSD != 0.87 || got.Draft.Points != 3 || got.Turns != 28 || got.Usage != (Usage{Input: 24, CacheWrite: 68985, CacheRead: 712326, Output: 8974}) {
		t.Errorf("result = %+v", got)
	}
	if got.Draft.TechnicalImplementation[0].Files[0] != (File{Path: "a.vue", New: true}) || got.Draft.OpenQuestions[0] != "How is it deployed?" {
		t.Errorf("draft = %+v", got.Draft)
	}
	cwd, _ := os.ReadFile(cwdFile)
	if resolved, _ := filepath.EvalSymlinks(dir); strings.TrimSpace(string(cwd)) != resolved {
		t.Errorf("cwd = %q, want %q", cwd, resolved)
	}
}

func TestRunReportsClaudeErrors(t *testing.T) {
	bin, _ := fakeClaude(t, `{"type":"result","subtype":"error_max_turns","is_error":true,"result":"Reached max turns"}`, 1)
	_, err := Runner{Bin: bin, Dir: t.TempDir()}.Run(context.Background(), "prompt")
	if err == nil || !strings.Contains(err.Error(), "error_max_turns: Reached max turns") {
		t.Errorf("err = %v", err)
	}

	_, err = Runner{Bin: filepath.Join(t.TempDir(), "missing"), Dir: t.TempDir()}.Run(context.Background(), "prompt")
	if err == nil {
		t.Error("missing binary: no error")
	}
}

func TestStoreRoundTrip(t *testing.T) {
	store := Store{Dir: filepath.Join(t.TempDir(), "drafts")}
	if _, ok, err := store.Load("GEEK-7"); ok || err != nil {
		t.Fatalf("empty load ok = %v err = %v", ok, err)
	}

	saved := Saved{Key: "GEEK-7", Approach: "use the SDK", SessionID: "s-1", Created: time.Now().UTC().Truncate(time.Second),
		Draft: Draft{AcceptanceCriteria: []string{"QA sees it."}, Points: 2}}
	if err := store.Save(saved); err != nil {
		t.Fatal(err)
	}
	got, ok, err := store.Load("GEEK-7")
	if err != nil || !ok {
		t.Fatalf("load ok = %v err = %v", ok, err)
	}
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(saved)
	if string(a) != string(b) {
		t.Errorf("got %s\nwant %s", a, b)
	}
}

func TestSchemaIsValidJSON(t *testing.T) {
	if !json.Valid([]byte(schema)) || !strings.Contains(rules, "## Acceptance Criteria") {
		t.Error("embedded schema or rules broken")
	}
}

func TestResumeArgsContinueTheSession(t *testing.T) {
	args := Runner{Repos: map[string]string{"a": "/r/a"}}.args("revise", "sess-1")
	got := strings.Join(args, " ")
	if !strings.HasPrefix(got, "--resume sess-1 -p revise") || !strings.Contains(got, "--json-schema") || !strings.Contains(got, "--add-dir /r/a") {
		t.Errorf("args: %s", got)
	}
}

func TestQuestionsAndAnswers(t *testing.T) {
	saved := Saved{
		Draft: Draft{
			Deployment:    Deployment{Blockers: []string{"Which host deploys?"}},
			OpenQuestions: []string{"Keep the client event?", "Which queue?"},
		},
		Answers: map[string]string{"Which host deploys?": "Forge", "Keep the client event?": " "},
	}
	if got := saved.Draft.Questions(); strings.Join(got, "|") != "Which host deploys?|Keep the client event?|Which queue?" {
		t.Errorf("questions = %v", got)
	}
	if got := saved.Unanswered(); got != 2 {
		t.Errorf("unanswered = %d", got)
	}

	got := RevisePrompt(saved)
	if !strings.Contains(got, "- Q: Which host deploys?\n  A: \"Forge\"\n") || strings.Contains(got, "Keep the client event?") {
		t.Errorf("revise prompt:\n%s", got)
	}
	if AnswersPrompt(Saved{}) != "" {
		t.Error("answers prompt without answers should be empty")
	}

	md := Markdown(saved, "SDK")
	if !strings.Contains(md, "### Blockers\n\n- Which host deploys?\n  - **Answer:** Forge\n") || !strings.Contains(md, "- Which queue?\n") {
		t.Errorf("markdown:\n%s", md)
	}
}

func TestBriefAndFromBriefPrompts(t *testing.T) {
	context := Context(jira.Ticket{Issue: jira.Issue{Key: "GEEK-8", Summary: "Credits"}}, nil, nil, "")
	if got := BriefPrompt(context); !strings.HasPrefix(got, "Ticket GEEK-8: Credits") || !strings.Contains(got, "Brief the developer") || strings.Contains(got, "approach, written") {
		t.Errorf("brief prompt:\n%s", got)
	}
	if got := FromBriefPrompt("Reuse the ledger? not sure"); !strings.Contains(got, "read your brief") || !strings.Contains(got, `"Reuse the ledger? not sure"`) {
		t.Errorf("from brief prompt:\n%s", got)
	}
	if !json.Valid([]byte(briefSchema)) || !strings.Contains(briefRules, "Do not propose an approach") || !strings.Contains(rules, "are not misses") {
		t.Error("embedded brief schema or rules broken")
	}
}

func TestBriefRunsWithItsOwnSchema(t *testing.T) {
	bin, _ := fakeClaude(t, `{"type":"result","subtype":"success","session_id":"b-1","total_cost_usd":0.5,"num_turns":9,
		"structured_output":{"summary":"Credits are spent per message.","flow":["Send in a.vue"],"parts":[{"repo":"dating","path":"a.vue","role":"chat"}],
			"terms":[{"term":"credit","meaning":"paid unit"}],"gotchas":[],"ask_team":["Who owns billing?"]}}`, 0)
	got, err := Runner{Bin: bin, Dir: t.TempDir()}.Brief(context.Background(), "prompt")
	if err != nil {
		t.Fatal(err)
	}
	if got.Run.SessionID != "b-1" || got.Run.Turns != 9 || got.Brief.Parts[0].Path != "a.vue" || got.Brief.Terms[0].Meaning != "paid unit" {
		t.Errorf("brief = %+v", got)
	}
}

func TestFromBriefForksTheBriefSession(t *testing.T) {
	dir := t.TempDir()
	bin, argsFile := filepath.Join(dir, "claude"), filepath.Join(dir, "args")
	script := "#!/bin/sh\necho \"$@\" > '" + argsFile + "'\n" +
		`echo '{"type":"result","subtype":"success","session_id":"d-1","structured_output":{"points":2}}'` + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := Runner{Bin: bin, Dir: dir}.FromBrief(context.Background(), "b-1", "draft")
	if err != nil || got.SessionID != "d-1" || got.Draft.Points != 2 {
		t.Fatalf("result = %+v err %v", got, err)
	}
	args, _ := os.ReadFile(argsFile)
	if !strings.HasPrefix(string(args), "--resume b-1 -p draft") || !strings.Contains(string(args), "--fork-session") || !strings.Contains(string(args), `"acceptance_criteria"`) {
		t.Errorf("args: %s", args)
	}
}

func TestCreateSplitSkipsCreatedChildrenAndKeepsEachKey(t *testing.T) {
	saved := Saved{
		Key:      "GEEK-1",
		Draft:    Draft{Split: []Child{{Title: "A"}, {Title: "B"}, {Title: "C"}, {Title: "D"}}},
		Children: map[int]string{0: "GEEK-10"},
	}
	var created []string
	var kept []map[int]string
	create := func(child Child) (string, error) {
		created = append(created, child.Title)
		switch child.Title {
		case "B":
			return "GEEK-11", nil
		case "C":
			return "GEEK-12", errors.New("could not set its points")
		}
		return "", errors.New("unreachable")
	}
	keep := func(s Saved) error {
		kept = append(kept, s.Children)
		return nil
	}

	children, err := CreateSplit(saved, create, keep)
	if err == nil || err.Error() != "C: could not set its points" {
		t.Errorf("err = %v", err)
	}
	if strings.Join(created, ",") != "B,C" {
		t.Errorf("created %v; A was already created and D comes after the failure", created)
	}
	if fmt.Sprint(children) != "map[0:GEEK-10 1:GEEK-11 2:GEEK-12]" {
		t.Errorf("children = %v", children)
	}
	if len(kept) != 2 || fmt.Sprint(kept[1]) != fmt.Sprint(children) {
		t.Errorf("kept = %v", kept)
	}
	if len(saved.Children) != 1 {
		t.Errorf("saved.Children changed: %v", saved.Children)
	}
}
