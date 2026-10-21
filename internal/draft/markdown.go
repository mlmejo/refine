package draft

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mlmejo/refine/internal/jira"
)

// Rows pairs each criterion with its label as Jira's Story Point Criteria field writes it.
func (c StoryPointCriteria) Rows() [][2]string {
	return [][2]string{
		{"Novelty & Complexity of Implementation", c.Novelty},
		{"Risk & Feature Impact", c.Risk},
		{"Dependency & Integration Complexity", c.Dependency},
		{"Testing & QA Effort", c.Testing},
		{"Deployment Complexity", c.Deployment},
		{"Total", c.Total},
	}
}

// Refinement is the draft as Jira's refinement fields take it. The fields
// render plain text, so nothing here is markdown: a backtick would show as is.
func (d Draft) Refinement() jira.Refinement {
	return refinement(d.AcceptanceCriteria, d.TechnicalImplementation, d.StoryPointCriteria, d.Points)
}

// Refinement is the split ticket as Jira's refinement fields take it. A child
// from a draft made before children were rated has no Story Point Criteria.
func (c Child) Refinement() jira.Refinement {
	return refinement(c.AcceptanceCriteria, c.TechnicalImplementation, c.StoryPointCriteria, c.Points)
}

// Rated reports whether Claude rated these criteria.
func (c StoryPointCriteria) Rated() bool {
	return c.Total != ""
}

// SplitDescription is a split ticket's description, pointing back at its parent.
func SplitDescription(parent, summary string) string {
	return fmt.Sprintf("Split from %s (%s), which holds the foundation this builds on.", parent, summary)
}

func refinement(criteria []string, repos []RepoFiles, rating StoryPointCriteria, points int) jira.Refinement {
	var ac strings.Builder
	for _, item := range criteria {
		ac.WriteString("* " + item + "\n")
	}

	var ti strings.Builder
	for i, repo := range repos {
		if i > 0 {
			ti.WriteString("\n")
		}
		ti.WriteString(repo.Repo + "\n\n")
		for _, file := range repo.Files {
			line := "* " + file.Path
			if file.New {
				line += " (new)"
			}
			ti.WriteString(line + "\n")
		}
	}

	var spc []string
	if rating.Rated() {
		for _, row := range rating.Rows() {
			spc = append(spc, row[0]+": "+row[1])
		}
	}

	return jira.Refinement{
		AcceptanceCriteria:      strings.TrimSpace(ac.String()),
		TechnicalImplementation: strings.TrimSpace(ti.String()),
		StoryPointCriteria:      strings.Join(spc, "\n"),
		Points:                  points,
	}
}

// Markdown writes the draft as a document; AC and TI follow the Jira field layout.
func Markdown(saved Saved, summary string) string {
	d := saved.Draft
	var out strings.Builder
	if summary == "" {
		fmt.Fprintf(&out, "# %s\n\n", saved.Key)
	} else {
		fmt.Fprintf(&out, "# %s: %s\n\n", saved.Key, summary)
	}
	meta := fmt.Sprintf("Drafted %s · %d pts", saved.Created.Local().Format("2006-01-02 15:04"), d.Points)
	if !saved.Stamped() {
		meta = fmt.Sprintf("Questions %s · not stamped yet", saved.Created.Local().Format("2006-01-02 15:04"))
	}
	if saved.Ref != "" {
		meta += " · repos at " + saved.Ref
	}
	out.WriteString(meta + "\n")

	if d.Problem != "" {
		out.WriteString("\n## Ticket\n\n")
		fmt.Fprintf(&out, "**Problem:** %s\n\n**Solution:** %s\n", d.Problem, d.Solution)
		out.WriteString("\n### Blockers\n\n")
		bullets(&out, "-", d.TicketBlockers)
	}

	out.WriteString("\n## Your approach\n\n")
	if approach := strings.TrimSpace(saved.Approach); approach == "" {
		out.WriteString("_Not known yet._\n")
	} else {
		out.WriteString("> " + strings.ReplaceAll(approach, "\n", "\n> ") + "\n")
	}
	if scope := ScopePrompt(saved.Scope); scope != "" {
		out.WriteString("\n## Scope answers\n\n")
		for _, question := range slices.Sorted(maps.Keys(saved.Scope)) {
			if answer := strings.TrimSpace(saved.Scope[question]); answer != "" {
				fmt.Fprintf(&out, "- %s **%s**\n", question, answer)
			}
		}
	}

	if len(d.Findings) > 0 || !saved.Stamped() {
		out.WriteString("\n## Findings\n\n")
		bullets(&out, "-", d.Findings)
	}

	out.WriteString("\n## Approach review\n")
	for _, part := range []struct {
		name  string
		items []string
	}{{"Holds", d.ApproachReview.Holds}, {"Unsure, answered", d.ApproachReview.Unsure}, {"Not covered yet", d.ApproachReview.Misses}, {"Alternatives", d.ApproachReview.Alternatives}} {
		out.WriteString("\n### " + part.name + "\n\n")
		bullets(&out, "-", part.items)
	}

	if saved.Stamped() {
		out.WriteString("\n## Acceptance Criteria\n\n")
		bullets(&out, "*", d.AcceptanceCriteria)

		out.WriteString("\n## Technical Implementation\n\n")
		repoFiles(&out, d.TechnicalImplementation)

		out.WriteString("\n## Story Point Criteria\n\n")
		for _, row := range d.StoryPointCriteria.Rows() {
			fmt.Fprintf(&out, "- %s: %s\n", row[0], row[1])
		}
		fmt.Fprintf(&out, "\n**Points:** %d\n", d.Points)
	}

	out.WriteString("\n## Deployment\n\n")
	fmt.Fprintf(&out, "**Platform:** %s\n", d.Deployment.Platform)
	for _, part := range []struct {
		name  string
		items []string
	}{{"Evidence", d.Deployment.Evidence}, {"Steps", d.Deployment.Steps}} {
		out.WriteString("\n### " + part.name + "\n\n")
		bullets(&out, "-", part.items)
	}
	out.WriteString("\n### Blockers\n\n")
	answered(&out, blockerChoices(d.Deployment.Blockers), saved)

	if len(d.Split) > 0 {
		out.WriteString("\n## Split\n\n")
		for i, child := range d.Split {
			if i > 0 {
				out.WriteString("\n")
			}
			fmt.Fprintf(&out, "### %s (%d pts)\n\n**Acceptance Criteria**\n\n", child.Title, child.Points)
			bullets(&out, "*", child.AcceptanceCriteria)
			out.WriteString("\n**Technical Implementation**\n\n")
			repoFiles(&out, child.TechnicalImplementation)
			if child.StoryPointCriteria.Rated() {
				out.WriteString("\n**Story Point Criteria**\n\n")
				for _, row := range child.StoryPointCriteria.Rows() {
					fmt.Fprintf(&out, "- %s: %s\n", row[0], row[1])
				}
			}
			if key := saved.Children[i]; key != "" {
				fmt.Fprintf(&out, "\nCreated as %s.\n", key)
			}
		}
	}

	if len(d.Elsewhere) > 0 {
		out.WriteString("\n## Left to other tickets\n\n")
		for _, left := range d.Elsewhere {
			fmt.Fprintf(&out, "- %s: %s\n", left.Key, left.Behaviour)
		}
	}

	if len(d.Duplicates) > 0 {
		out.WriteString("\n## Already delivered by other tickets\n\n")
		for _, dup := range d.Duplicates {
			fmt.Fprintf(&out, "- %s: %q is delivered by %s: %q\n", dup.Ticket, dup.Criterion, dup.Key, dup.Covered)
		}
	}

	out.WriteString("\n## Related tickets\n\n")
	if len(d.Relations) == 0 {
		out.WriteString("_None._\n")
	}
	for _, relation := range d.Relations {
		fmt.Fprintf(&out, "- %s %s: %s\n", relation.Label(), relation.Key, relation.Why)
	}

	out.WriteString("\n## Open questions\n\n")
	answered(&out, d.OpenQuestions, saved)
	return out.String()
}

// answered lists the questions with their answers. A stamped draft marks the
// ones the developer proceeded with unanswered.
func answered(out *strings.Builder, questions []Choice, saved Saved) {
	if len(questions) == 0 {
		out.WriteString("_None._\n")
	}
	for _, q := range questions {
		out.WriteString("- " + q.String() + "\n")
		switch answer := strings.TrimSpace(saved.Answers[q.Question]); {
		case answer != "":
			out.WriteString("  - **Answer:** " + strings.ReplaceAll(answer, "\n", " ") + "\n")
		case saved.Step == StepStamp:
			out.WriteString("  - _Still open: stamped without an answer._\n")
		}
	}
}

// blockerChoices are deploy blockers as questions, which have no options.
func blockerChoices(blockers []string) []Choice {
	choices := make([]Choice, len(blockers))
	for i, blocker := range blockers {
		choices[i] = Choice{Question: blocker}
	}
	return choices
}

func repoFiles(out *strings.Builder, repos []RepoFiles) {
	if len(repos) == 0 {
		out.WriteString("_None._\n")
	}
	for i, repo := range repos {
		if i > 0 {
			out.WriteString("\n")
		}
		out.WriteString("`" + repo.Repo + "`\n\n")
		for _, file := range repo.Files {
			line := "* `" + file.Path + "`"
			if file.New {
				line += " (new)"
			}
			out.WriteString(line + "\n")
		}
	}
}

func bullets(out *strings.Builder, marker string, items []string) {
	if len(items) == 0 {
		out.WriteString("_None._\n")
	}
	for _, item := range items {
		out.WriteString(marker + " " + item + "\n")
	}
}

// WriteMarkdown saves the draft as a markdown file at path.
func WriteMarkdown(path string, saved Saved, summary string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(Markdown(saved, summary)), 0o600)
}
