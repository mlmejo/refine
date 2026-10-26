package refine

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mlmejo/refine/internal/jira"
)

//go:embed prompts/efficiency.md
var efficiency string

//go:embed prompts/related.md
var relatedRules string

//go:embed prompts/related.schema.json
var relatedSchema string

//go:embed prompts/questions.md
var questionsText string

//go:embed prompts/questions.schema.json
var questionsSchema string

//go:embed prompts/deploy.md
var deployText string

//go:embed prompts/deploy.schema.json
var deploySchema string

//go:embed prompts/generate.md
var generateText string

//go:embed prompts/generate.schema.json
var generateSchema string

var (
	questionsRules = withShared(questionsText)
	deployRules    = withShared(deployText)
	generateRules  = withShared(generateText)
)

// withShared puts the sections that steps reading code share in place of
// their markers.
func withShared(rules string) string {
	return strings.Replace(rules, "{{working efficiently}}\n", efficiency, 1)
}

// PromptsVersion fingerprints every rule and schema Claude is given, so a
// refinement can be traced to the prompts that made it.
func PromptsVersion() string {
	h := sha256.New()
	for _, part := range []string{relatedRules, relatedSchema, questionsRules, questionsSchema, deployRules, deploySchema, generateRules, generateSchema} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:10]
}

// ticketText describes a ticket as Jira has it.
func ticketText(t jira.Ticket) string {
	var out strings.Builder
	fmt.Fprintf(&out, "Ticket %s: %s\n", t.Key, t.Summary)
	points := "none"
	if t.Points != nil {
		points = strconv.FormatFloat(*t.Points, 'f', -1, 64)
	}
	fmt.Fprintf(&out, "Status: %s. Story points: %s.\n", t.Status, points)
	section(&out, "Description", t.Description)
	if len(t.Links) > 0 {
		out.WriteString("\nLinks:\n")
		for _, link := range t.Links {
			fmt.Fprintf(&out, "- %s %s (%s): %s\n", link.Relation, link.Key, link.Status, link.Summary)
		}
	}
	if len(t.Comments) > 0 {
		out.WriteString("\nComments:\n")
		for _, comment := range t.Comments {
			fmt.Fprintf(&out, "- %s: %q\n", comment.Author, comment.Body)
		}
	}
	return out.String()
}

// maxCandidateDescription caps each other ticket's description, so the list
// stays a small part of the prompt.
const maxCandidateDescription = 1500

// relatedPrompt asks which of the candidates the ticket builds on or overlaps.
func relatedPrompt(t jira.Ticket, candidates []jira.Ticket, explanations []string) string {
	var out strings.Builder
	out.WriteString("The ticket being refined:\n\n")
	out.WriteString(ticketText(t))
	out.WriteString("\nThe developer's other open tickets:\n")
	if len(candidates) == 0 {
		out.WriteString("none\n")
	}
	for _, c := range candidates {
		fmt.Fprintf(&out, "\n%s (%s): %s\n", c.Key, c.Status, c.Summary)
		if c.Refined() {
			section(&out, "Acceptance Criteria", c.AcceptanceCriteria)
			section(&out, "Technical Implementation", c.TechnicalImplementation)
		} else {
			section(&out, "Description", clip(c.Description, maxCandidateDescription))
		}
	}
	if len(explanations) > 0 {
		out.WriteString("\nThe developer explained, about your earlier answer:\n")
		for _, e := range explanations {
			fmt.Fprintf(&out, "- %q\n", strings.TrimSpace(e))
		}
	}
	out.WriteString("\nFind the blockers and overlaps as your instructions say.\n")
	return out.String()
}

// elsewhere describes the tickets this one builds on, whose work Claude must
// treat as done and never ask about.
func elsewhere(blockers []jira.Ticket) string {
	if len(blockers) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("\nTickets this one builds on. Their work is delivered elsewhere: treat it as done and never ask about it.\n")
	for _, b := range blockers {
		fmt.Fprintf(&out, "\n%s: %s\n", b.Key, b.Summary)
		if b.AcceptanceCriteria != "" || b.TechnicalImplementation != "" {
			section(&out, "Acceptance Criteria", b.AcceptanceCriteria)
			section(&out, "Technical Implementation", b.TechnicalImplementation)
		} else {
			section(&out, "Description", clip(b.Description, maxCandidateDescription))
		}
	}
	return out.String()
}

// reposText lists the repos Claude reads, each at its fetched branch.
func reposText(r Refinement, dirs map[string]string) string {
	var out strings.Builder
	out.WriteString("\nRepos you can read, each at its branch on origin:\n")
	for _, name := range r.Repos {
		s, _ := r.Commit(name)
		fmt.Fprintf(&out, "- %s: %s (%s at %s)\n", name, dirs[name], s.Branch, short(s.Commit))
	}
	return out.String()
}

// answersText lists questions with their answers.
func answersText(title string, qs []Question) string {
	if len(qs) == 0 {
		return ""
	}
	var out strings.Builder
	fmt.Fprintf(&out, "\n%s\n", title)
	for _, q := range qs {
		fmt.Fprintf(&out, "- Q: %s\n  A: %q\n", q.Text, strings.TrimSpace(q.Answer))
	}
	return out.String()
}

// allAnswers lists every round's questions and answers.
func allAnswers(r Refinement) string {
	var qs []Question
	for _, round := range r.Rounds {
		qs = append(qs, round.Questions...)
	}
	return answersText("The developer's answers about how to build it:", qs)
}

// maxOtherDescription caps each other ticket's description in the questions
// step, where the list is background rather than the task.
const maxOtherDescription = 600

// otherTickets lists the developer's other open tickets that are not
// accepted blockers, so Claude can tell when the code needs one of them.
// Those the developer rejected as blockers are marked, and so are overlaps,
// whose own work stays with them.
func otherTickets(candidates []jira.Ticket, r Refinement) string {
	decided := map[string]string{}
	for _, b := range r.Related.Blockers {
		decided[b.Key] = b.Decision
	}
	overlaps := map[string]string{}
	for _, o := range r.Related.Overlaps {
		overlaps[o.Key] = o.Reason
	}
	var out strings.Builder
	for _, c := range candidates {
		if decided[c.Key] == Accepted {
			continue
		}
		if out.Len() == 0 {
			out.WriteString("\nThe developer's other open tickets. When the code shows this ticket needs something one of them delivers, report it in blockers:\n")
		}
		fmt.Fprintf(&out, "\n%s (%s): %s\n", c.Key, c.Status, c.Summary)
		if decided[c.Key] == Rejected {
			out.WriteString("The developer said this ticket does not build on it.\n")
		}
		if reason, ok := overlaps[c.Key]; ok {
			fmt.Fprintf(&out, "It overlaps this ticket (%s). The work it describes beyond this ticket's stays with it.\n", reason)
		}
		if c.Refined() {
			section(&out, "Acceptance Criteria", c.AcceptanceCriteria)
			section(&out, "Technical Implementation", c.TechnicalImplementation)
		} else {
			section(&out, "Description", clip(c.Description, maxOtherDescription))
		}
	}
	return out.String()
}

// firstRoundPrompt asks the first round of questions.
func firstRoundPrompt(t jira.Ticket, blockers, candidates []jira.Ticket, r Refinement, dirs map[string]string) string {
	return ticketText(t) + elsewhere(blockers) + otherTickets(candidates, r) + reposText(r, dirs) +
		fmt.Sprintf("\nThis is round 1 of %d. Ask your questions as your instructions say.\n", MaxRounds)
}

// nextRoundPrompt gives the last round's answers and asks for the next.
func nextRoundPrompt(r Refinement) string {
	last := r.Rounds[len(r.Rounds)-1]
	n := len(r.Rounds) + 1
	prompt := answersText(fmt.Sprintf("The developer answered round %d:", n-1), last.Questions) +
		fmt.Sprintf("\nThis is round %d of %d. Recheck what the answers touch, then ask only what is still open. Return no questions when every decision is settled.\n", n, MaxRounds)
	if n == MaxRounds {
		prompt += "It is the last round, so ask only what the build cannot do without.\n"
	}
	return prompt
}

// profilesText gives the saved deployment profiles and names the repos
// without one.
func profilesText(r Refinement, profiles map[string]Profile) string {
	var out strings.Builder
	var missing []string
	for _, name := range r.Repos {
		p, ok := profiles[name]
		if !ok || len(p.Answers) == 0 {
			missing = append(missing, name)
			continue
		}
		fmt.Fprintf(&out, "\nHow %s deploys, as the developer answered before:\n", name)
		for _, qa := range p.Answers {
			fmt.Fprintf(&out, "- Q: %s\n  A: %q\n", qa.Question, qa.Answer)
		}
	}
	if len(missing) > 0 {
		fmt.Fprintf(&out, "\nRepos with no saved deployment profile: %s.\n", strings.Join(missing, ", "))
	} else {
		out.WriteString("\nEvery repo has a saved deployment profile; ask no repo questions.\n")
	}
	return out.String()
}

// deployPrompt asks the deployment questions, in a fresh session.
func deployPrompt(t jira.Ticket, blockers []jira.Ticket, r Refinement, dirs map[string]string, profiles map[string]Profile) string {
	return ticketText(t) + elsewhere(blockers) + reposText(r, dirs) + allAnswers(r) + profilesText(r, profiles) +
		"\nAsk the deployment questions as your instructions say.\n"
}

// generatePrompt asks for the write-up, in a fresh session.
func generatePrompt(t jira.Ticket, blockers []jira.Ticket, r Refinement, dirs map[string]string, profiles map[string]Profile) string {
	var deploy strings.Builder
	if r.Deploy != nil {
		for _, name := range r.Repos {
			var qs []Question
			for _, q := range r.Deploy.Repos {
				if q.Repo == name {
					qs = append(qs, q.Question)
				}
			}
			deploy.WriteString(answersText(fmt.Sprintf("How %s deploys:", name), qs))
		}
		deploy.WriteString(answersText("What this ticket needs at deploy:", r.Deploy.Ticket))
	}
	var saved strings.Builder
	for _, name := range r.Repos {
		if p, ok := profiles[name]; ok && len(p.Answers) > 0 {
			fmt.Fprintf(&saved, "\nHow %s deploys:\n", name)
			for _, qa := range p.Answers {
				fmt.Fprintf(&saved, "- Q: %s\n  A: %q\n", qa.Question, qa.Answer)
			}
		}
	}
	return ticketText(t) + elsewhere(blockers) + reposText(r, dirs) + allAnswers(r) + saved.String() + deploy.String() +
		"\nWrite up the ticket as your instructions say.\n"
}

func section(out *strings.Builder, name, text string) {
	if strings.TrimSpace(text) == "" {
		fmt.Fprintf(out, "\n%s: empty.\n", name)
		return
	}
	fmt.Fprintf(out, "\n%s:\n%s\n", name, text)
}

func clip(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return strings.TrimSpace(text[:limit]) + "…"
}

func short(commit string) string {
	return commit[:min(len(commit), 12)]
}

// normalize holds Claude's questions to Go's rules: each has 2 to MaxOptions
// distinct options and a recommendation among them, and there are at most
// limit.
func normalize(in []Question, limit int) []Question {
	var out []Question
	for _, q := range in {
		q.Text = strings.TrimSpace(q.Text)
		var options []string
		recommended := -1
		for i, o := range q.Options {
			o = strings.TrimSpace(o)
			if o == "" || slices.Contains(options, o) || len(options) == MaxOptions {
				continue
			}
			if i == q.Recommended {
				recommended = len(options)
			}
			options = append(options, o)
		}
		if q.Text == "" || len(options) < 2 {
			continue
		}
		q.Options, q.Recommended, q.Answer = options, max(recommended, 0), ""
		out = append(out, q)
		if len(out) == limit {
			break
		}
	}
	return out
}
