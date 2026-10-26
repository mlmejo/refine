package claude

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeClaude writes a script that prints stream as claude's stdout.
func fakeClaude(t *testing.T, stream string) string {
	t.Helper()
	dir := t.TempDir()
	data := filepath.Join(dir, "stream.jsonl")
	if err := os.WriteFile(data, []byte(stream), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "claude")
	script := "#!/bin/sh\necho \"$@\" > " + filepath.Join(dir, "args") + "\ncat " + data + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return bin
}

const stream = `{"type":"system","subtype":"init"}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/w/dating-backend/app/Refund.php"}}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Grep","input":{"pattern":"refund","path":"/w/admin/src"}}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"thinking aloud"}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"StructuredOutput","input":{"n":1}}]}}
{"type":"result","subtype":"success","session_id":"s1","total_cost_usd":0.5,"num_turns":4,"structured_output":{"n":1}}
`

func TestDoReportsToolsAndDecodesTheAnswer(t *testing.T) {
	bin := fakeClaude(t, stream)
	var got []Activity
	var out struct{ N int }
	run, err := Runner{Bin: bin, Dir: t.TempDir()}.Do(context.Background(), Call{
		Prompt: "p", Rules: "r", Schema: "{}",
		Repos: map[string]string{"dating-backend": "/w/dating-backend", "admin": "/w/admin"},
	}, &out, func(a Activity) { got = append(got, a) })
	if err != nil {
		t.Fatal(err)
	}
	if out.N != 1 || run.SessionID != "s1" || run.CostUSD != 0.5 || run.Turns != 4 {
		t.Fatalf("out = %+v, run = %+v", out, run)
	}
	var lines []string
	for _, a := range got {
		lines = append(lines, a.Verb+" "+a.What)
	}
	want := []string{`Read dating-backend/app/Refund.php`, `Searched "refund" in admin/src`, `Writing the answer`}
	if !slices.Equal(lines, want) {
		t.Fatalf("activity = %q", lines)
	}
}

func TestDoFailsWithoutAnAnswer(t *testing.T) {
	bin := fakeClaude(t, `{"type":"result","subtype":"error_max_turns","is_error":true,"result":"ran out of turns"}`+"\n")
	var out struct{}
	_, err := Runner{Bin: bin}.Do(context.Background(), Call{Prompt: "p"}, &out, nil)
	if err == nil || !strings.Contains(err.Error(), "ran out of turns") {
		t.Fatalf("err = %v", err)
	}
}

// fakeScript writes body as a claude binary.
func fakeScript(t *testing.T, body string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestDoWrapsTheExitError(t *testing.T) {
	bin := fakeScript(t, "echo 'not logged in' >&2\nexit 3\n")
	var out struct{}
	_, err := Runner{Bin: bin}.Do(context.Background(), Call{Prompt: "p"}, &out, nil)
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 3 {
		t.Fatalf("err = %v, want an *exec.ExitError with code 3", err)
	}
	if !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("err = %v, want claude's stderr", err)
	}
}

func TestDoReportsAReadErrorAfterTheResult(t *testing.T) {
	result := `{"type":"result","subtype":"success","structured_output":{}}`
	bin := fakeScript(t, "echo '"+result+"'\nhead -c 67108865 /dev/zero | tr '\\0' a\n")
	var out struct{}
	_, err := Runner{Bin: bin}.Do(context.Background(), Call{Prompt: "p"}, &out, nil)
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("err = %v, want bufio.ErrTooLong", err)
	}
}

func TestDecodeNamesTheSubtype(t *testing.T) {
	_, err := decode(result{Subtype: "error_during_execution", IsError: true}, &struct{}{})
	if err == nil || err.Error() != "claude error_during_execution: no structured output returned" {
		t.Fatalf("err = %v", err)
	}
}

func TestArgsWithoutReposGiveNoTools(t *testing.T) {
	got := strings.Join(args(Call{Prompt: "p", Rules: "r", Schema: "s"}), " ")
	if !strings.Contains(got, "--tools ") || strings.Contains(got, "--allowedTools") || strings.Contains(got, "--resume") {
		t.Fatalf("args = %s", got)
	}
	got = strings.Join(args(Call{Prompt: "p", Session: "s1", Repos: map[string]string{"b": "/b", "a": "/a"}}), " ")
	if !strings.Contains(got, "--resume s1") || !strings.Contains(got, "--add-dir /a --add-dir /b") {
		t.Fatalf("args = %s", got)
	}
}
