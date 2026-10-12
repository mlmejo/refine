package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchSendsJQLAndDecodesIssues(t *testing.T) {
	var got struct {
		JQL    string   `json:"jql"`
		Fields []string `json:"fields"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rest/api/3/search/jql" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if user, pass, ok := r.BasicAuth(); !ok || user != "me@example.com" || pass != "secret" {
			t.Errorf("basic auth = %q %q %v", user, pass, ok)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(`{"issues":[{"key":"GEEK-1","fields":{
			"summary":"Delete account",
			"status":{"name":"To Do"},
			"customfield_10042":"* Clicking Delete Account is recorded.\n\n",
			"customfield_10043":{"type":"doc","version":1,"content":[
				{"type":"paragraph","content":[{"type":"text","text":"dating"}]},
				{"type":"bulletList","content":[
					{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"components/a.vue"}]}]},
					{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"components/b.vue (new)"}]}]}
				]}
			]},
			"customfield_10284":null
		}}]}`))
	}))
	defer server.Close()

	client := New(server.URL, "me@example.com", "secret")
	issues, err := client.Search(context.Background(), AssignedJQL("Refinement Ready - Team John"))
	if err != nil {
		t.Fatal(err)
	}

	if got.JQL != `assignee = currentUser() AND sprint = "Refinement Ready - Team John" ORDER BY updated DESC` {
		t.Errorf("jql = %q", got.JQL)
	}
	if !strings.Contains(strings.Join(got.Fields, ","), FieldStoryPointCriteria) {
		t.Errorf("fields = %v", got.Fields)
	}

	want := Issue{
		Key:                     "GEEK-1",
		Summary:                 "Delete account",
		Status:                  "To Do",
		AcceptanceCriteria:      "* Clicking Delete Account is recorded.",
		TechnicalImplementation: "dating\n* components/a.vue\n* components/b.vue (new)",
	}
	if len(issues) != 1 || issues[0] != want {
		t.Errorf("issues = %#v", issues)
	}
}

func TestSearchReportsJiraErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"errorMessages":["The value 'Nope' does not exist for the field 'status'."]}`))
	}))
	defer server.Close()

	_, err := New(server.URL, "me", "t").Search(context.Background(), "status = Nope")
	if err == nil || !strings.Contains(err.Error(), "does not exist for the field 'status'") {
		t.Errorf("err = %v", err)
	}
}

func TestAssignedJQLEscapesQuotes(t *testing.T) {
	got := AssignedJQL(`Say "hi"`)
	if !strings.Contains(got, `sprint = "Say \"hi\""`) {
		t.Errorf("jql = %q", got)
	}
}

func TestNewAddsScheme(t *testing.T) {
	if got := New("geekee.atlassian.net/", "", "").BaseURL; got != "https://geekee.atlassian.net" {
		t.Errorf("base = %q", got)
	}
}

func TestMyselfReturnsDisplayName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/rest/api/3/myself" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if _, pass, _ := r.BasicAuth(); pass != "secret" {
			t.Errorf("token = %q", pass)
		}
		w.Write([]byte(`{"accountId":"abc","displayName":"Mark Mejo"}`))
	}))
	defer server.Close()

	name, err := New(server.URL, "me@example.com", "secret").Myself(context.Background())
	if err != nil || name != "Mark Mejo" {
		t.Errorf("name = %q err = %v", name, err)
	}
}

func TestGetTicketReadsDescriptionPointsAndComments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/issue/GEEK-7" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if fields := r.URL.Query().Get("fields"); !strings.Contains(fields, "comment") || !strings.Contains(fields, FieldStoryPoints) {
			t.Errorf("fields = %q", fields)
		}
		w.Write([]byte(`{"key":"GEEK-7","fields":{
			"summary":"SDK",
			"status":{"name":"To Do"},
			"description":{"type":"doc","content":[
				{"type":"heading","content":[{"type":"text","text":"Problems"}]},
				{"type":"rule"},
				{"type":"paragraph","content":[{"type":"text","text":"APIs cannot talk to StarVault"}]}
			]},
			"customfield_10026":5,
			"customfield_10284":"Total: High",
			"comment":{"comments":[{"author":{"displayName":"PO"},"body":{"type":"doc","content":[
				{"type":"paragraph","content":[{"type":"text","text":"D23 to D25, D01"}]}
			]}}]}
		}}`))
	}))
	defer server.Close()

	ticket, err := New(server.URL, "me", "t").GetTicket(context.Background(), "GEEK-7")
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Description != "Problems\n\nAPIs cannot talk to StarVault" {
		t.Errorf("description = %q", ticket.Description)
	}
	if ticket.Points == nil || *ticket.Points != 5 || ticket.StoryPointCriteria != "Total: High" {
		t.Errorf("points = %v criteria = %q", ticket.Points, ticket.StoryPointCriteria)
	}
	if len(ticket.Comments) != 1 || ticket.Comments[0] != (Comment{Author: "PO", Body: "D23 to D25, D01"}) {
		t.Errorf("comments = %#v", ticket.Comments)
	}
}

func TestParseKey(t *testing.T) {
	for input, want := range map[string]string{
		"GEEK-11963":   "GEEK-11963",
		" geek-11963 ": "GEEK-11963",
		"11963":        "GEEK-11963",
		"https://geekee.atlassian.net/browse/GEEK-11963":                                         "GEEK-11963",
		"https://geekee.atlassian.net/jira/software/projects/GEEK/boards/5?selectedIssue=GEEK-7": "GEEK-7",
	} {
		if got, ok := ParseKey(input, "GEEK"); !ok || got != want {
			t.Errorf("ParseKey(%q) = %q %v, want %q", input, got, ok, want)
		}
	}
	for _, input := range []string{"", "hello", "-5", "GEEK-"} {
		if got, ok := ParseKey(input, "GEEK"); ok {
			t.Errorf("ParseKey(%q) = %q, want no key", input, got)
		}
	}
}

func TestUpdateSendsPlainTextFieldsThroughV2(t *testing.T) {
	var got struct {
		Fields map[string]any `json:"fields"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/rest/api/2/issue/GEEK-1" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	err := New(server.URL, "me", "t").Update(context.Background(), "GEEK-1", Refinement{
		AcceptanceCriteria:      "* Clicking Delete Account is recorded.",
		TechnicalImplementation: "dating\n\n* pages/[uuid].vue",
		StoryPointCriteria:      "Total: Low",
		Points:                  3,
	})
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]any{
		FieldAcceptanceCriteria:      "* Clicking Delete Account is recorded.",
		FieldTechnicalImplementation: "dating\n\n* pages/[uuid].vue",
		FieldStoryPointCriteria:      "Total: Low",
		FieldStoryPoints:             float64(3),
	}
	if len(got.Fields) != len(want) {
		t.Errorf("fields = %v", got.Fields)
	}
	for field, value := range want {
		if got.Fields[field] != value {
			t.Errorf("%s = %#v, want %#v", field, got.Fields[field], value)
		}
	}
}

func TestUpdateReportsJiraErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"errors":{"customfield_10026":"Field 'customfield_10026' cannot be set."}}`))
	}))
	defer server.Close()

	err := New(server.URL, "me", "t").Update(context.Background(), "GEEK-1", Refinement{})
	if err == nil || !strings.Contains(err.Error(), "cannot be set") {
		t.Errorf("err = %v", err)
	}
}

func TestMoveToSprintFindsTheSprintOnTheTicketsBoard(t *testing.T) {
	var moved []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/issue/GEEK-1":
			w.Write([]byte(`{"fields":{"customfield_10020":[{"id":1493,"name":"Refinement Ready - Team John","boardId":5}]}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/agile/1.0/board/5/sprint":
			if r.URL.Query().Get("startAt") == "0" {
				w.Write([]byte(`{"isLast":false,"values":[{"id":1817,"name":"Sep 28 - Oct 12"},{"id":1493,"name":"Refinement Ready - Team John"}]}`))
				return
			}
			w.Write([]byte(`{"isLast":true,"values":[{"id":1323,"name":"Ready for Sprint - Team John"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/agile/1.0/sprint/1323/issue":
			var body struct {
				Issues []string `json:"issues"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			moved = body.Issues
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	if err := New(server.URL, "me", "t").MoveToSprint(context.Background(), "GEEK-1", "Ready for Sprint - Team John"); err != nil {
		t.Fatal(err)
	}
	if len(moved) != 1 || moved[0] != "GEEK-1" {
		t.Errorf("moved = %v", moved)
	}
}

func TestMoveToSprintReportsAMissingSprint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/rest/api/3/issue/") {
			w.Write([]byte(`{"fields":{"customfield_10020":[{"boardId":5}]}}`))
			return
		}
		w.Write([]byte(`{"isLast":true,"values":[{"id":1,"name":"Other"}]}`))
	}))
	defer server.Close()

	err := New(server.URL, "me", "t").MoveToSprint(context.Background(), "GEEK-1", "Ready for Sprint - Team John")
	if err == nil || !strings.Contains(err.Error(), `no open sprint named "Ready for Sprint - Team John" on board 5`) {
		t.Errorf("err = %v", err)
	}
}

func TestMoveToSprintNeedsATicketInASprint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"fields":{"customfield_10020":null}}`))
	}))
	defer server.Close()

	err := New(server.URL, "me", "t").MoveToSprint(context.Background(), "GEEK-1", "Ready")
	if err == nil || !strings.Contains(err.Error(), "in no sprint") {
		t.Errorf("err = %v", err)
	}
}
