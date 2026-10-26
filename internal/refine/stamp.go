package refine

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/mlmejo/refine/internal/jira"
)

// Action is one write to Jira when stamping.
type Action struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Plan lists the Jira writes a stamp makes, in order: the fields, the deploy
// comment, each split ticket, a link to each accepted blocker, the move.
func Plan(r Refinement) []Action {
	if r.Result == nil {
		return nil
	}
	res := r.Result
	plan := []Action{{ID: "write", Label: fmt.Sprintf("Write Acceptance Criteria, Technical Implementation and %d point%s to %s", res.Points, plural(res.Points), r.Key)}}
	if len(res.DeployNeeds) > 0 {
		plan = append(plan, Action{ID: "comment", Label: fmt.Sprintf("Comment %d deploy need%s on %s", len(res.DeployNeeds), plural(len(res.DeployNeeds)), r.Key)})
	}
	for i, c := range res.Split {
		plan = append(plan, Action{ID: "child:" + strconv.Itoa(i), Label: fmt.Sprintf("Create %q in Ready for Sprint, related to %s", c.Title, r.Key)})
	}
	for _, b := range r.Related.Accepted() {
		plan = append(plan, Action{ID: "link:" + b.Key, Label: fmt.Sprintf("Link %s relates to %s", b.Key, r.Key)})
	}
	return append(plan, Action{ID: "move", Label: fmt.Sprintf("Move %s to Ready for Sprint", r.Key)})
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
	var target *jira.SplitTarget
	for _, action := range Plan(r) {
		if slices.Contains(r.Stamp.Done, action.ID) {
			continue
		}
		child, err := s.act(ctx, r, action.ID, &target)
		r, err = s.update(key, func(r *Refinement) error {
			if child != "" {
				if r.Stamp.Children == nil {
					r.Stamp.Children = map[int]string{}
				}
				i, _ := strconv.Atoi(strings.TrimPrefix(action.ID, "child:"))
				r.Stamp.Children[i] = child
			}
			if err != nil {
				r.Stamp.Failed, r.Stamp.Error = action.ID, err.Error()
				return nil
			}
			r.Stamp.Done = append(r.Stamp.Done, action.ID)
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

// act runs one stamp action. A created split ticket's key comes back even
// when part of its creation failed, so it is not created twice.
func (s *Service) act(ctx context.Context, r Refinement, id string, target **jira.SplitTarget) (string, error) {
	res := r.Result
	switch {
	case id == "write":
		return "", s.Jira.Update(ctx, r.Key, fields(res.AcceptanceCriteria, res.TechnicalImplementation, res.Decisions, res.Ratings, res.Points))
	case id == "comment":
		body := jira.DeployComment(res.DeployNeeds)
		t, err := s.Jira.GetTicket(ctx, r.Key)
		if err != nil || t.HasComment(body) {
			return "", err
		}
		return "", s.Jira.Comment(ctx, r.Key, body)
	case strings.HasPrefix(id, "child:"):
		i, _ := strconv.Atoi(strings.TrimPrefix(id, "child:"))
		if *target == nil {
			t, err := s.Jira.Split(ctx, r.Key, s.ReadySprint)
			if err != nil {
				return "", err
			}
			*target = &t
		}
		c := res.Split[i]
		return s.Jira.CreateChild(ctx, **target, jira.NewChild{
			Summary:     c.Title,
			Description: fmt.Sprintf("Split from %s (%s), which holds the foundation this builds on.", r.Key, r.Summary),
			Refinement:  fields(c.AcceptanceCriteria, c.TechnicalImplementation, c.Decisions, c.Ratings, c.Points),
		})
	case strings.HasPrefix(id, "link:"):
		return "", s.Jira.Relate(ctx, r.Key, strings.TrimPrefix(id, "link:"))
	case id == "move":
		return "", s.Jira.MoveToSprint(ctx, r.Key, s.ReadySprint)
	}
	return "", fmt.Errorf("unknown stamp action %q", id)
}

// fields is a ticket's refinement as Jira's fields take it.
func fields(criteria []string, repos []RepoFiles, decisions []string, ratings Ratings, points int) jira.Refinement {
	files := make([]jira.RepoFiles, len(repos))
	for i, repo := range repos {
		files[i].Repo = repo.Repo
		for _, f := range repo.Files {
			files[i].Files = append(files[i].Files, jira.File{Path: f.Path, New: f.New})
		}
	}
	return jira.Fields(jira.Draft{
		AcceptanceCriteria: criteria,
		Repos:              files,
		Decisions:          decisions,
		Ratings:            ratings.Rows(),
		Total:              ratings.Total(),
		Points:             points,
	})
}
