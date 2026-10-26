package refine

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/mlmejo/refine/internal/jira"
)

// ActionKind is which Jira write an action makes.
type ActionKind int

const (
	ActionWrite ActionKind = iota
	ActionComment
	ActionChild
	ActionLink
	ActionMove
)

// Action is one write to Jira when stamping. ID is what the stamp records
// as done; Index and Key say which split ticket or blocker it is for.
type Action struct {
	ID    string     `json:"id"`
	Label string     `json:"label"`
	Kind  ActionKind `json:"-"`
	Index int        `json:"-"`
	Key   string     `json:"-"`
}

// Plan lists the Jira writes a stamp makes, in order: the fields, the deploy
// comment, each split ticket, a link to each accepted blocker, the move.
func Plan(r Refinement) []Action {
	if r.Result == nil {
		return nil
	}
	res := r.Result
	plan := []Action{{ID: "write", Kind: ActionWrite, Label: fmt.Sprintf("Write Acceptance Criteria, Technical Implementation and %d point%s to %s", res.Points, plural(res.Points), r.Key)}}
	if len(res.DeployNeeds) > 0 {
		plan = append(plan, Action{ID: "comment", Kind: ActionComment, Label: fmt.Sprintf("Comment %d deploy need%s on %s", len(res.DeployNeeds), plural(len(res.DeployNeeds)), r.Key)})
	}
	for i, c := range res.Split {
		plan = append(plan, Action{ID: "child:" + strconv.Itoa(i), Kind: ActionChild, Index: i, Label: fmt.Sprintf("Create %q in Ready for Sprint, related to %s", c.Title, r.Key)})
	}
	for _, b := range r.Related.Accepted() {
		plan = append(plan, Action{ID: "link:" + b.Key, Kind: ActionLink, Key: b.Key, Label: fmt.Sprintf("Link %s relates to %s", b.Key, r.Key)})
	}
	return append(plan, Action{ID: "move", Kind: ActionMove, Label: fmt.Sprintf("Move %s to Ready for Sprint", r.Key)})
}

// Stamp writes the result to Jira, one action at a time, and records each as
// it finishes. A retry skips the actions already done. Once all are done the
// ticket is stamped.
func (s *Service) Stamp(ctx context.Context, key string) (Refinement, error) {
	r, ok, err := s.Store.Load(key)
	if err != nil {
		return Refinement{}, err
	}
	if !ok || r.Step != StepResults || r.Result == nil {
		return Refinement{}, fmt.Errorf("stamp: %w", ErrStep)
	}
	st := &stamper{s: s, r: r}
	for _, a := range Plan(r) {
		if slices.Contains(r.Stamp.Done, a.ID) {
			continue
		}
		child, actErr := st.act(ctx, a)
		r, err = s.update(key, func(r *Refinement) error {
			if child != "" {
				if r.Stamp.Children == nil {
					r.Stamp.Children = map[int]string{}
				}
				r.Stamp.Children[a.Index] = child
			}
			if actErr != nil {
				r.Stamp.Failed, r.Stamp.Error = a.ID, actErr.Error()
				return nil
			}
			r.Stamp.Done = append(r.Stamp.Done, a.ID)
			r.Stamp.Failed, r.Stamp.Error = "", ""
			return nil
		})
		if err != nil {
			return Refinement{}, err
		}
		if r.Stamp.Error != "" {
			return r, nil
		}
	}
	return s.update(key, func(r *Refinement) error {
		r.Step = StepStamped
		return nil
	})
}

// stamper runs the actions of one stamp. It keeps the split target, made
// on the first split ticket, for the ones after it.
type stamper struct {
	s      *Service
	r      Refinement
	target *jira.SplitTarget
}

// act runs one stamp action. A created split ticket's key comes back even
// when part of its creation failed, so it is not created twice.
func (st *stamper) act(ctx context.Context, a Action) (string, error) {
	s, r := st.s, st.r
	res := r.Result
	switch a.Kind {
	case ActionWrite:
		return "", s.Jira.Update(ctx, r.Key, fields(res.AcceptanceCriteria, res.TechnicalImplementation, res.Decisions, res.Ratings, res.Points))
	case ActionComment:
		body := DeployComment(res.DeployNeeds)
		t, err := s.Jira.GetTicket(ctx, r.Key)
		if err != nil || t.HasComment(body) {
			return "", err
		}
		return "", s.Jira.Comment(ctx, r.Key, body)
	case ActionChild:
		if a.Index < 0 || a.Index >= len(res.Split) {
			return "", fmt.Errorf("no split ticket %d", a.Index)
		}
		target, err := st.splitTarget(ctx)
		if err != nil {
			return "", err
		}
		c := res.Split[a.Index]
		return s.Jira.CreateChild(ctx, target, jira.NewChild{
			Summary:     c.Title,
			Description: fmt.Sprintf("Split from %s (%s), which holds the foundation this builds on.", r.Key, r.Summary),
			Refinement:  fields(c.AcceptanceCriteria, c.TechnicalImplementation, c.Decisions, c.Ratings, c.Points),
		})
	case ActionLink:
		return "", s.Jira.Relate(ctx, r.Key, a.Key)
	case ActionMove:
		return "", s.Jira.MoveToSprint(ctx, r.Key, s.ReadySprint)
	}
	return "", fmt.Errorf("unknown stamp action %q", a.ID)
}

// splitTarget is where split tickets go, asked of Jira once per stamp.
func (st *stamper) splitTarget(ctx context.Context) (jira.SplitTarget, error) {
	if st.target == nil {
		t, err := st.s.Jira.Split(ctx, st.r.Key, st.s.ReadySprint)
		if err != nil {
			return jira.SplitTarget{}, err
		}
		st.target = &t
	}
	return *st.target, nil
}

// fields is a ticket's refinement as Jira's fields take it. The fields
// render plain text, so nothing here is markdown.
func fields(criteria []string, repos []RepoFiles, decisions []string, ratings Ratings, points int) jira.Refinement {
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
	if len(decisions) > 0 {
		ti.WriteString("\n---\n\n")
		for _, d := range decisions {
			ti.WriteString("* " + d + "\n")
		}
	}
	var spc []string
	if total := ratings.Total(); total != "" {
		for _, row := range ratings.Rows() {
			spc = append(spc, row[0]+": "+row[1])
		}
		spc = append(spc, "Total: "+total)
	}
	return jira.Refinement{
		AcceptanceCriteria:      strings.TrimSpace(ac.String()),
		TechnicalImplementation: strings.TrimSpace(ti.String()),
		StoryPointCriteria:      strings.Join(spc, "\n"),
		Points:                  points,
	}
}

// DeployComment is the deploy needs as one Jira comment in wiki markup.
func DeployComment(needs []string) string {
	var out strings.Builder
	out.WriteString("Deploy needs found while refining this ticket.\n\n")
	for _, need := range needs {
		out.WriteString("* " + wiki(need) + "\n")
	}
	return out.String()
}

var backticked = regexp.MustCompile("`([^`]+)`")

// wiki puts one line of text in Jira wiki markup: `code` becomes {{code}},
// and line breaks become spaces so a list item stays one item.
func wiki(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	return backticked.ReplaceAllString(text, "{{$1}}")
}
