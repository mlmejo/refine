package draft

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// Choice is a question the developer answers by picking an option, or in
// words. Options is empty for an open question, whose Recommended is a
// suggested answer. Answers are keyed by Question.
type Choice struct {
	Question    string   `json:"question"`
	Options     []string `json:"options"`
	Recommended string   `json:"recommended"`
	Why         string   `json:"why"`
	// Consequences says what each option leads to, one line per option in
	// the same order, written evenly so none reads as the preferred one.
	Consequences []string `json:"consequences,omitempty"`
	// Evidence are the indexes of the findings the question rests on. The
	// app shows them with the question and the rest as background.
	Evidence []int `json:"evidence,omitempty"`
	// Anchor is the ticket text the decision serves, so the developer
	// decides knowing what in the ticket it is for.
	Anchor *Anchor `json:"anchor,omitempty"`
	// Reopens is the index into Draft.Decided of the earlier decision this
	// question reopens, when the code shows a risk in it; nil otherwise.
	Reopens *int `json:"reopens,omitempty"`
}

// Anchor quotes the ticket: the words a decision serves and where they are.
// Source "none" means the question is not in the ticket; Claude raised it
// because the code shows the ticket cannot be built without it.
type Anchor struct {
	Quote  string `json:"quote"`
	Source string `json:"source"`
	Ref    string `json:"ref"`
}

// Reopened reports which earlier decision the question reopens, if any.
// Claude writes -1 for none.
func (c Choice) Reopened() (int, bool) {
	if c.Reopens == nil || *c.Reopens < 0 {
		return 0, false
	}
	return *c.Reopens, true
}

// UnmarshalJSON also reads a question saved as plain text, as drafts were
// before questions had options. The whole text stays the question, so its
// saved answer still matches.
func (c *Choice) UnmarshalJSON(data []byte) error {
	var text string
	if json.Unmarshal(data, &text) == nil {
		*c = Choice{Question: text}
		return nil
	}
	type plain Choice
	return json.Unmarshal(data, (*plain)(c))
}

// ScopeQuestion is a triage question about what to build. The developer
// answers it with their approach, so the questions step does not ask it
// again. Options is empty for an open question.
type ScopeQuestion struct {
	Question     string   `json:"question"`
	Options      []string `json:"options"`
	Consequences []string `json:"consequences,omitempty"`
	Recommended  string   `json:"recommended"`
	Why          string   `json:"why"`
}

// legacyOption finds the "(a) " markers in a scope question written as one
// line, as triages were before they had options.
var legacyOption = regexp.MustCompile(`\(([a-z])\)\s*`)

// UnmarshalJSON also reads a scope question saved as one line:
// "Question? (a) option; (b) option → (b), reason".
func (q *ScopeQuestion) UnmarshalJSON(data []byte) error {
	var line string
	if json.Unmarshal(data, &line) == nil {
		*q = parseScopeLine(line)
		return nil
	}
	type plain ScopeQuestion
	return json.Unmarshal(data, (*plain)(q))
}

func parseScopeLine(line string) ScopeQuestion {
	ask, advice, _ := strings.Cut(line, "→")
	ask, advice = strings.TrimSpace(ask), strings.TrimSpace(advice)
	q := ScopeQuestion{Question: ask}
	if marks := legacyOption.FindAllStringSubmatchIndex(ask, -1); len(marks) > 0 {
		q.Question = strings.TrimSpace(ask[:marks[0][0]])
		letters := map[string]string{}
		for i, mark := range marks {
			end := len(ask)
			if i+1 < len(marks) {
				end = marks[i+1][0]
			}
			option := strings.Trim(strings.TrimSpace(ask[mark[1]:end]), ";,. ")
			q.Options = append(q.Options, option)
			letters[ask[mark[2]:mark[3]]] = option
		}
		if m := legacyOption.FindStringSubmatchIndex(advice); m != nil && m[0] == 0 {
			q.Recommended = letters[advice[m[2]:m[3]]]
			advice = advice[m[1]:]
		}
	}
	advice = strings.TrimSpace(strings.TrimLeft(advice, ", "))
	if q.Recommended == "" && len(q.Options) == 0 {
		q.Recommended, advice, _ = strings.Cut(advice, ",")
		q.Recommended, advice = strings.TrimSpace(q.Recommended), strings.TrimSpace(advice)
	}
	q.Why = advice
	return q
}

// String is the question on one line, as prompts give it.
func (q ScopeQuestion) String() string {
	return Choice{Question: q.Question, Options: q.Options, Consequences: q.Consequences, Recommended: q.Recommended, Why: q.Why}.String()
}

// String is the question on one line, as prompts and markdown give it.
func (q Choice) String() string {
	var out strings.Builder
	out.WriteString(q.Question)
	if len(q.Options) > 0 {
		options := make([]string, len(q.Options))
		for i, option := range q.Options {
			options[i] = option
			if i < len(q.Consequences) && q.Consequences[i] != "" {
				options[i] += " (" + q.Consequences[i] + ")"
			}
		}
		out.WriteString(" Options: " + strings.Join(options, "; ") + ".")
	}
	if q.Recommended != "" {
		fmt.Fprintf(&out, " Recommended: %s", q.Recommended)
		if q.Why != "" {
			out.WriteString(", " + q.Why)
		}
		out.WriteString(".")
	}
	return out.String()
}

// ScopePrompt gives the developer's answers to the scope questions, which
// settle them.
func ScopePrompt(answers map[string]string) string {
	var out strings.Builder
	for _, question := range slices.Sorted(maps.Keys(answers)) {
		answer := strings.TrimSpace(answers[question])
		if answer == "" {
			continue
		}
		if out.Len() == 0 {
			out.WriteString("\nThe developer answered these scope questions with their approach. They settle the scope; do not ask them again:\n")
		}
		fmt.Fprintf(&out, "- Q: %s\n  A: %q\n", question, answer)
	}
	return out.String()
}
