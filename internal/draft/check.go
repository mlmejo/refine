package draft

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/mlmejo/refine/internal/jira"
)

//go:embed check.md
var checkRules string

//go:embed check.schema.json
var checkSchema string

// Check is the last step before writing: the stamped criteria compared with
// the other open tickets, judged from the text alone.
type Check struct {
	Duplicates []Duplicate `json:"duplicates"`
}

// Duplicate is a criterion of this ticket or one of its split tickets that
// another open ticket already delivers.
type Duplicate struct {
	// Ticket is this ticket's key, or the split ticket's title.
	Ticket    string `json:"ticket"`
	Criterion string `json:"criterion"`
	Key       string `json:"key"`
	// Covered quotes where the other ticket delivers it.
	Covered string `json:"covered"`
}

// WithCheck is the draft with the check's findings.
func (d Draft) WithCheck(c Check) Draft {
	d.Checked, d.Duplicates = true, c.Duplicates
	return d
}

// CheckPrompt lists the stamped criteria of the ticket and its split
// tickets, then the other open tickets to compare them with.
func CheckPrompt(saved Saved, others []jira.Ticket) string {
	var out strings.Builder
	d := saved.Draft
	fmt.Fprintf(&out, "Ticket %s, about to be written to Jira.\n", saved.Key)
	criteria(&out, "Acceptance Criteria", d.AcceptanceCriteria)
	for _, child := range d.Split {
		criteria(&out, fmt.Sprintf("Split ticket %q, Acceptance Criteria", child.Title), child.AcceptanceCriteria)
	}
	list := othersList("\nOther open tickets:\n", others, ownKeys(saved)...)
	if list == "" {
		list = "\nOther open tickets: none.\n"
	}
	out.WriteString(list)
	out.WriteString("\nCheck the criteria as your instructions say.\n")
	return out.String()
}

func criteria(out *strings.Builder, name string, items []string) {
	fmt.Fprintf(out, "\n%s:\n", name)
	if len(items) == 0 {
		out.WriteString("none\n")
	}
	for _, item := range items {
		fmt.Fprintf(out, "* %s\n", item)
	}
}

// Check compares the stamped criteria with the other open tickets, with no
// repos and no tools.
func (r Runner) Check(ctx context.Context, prompt string) (Check, Run, error) {
	var check Check
	args := []string{
		"-p", prompt,
		"--append-system-prompt", checkRules,
		"--json-schema", checkSchema,
		"--output-format", "json",
		"--tools", "",
		"--strict-mcp-config",
	}
	run, err := r.run(ctx, args, &check)
	return check, run, err
}
