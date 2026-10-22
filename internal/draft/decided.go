package draft

import (
	"fmt"
	"strings"
)

// Decided is a decision the developer made in an earlier round, with Claude's
// pick, so every round and the stamp carry the whole record.
type Decided struct {
	Question    string `json:"question"`
	Answer      string `json:"answer"`
	Recommended string `json:"recommended,omitempty"`
	Why         string `json:"why,omitempty"`
}

// Clear reports whether Claude has no questions left. Only then is the draft
// ready to stamp: both sides have seen every decision.
func (d Draft) Clear() bool {
	return len(d.Questions()) == 0
}

// Decide returns the decisions made up to and including prev's round: the
// ones prev carried, then prev's answered questions. A question asked again
// keeps its latest answer.
func Decide(prev Saved) []Decided {
	log := append([]Decided(nil), prev.Draft.Decided...)
	add := func(c Choice) {
		answer := strings.TrimSpace(prev.Answers[c.Question])
		if !decided(answer) {
			return
		}
		entry := Decided{Question: c.Question, Answer: answer, Recommended: c.Recommended, Why: c.Why}
		for i, e := range log {
			if e.Question == c.Question {
				log[i] = entry
				return
			}
		}
		log = append(log, entry)
	}
	for _, b := range prev.Draft.Deployment.Blockers {
		add(Choice{Question: b})
	}
	for _, q := range prev.Draft.OpenQuestions {
		add(q)
	}
	return log
}

// DecidedPrompt lists every decision made so far.
func DecidedPrompt(log []Decided) string {
	if len(log) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("\nEvery decision the developer made over the rounds, numbered for `reopens`. They settle the build; follow them:\n")
	for i, d := range log {
		fmt.Fprintf(&out, "[%d] Q: %s\n    A: %q\n", i, d.Question, d.Answer)
	}
	return out.String()
}

// unknownMark starts an answer that says the developer does not know yet,
// followed by an optional note of what they need to find out.
const unknownMark = "?unknown:"

// Unknown is the answer for "I don't know yet", with what to find out.
func Unknown(note string) string {
	return unknownMark + strings.TrimSpace(note)
}

// IsUnknown reports whether an answer says "I don't know yet", and its note.
func IsUnknown(answer string) (string, bool) {
	note, ok := strings.CutPrefix(strings.TrimSpace(answer), unknownMark)
	return strings.TrimSpace(note), ok
}

// decided reports whether an answer settles its question: it has text and
// is not "I don't know yet".
func decided(answer string) bool {
	_, unknown := IsUnknown(answer)
	return strings.TrimSpace(answer) != "" && !unknown
}

// UnknownPrompt lists the questions the developer does not know yet, for
// Claude to settle from the code and the ticket or hand to who can answer.
func UnknownPrompt(prev Saved) string {
	var out strings.Builder
	for _, question := range prev.Draft.Questions() {
		note, ok := IsUnknown(prev.Answers[question])
		if !ok {
			continue
		}
		if out.Len() == 0 {
			out.WriteString("\nThe developer does not know these yet. Settle each from the code and the ticket if you can, and ask it again with what you found. If you cannot, make it a deploy blocker or a question that names who can answer it:\n")
		}
		fmt.Fprintf(&out, "- %s\n", question)
		if note != "" {
			fmt.Fprintf(&out, "  They need to find out: %q\n", note)
		}
	}
	return out.String()
}
