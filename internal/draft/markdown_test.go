package draft

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMarkdown(t *testing.T) {
	saved := Saved{
		Key:      "GEEK-7",
		Approach: "Use the SDK.\nThen the header.",
		Ref:      "develop",
		Created:  time.Date(2026, 10, 8, 18, 0, 0, 0, time.Local),
		Draft: Draft{
			AcceptanceCriteria: []string{"QA sees the purchase."},
			TechnicalImplementation: []RepoFiles{
				{Repo: "dating-backend", Files: []File{{Path: "app/A.php"}, {Path: "app/B.php", New: true}}},
				{Repo: "dating", Files: []File{{Path: "plugins/axios.js"}}},
			},
			StoryPointCriteria: StoryPointCriteria{Novelty: "Low", Risk: "Medium", Dependency: "Low", Testing: "Low", Deployment: "Low", Total: "Medium"},
			Points:             3,
			Split: []Child{{Title: "Purchase Tracking - Session Header", Points: 1,
				AcceptanceCriteria:      []string{"As an analyst, I want purchases tied to the session, so that funnels add up."},
				TechnicalImplementation: []RepoFiles{{Repo: "dating", Files: []File{{Path: "plugins/axios.js"}}}},
			}},
			ApproachReview: ApproachReview{Holds: []string{"SDK fits."}},
		},
	}

	got := Markdown(saved, "Starvault PHP SDK")
	for _, want := range []string{
		"# GEEK-7: Starvault PHP SDK\n\nDrafted 2026-10-08 18:00 · 3 pts · repos at develop\n",
		"> Use the SDK.\n> Then the header.\n",
		"### Holds\n\n- SDK fits.\n",
		"### Unsure, answered\n\n_None._\n", "### Not covered yet\n\n_None._\n",
		"## Acceptance Criteria\n\n* QA sees the purchase.\n",
		"`dating-backend`\n\n* `app/A.php`\n* `app/B.php` (new)\n\n`dating`\n\n* `plugins/axios.js`\n",
		"- Risk & Feature Impact: Medium\n",
		"- Total: Medium\n\n**Points:** 3\n",
		"## Split\n\n### Purchase Tracking - Session Header (1 pts)\n\n**Acceptance Criteria**\n\n* As an analyst, I want purchases tied to the session, so that funnels add up.\n\n**Technical Implementation**\n\n`dating`\n\n* `plugins/axios.js`\n",
		"## Open questions\n\n_None._\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("markdown missing %q:\n%s", want, got)
		}
	}
}

func TestWriteMarkdown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exports", "GEEK-7", "1.md")
	if err := WriteMarkdown(path, Saved{Key: "GEEK-7"}, "SDK"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); !strings.HasPrefix(string(data), "# GEEK-7: SDK") {
		t.Errorf("file:\n%s", data)
	}
}

func TestMarkdownWithoutSummary(t *testing.T) {
	if got := Markdown(Saved{Key: "GEEK-7"}, ""); !strings.HasPrefix(got, "# GEEK-7\n") {
		t.Errorf("markdown:\n%s", got)
	}
}

func TestRefinementIsPlainText(t *testing.T) {
	d := Draft{
		AcceptanceCriteria: []string{"The bell shows notifications only.", "Both badges survive a refresh."},
		TechnicalImplementation: []RepoFiles{
			{Repo: "datingv2", Files: []File{{Path: "app/pages/messages/[uuid].vue"}, {Path: "app/composables/useNotifications.ts", New: true}}},
			{Repo: "dating-backend", Files: []File{{Path: "routes/dating.php"}}},
		},
		StoryPointCriteria: StoryPointCriteria{Novelty: "Low", Risk: "Low", Dependency: "Low", Testing: "Medium", Deployment: "Low", Total: "Medium"},
		Points:             3,
	}
	got := d.Refinement()

	if want := "* The bell shows notifications only.\n* Both badges survive a refresh."; got.AcceptanceCriteria != want {
		t.Errorf("AC = %q", got.AcceptanceCriteria)
	}
	wantTI := "datingv2\n\n* app/pages/messages/[uuid].vue\n* app/composables/useNotifications.ts (new)\n\ndating-backend\n\n* routes/dating.php"
	if got.TechnicalImplementation != wantTI {
		t.Errorf("TI = %q", got.TechnicalImplementation)
	}
	wantSPC := "Novelty & Complexity of Implementation: Low\nRisk & Feature Impact: Low\nDependency & Integration Complexity: Low\nTesting & QA Effort: Medium\nDeployment Complexity: Low\nTotal: Medium"
	if got.StoryPointCriteria != wantSPC {
		t.Errorf("SPC = %q", got.StoryPointCriteria)
	}
	if got.Points != 3 {
		t.Errorf("points = %d", got.Points)
	}
}

func TestChildRefinementLeavesOutUnratedCriteria(t *testing.T) {
	child := Child{
		Title:                   "Books - Deleting",
		Points:                  1,
		AcceptanceCriteria:      []string{"As an admin, I want to delete books, so that the catalogue stays tidy."},
		TechnicalImplementation: []RepoFiles{{Repo: "api", Files: []File{{Path: "app/Books.php"}}}},
	}
	got := child.Refinement()
	if got.AcceptanceCriteria != "* As an admin, I want to delete books, so that the catalogue stays tidy." || got.TechnicalImplementation != "api\n\n* app/Books.php" || got.Points != 1 {
		t.Errorf("refinement = %+v", got)
	}
	if got.StoryPointCriteria != "" {
		t.Errorf("SPC = %q, want none for an unrated child", got.StoryPointCriteria)
	}

	child.StoryPointCriteria = StoryPointCriteria{Novelty: "Low", Risk: "Low", Dependency: "Low", Testing: "Low", Deployment: "Low", Total: "Low"}
	if got := child.Refinement().StoryPointCriteria; !strings.HasSuffix(got, "\nTotal: Low") {
		t.Errorf("SPC = %q", got)
	}
}

func TestMarkdownListsRelations(t *testing.T) {
	saved := Saved{Key: "GEEK-11989", Draft: Draft{Relations: []Relation{
		{Key: "GEEK-12000", Kind: "needs", Why: "A new notification raises the bell only once Echo runs app-wide."},
		{Key: "GEEK-11998", Kind: "conflicts", Why: "The mobile Messages badge shows unread messages here, notifications there."},
	}}}
	got := Markdown(saved, "")
	for _, want := range []string{
		"## Related tickets\n\n- needs GEEK-12000: A new notification",
		"- conflicts with GEEK-11998: The mobile Messages badge",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestMarkdownListsWhatOtherTicketsDeliver(t *testing.T) {
	saved := Saved{Key: "GEEK-12069", Draft: Draft{
		Elsewhere:  []Elsewhere{{Key: "GEEK-11998", Behaviour: "the notification list"}},
		Checked:    true,
		Duplicates: []Duplicate{{Ticket: "GEEK-12069", Criterion: "The badge clears.", Key: "GEEK-11998", Covered: "The badge clears once the bell has been opened."}},
	}}
	got := Markdown(saved, "")
	for _, want := range []string{
		"## Left to other tickets\n\n- GEEK-11998: the notification list",
		"## Already delivered by other tickets\n\n- GEEK-12069: \"The badge clears.\" is delivered by GEEK-11998: \"The badge clears once the bell has been opened.\"",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestMarkdownFollowsTheSteps(t *testing.T) {
	questions := Saved{
		Key:     "GEEK-8",
		Step:    StepQuestions,
		Created: time.Date(2026, 10, 13, 9, 0, 0, 0, time.Local),
		Draft: Draft{
			Problem: "Counts go stale.", Solution: "Live counts.",
			TicketBlockers: []string{"GEEK-5 blocks the reset"},
			Findings:       []string{"echo.client.ts:73 connects on login"},
			OpenQuestions:  []Choice{{Question: "Server count?"}, {Question: "Reset on open?"}},
		},
		Answers: map[string]string{"Server count?": "yes"},
	}
	got := Markdown(questions, "Live")
	for _, want := range []string{
		"Questions 2026-10-13 09:00 · not stamped yet\n",
		"## Ticket\n\n**Problem:** Counts go stale.\n\n**Solution:** Live counts.\n\n### Blockers\n\n- GEEK-5 blocks the reset\n",
		"## Findings\n\n- echo.client.ts:73 connects on login\n",
		"- Server count?\n  - **Answer:** yes\n- Reset on open?\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("questions markdown missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "## Acceptance Criteria") || strings.Contains(got, "Still open") {
		t.Errorf("questions markdown has stamp parts:\n%s", got)
	}

	stamped := questions
	stamped.Step = StepStamp
	stamped.Draft.AcceptanceCriteria = []string{"QA sees live counts."}
	got = Markdown(stamped, "Live")
	if !strings.Contains(got, "## Acceptance Criteria\n\n* QA sees live counts.\n") || !strings.Contains(got, "- Reset on open?\n  - _Still open: stamped without an answer._\n") {
		t.Errorf("stamped markdown:\n%s", got)
	}
}
