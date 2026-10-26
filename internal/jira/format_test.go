package jira

import (
	"strings"
	"testing"
)

func TestFieldsFollowJiraLayout(t *testing.T) {
	f := Fields(Draft{
		AcceptanceCriteria: []string{"One.", "Two."},
		Repos:              []RepoFiles{{Repo: "a", Files: []File{{Path: "x.go", New: true}, {Path: "y.go"}}}},
		Decisions:          []string{"Soft delete, keep 30 days"},
		Ratings:            [][2]string{{"Novelty", "Medium"}, {"Deployment Complexity", "Low"}},
		Total:              "Medium (1.6)",
		Points:             2,
	})
	if f.AcceptanceCriteria != "* One.\n* Two." || f.TechnicalImplementation != "a\n\n* x.go (new)\n* y.go\n\n---\n\n* Soft delete, keep 30 days" ||
		f.StoryPointCriteria != "Novelty: Medium\nDeployment Complexity: Low\nTotal: Medium (1.6)" || f.Points != 2 {
		t.Fatalf("fields = %+v", f)
	}
}

func TestFieldsLeaveStoryPointCriteriaEmptyWithoutATotal(t *testing.T) {
	f := Fields(Draft{Ratings: [][2]string{{"Novelty", ""}}})
	if f.StoryPointCriteria != "" {
		t.Fatalf("story point criteria = %q", f.StoryPointCriteria)
	}
}

func TestDeployCommentWritesWikiMarkup(t *testing.T) {
	got := DeployComment([]string{"Set `KEY` on prod", "Run the\nmigration"})
	if !strings.Contains(got, "* Set {{KEY}} on prod\n") || !strings.Contains(got, "* Run the migration\n") {
		t.Fatalf("comment = %q", got)
	}
}

func TestLinkBlocksAndBlockedBy(t *testing.T) {
	cases := []struct {
		link              Link
		blockedBy, blocks bool
	}{
		{Link{Type: "Blocks", Relation: "is blocked by"}, true, false},
		{Link{Type: "Blocks", Relation: "blocks"}, false, true},
		{Link{Type: "Relates", Relation: "relates to"}, false, false},
		{Link{Type: "Cloners", Relation: "blocks"}, false, false},
	}
	for _, c := range cases {
		if c.link.BlockedBy() != c.blockedBy || c.link.Blocks() != c.blocks {
			t.Errorf("%+v: blocked by %v, blocks %v", c.link, c.link.BlockedBy(), c.link.Blocks())
		}
	}
}
