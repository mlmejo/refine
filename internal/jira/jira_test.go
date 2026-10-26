package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
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
	issues, err := client.Search(context.Background(), ListJQL("Refinement Ready - Team John"))
	if err != nil {
		t.Fatal(err)
	}

	if got.JQL != `assignee = currentUser() AND resolution = Unresolved AND sprint = "Refinement Ready - Team John" ORDER BY updated DESC` {
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

func TestListJQLEscapesQuotes(t *testing.T) {
	got := ListJQL(`Say "hi"`)
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

func TestTicketReadsDescriptionPointsCommentsAndLinks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/issue/GEEK-7" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if fields := r.URL.Query().Get("fields"); !strings.Contains(fields, "comment") || !strings.Contains(fields, "labels") || !strings.Contains(fields, FieldStoryPoints) {
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
			"parent":{"key":"GEEK-1"},
			"labels":["Dating_Brands","SDK"],
			"customfield_10026":5,
			"customfield_10284":"Total: High",
			"comment":{"comments":[{"author":{"displayName":"PO"},"body":{"type":"doc","content":[
				{"type":"paragraph","content":[{"type":"text","text":"D23 to D25, D01"}]}
			]}}]},
			"issuelinks":[
				{"type":{"inward":"is blocked by","outward":"blocks"},"inwardIssue":{"key":"GEEK-5","fields":{"summary":"Notifications view","status":{"name":"In Progress"}}}},
				{"type":{"inward":"is cloned by","outward":"clones"},"outwardIssue":{"key":"GEEK-3","fields":{"summary":"Old SDK","status":{"name":"Done"}}}}
			]
		}}`))
	}))
	defer server.Close()

	ticket, err := New(server.URL, "me", "t").Ticket(context.Background(), "GEEK-7")
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Description != "Problems\n\nAPIs cannot talk to StarVault" {
		t.Errorf("description = %q", ticket.Description)
	}
	if ticket.DescriptionMarkdown != "## Problems\n\n---\n\nAPIs cannot talk to StarVault" {
		t.Errorf("description markdown = %q", ticket.DescriptionMarkdown)
	}
	if ticket.Parent != "GEEK-1" {
		t.Errorf("parent = %q", ticket.Parent)
	}
	if !slices.Equal(ticket.Labels, []string{"Dating_Brands", "SDK"}) {
		t.Errorf("labels = %q", ticket.Labels)
	}
	if ticket.Points == nil || *ticket.Points != 5 || ticket.StoryPointCriteria != "Total: High" {
		t.Errorf("points = %v criteria = %q", ticket.Points, ticket.StoryPointCriteria)
	}
	if len(ticket.Comments) != 1 || ticket.Comments[0] != (Comment{Author: "PO", Body: "D23 to D25, D01"}) {
		t.Errorf("comments = %#v", ticket.Comments)
	}
	want := []Link{
		{Relation: "is blocked by", Key: "GEEK-5", Summary: "Notifications view", Status: "In Progress"},
		{Relation: "clones", Key: "GEEK-3", Summary: "Old SDK", Status: "Done"},
	}
	if len(ticket.Links) != 2 || ticket.Links[0] != want[0] || ticket.Links[1] != want[1] {
		t.Errorf("links = %#v", ticket.Links)
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

func TestSplitCopiesTheParentAndFindsTheSprint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/3/issue/GEEK-1":
			w.Write([]byte(`{"key":"GEEK-1","fields":{
				"summary":"Book Management",
				"project":{"key":"GEEK"},
				"labels":["Dating_Brands"],
				"priority":{"id":"3"},
				"assignee":{"accountId":"abc"},
				"customfield_10020":[{"id":1493,"boardId":5}]
			}}`))
		case r.URL.Path == "/rest/agile/1.0/board/5/sprint":
			w.Write([]byte(`{"isLast":true,"values":[{"id":1323,"name":"Ready for Sprint - Team John"}]}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	target, err := New(server.URL, "me", "t").Split(context.Background(), "GEEK-1", "Ready for Sprint - Team John")
	if err != nil {
		t.Fatal(err)
	}
	want := SplitTarget{Parent: "GEEK-1", Summary: "Book Management", Project: "GEEK", Labels: []string{"Dating_Brands"}, PriorityID: "3", AssigneeID: "abc", SprintID: 1323}
	if fmt.Sprint(target) != fmt.Sprint(want) {
		t.Errorf("target = %+v, want %+v", target, want)
	}
}

func TestCreateChildCreatesALinkedTaskThenSetsPoints(t *testing.T) {
	var created map[string]any
	var points map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/2/issue":
			json.NewDecoder(r.Body).Decode(&created)
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"id":"42702","key":"GEEK-12"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/2/issue/GEEK-12":
			json.NewDecoder(r.Body).Decode(&points)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	target := SplitTarget{Parent: "GEEK-1", Project: "GEEK", Labels: []string{"Dating_Brands"}, PriorityID: "3", AssigneeID: "abc", SprintID: 1323}
	key, err := New(server.URL, "me", "t").CreateChild(context.Background(), target, NewChild{
		Summary:     "Book Management - Deleting Book",
		Description: "Split from GEEK-1.",
		Refinement: Refinement{
			AcceptanceCriteria:      "* As an admin, I want to delete books.",
			TechnicalImplementation: "api\n\n* app/Books.php",
			StoryPointCriteria:      "Total: Low",
			Points:                  1,
		},
	})
	if err != nil || key != "GEEK-12" {
		t.Fatalf("key %q err %v", key, err)
	}

	fields := created["fields"].(map[string]any)
	for field, want := range map[string]string{
		"summary":                    "Book Management - Deleting Book",
		"description":                "Split from GEEK-1.",
		FieldAcceptanceCriteria:      "* As an admin, I want to delete books.",
		FieldTechnicalImplementation: "api\n\n* app/Books.php",
		FieldStoryPointCriteria:      "Total: Low",
		"issuetype":                  "map[name:Task]",
		"project":                    "map[key:GEEK]",
		"priority":                   "map[id:3]",
		"assignee":                   "map[accountId:abc]",
		"labels":                     "[Dating_Brands]",
		FieldSprint:                  "1323",
	} {
		if got := fmt.Sprint(fields[field]); got != want {
			t.Errorf("%s = %q, want %q", field, got, want)
		}
	}
	if _, ok := fields[FieldStoryPoints]; ok {
		t.Error("points sent on create; they are not on the create screen")
	}
	link := fmt.Sprint(created["update"])
	if !strings.Contains(link, "name:Relates") || !strings.Contains(link, "outwardIssue:map[key:GEEK-1]") {
		t.Errorf("update = %s", link)
	}
	if got := fmt.Sprint(points); got != "map[fields:map["+FieldStoryPoints+":1]]" {
		t.Errorf("points = %s", got)
	}
}

func TestCreateChildReturnsTheKeyWhenPointsFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.Write([]byte(`{"key":"GEEK-12"}`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"errors":{"customfield_10026":"Field cannot be set."}}`))
	}))
	defer server.Close()

	key, err := New(server.URL, "me", "t").CreateChild(context.Background(), SplitTarget{Parent: "GEEK-1", Project: "GEEK"}, NewChild{Summary: "x"})
	if key != "GEEK-12" || err == nil || !strings.Contains(err.Error(), "created GEEK-12, but could not set its points") {
		t.Errorf("key %q err %v", key, err)
	}
}

func TestCandidatesJQLCoversBothSprintsWithoutTheTicket(t *testing.T) {
	got := CandidatesJQL("GEEK-101", "Refinement Ready - Team John", "Ready for Sprint - Team John")
	want := `assignee = currentUser() AND resolution = Unresolved AND sprint in ("Refinement Ready - Team John", "Ready for Sprint - Team John") AND key != "GEEK-101" ORDER BY key ASC`
	if got != want {
		t.Errorf("jql = %q", got)
	}
}

func TestTicketsReadLinks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"issues":[{"key":"GEEK-2","fields":{"summary":"Refunds","status":{"name":"To Do"},
			"issuelinks":[
				{"type":{"name":"Blocks","inward":"is blocked by","outward":"blocks"},"inwardIssue":{"key":"GEEK-1","fields":{"summary":"Stripe","status":{"name":"Done"}}}},
				{"type":{"name":"Relates","inward":"relates to","outward":"relates to"},"outwardIssue":{"key":"GEEK-3","fields":{"summary":"Email","status":{"name":"To Do"}}}}
			]}}]}`))
	}))
	defer server.Close()

	tickets, err := New(server.URL, "me", "t").Linked(context.Background(), "jql")
	if err != nil {
		t.Fatal(err)
	}
	want := []Link{
		{Type: "Blocks", Relation: "is blocked by", Key: "GEEK-1", Summary: "Stripe", Status: "Done"},
		{Type: "Relates", Relation: "relates to", Key: "GEEK-3", Summary: "Email", Status: "To Do"},
	}
	if len(tickets) != 1 || !reflect.DeepEqual(tickets[0].Links, want) {
		t.Fatalf("links = %+v", tickets)
	}
}

func TestRelateSkipsTicketsAlreadyLinked(t *testing.T) {
	var posts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Write([]byte(`{"key":"GEEK-2","fields":{"summary":"Refunds","status":{"name":"To Do"},"issuelinks":[
				{"type":{"name":"Relates","inward":"relates to","outward":"relates to"},"outwardIssue":{"key":"GEEK-1","fields":{"summary":"Stripe","status":{"name":"Done"}}}}]}}`))
			return
		}
		body, _ := io.ReadAll(r.Body)
		posts = append(posts, r.URL.Path+" "+string(body))
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	c := New(server.URL, "me", "t")
	if err := c.Relate(context.Background(), "GEEK-2", "GEEK-1"); err != nil {
		t.Fatal(err)
	}
	if err := c.Relate(context.Background(), "GEEK-2", "GEEK-9"); err != nil {
		t.Fatal(err)
	}
	if len(posts) != 1 || !strings.Contains(posts[0], `/rest/api/2/issueLink`) || !strings.Contains(posts[0], `"outwardIssue":{"key":"GEEK-9"}`) || !strings.Contains(posts[0], `"name":"Relates"`) {
		t.Fatalf("posts = %q", posts)
	}
}

func TestRefinedNeedsBothFields(t *testing.T) {
	if (Issue{AcceptanceCriteria: "* a"}).Refined() || !(Issue{AcceptanceCriteria: "* a", TechnicalImplementation: "x"}).Refined() {
		t.Fatal("Refined")
	}
}

func TestTicketsReadDescriptions(t *testing.T) {
	var got struct {
		Fields []string `json:"fields"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"issues":[
			{"key":"GEEK-1","fields":{"summary":"Echo","status":{"name":"To Do"},
				"description":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Add a listener."}]}]},
				"customfield_10042":"* No user-facing behaviour."}},
			{"key":"GEEK-2","fields":{"summary":"Bell","status":{"name":"To Do"},"description":null}}
		]}`))
	}))
	defer server.Close()

	tickets, err := New(server.URL, "me", "t").Tickets(context.Background(), "jql")
	if err != nil {
		t.Fatal(err)
	}
	if fields := strings.Join(got.Fields, ","); !strings.Contains(fields, "description") {
		t.Errorf("fields = %s", fields)
	}
	if len(tickets) != 2 || tickets[0].Description != "Add a listener." || tickets[0].AcceptanceCriteria != "* No user-facing behaviour." {
		t.Fatalf("tickets = %+v", tickets)
	}
	if tickets[1].Description != "" {
		t.Errorf("no description = %+v", tickets[1])
	}
}

func TestFieldMarkdownKeepsFormatting(t *testing.T) {
	raw := json.RawMessage(`{"type":"doc","content":[
		{"type":"paragraph","content":[
			{"type":"text","text":"Link: ","marks":[{"type":"strong"}]},
			{"type":"text","text":"https://example.com/1","marks":[{"type":"link","attrs":{"href":"https://example.com/1"}}]}
		]},
		{"type":"heading","attrs":{"level":3},"content":[{"type":"text","text":"Steps"}]},
		{"type":"bulletList","content":[
			{"type":"listItem","content":[{"type":"paragraph","content":[
				{"type":"text","text":"call "},{"type":"text","text":"track()","marks":[{"type":"code"}]},
				{"type":"text","text":" on "},{"type":"text","text":"login","marks":[{"type":"link","attrs":{"href":"https://example.com/l"}}]}
			]}]},
			{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"done","marks":[{"type":"em"}]}]}]}
		]},
		{"type":"blockquote","content":[{"type":"paragraph","content":[{"type":"text","text":"as legacy"}]}]}
	]}`)
	want := "**Link:** https://example.com/1\n\n### Steps\n\n- call `track()` on [login](https://example.com/l)\n- *done*\n\n> as legacy"
	if got := fieldMarkdown(raw); got != want {
		t.Errorf("markdown:\n%s\nwant:\n%s", got, want)
	}
	if got := fieldMarkdown(json.RawMessage(`"plain"`)); got != "plain" {
		t.Errorf("string field = %q", got)
	}
}

func TestCommentPostsWikiThroughV2(t *testing.T) {
	var path, body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.Method + " " + r.URL.Path
		var in struct {
			Body string `json:"body"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		body = in.Body
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	c := New(server.URL, "me@example.com", "token")
	if err := c.Comment(context.Background(), "GEEK-1", "* {{a}}"); err != nil {
		t.Fatal(err)
	}
	if path != "POST /rest/api/2/issue/GEEK-1/comment" || body != "* {{a}}" {
		t.Fatalf("sent %s %q", path, body)
	}
}

func TestHasCommentIgnoresMarkup(t *testing.T) {
	// The wiki comment as written, and as Jira reads it back from ADF.
	ticket := Ticket{Comments: []Comment{{Body: "Blockers found.\n\nDeploy\n* Who sets KEY?\n  * Answer: Ops"}}}
	if !ticket.HasComment("Blockers found.\n\n*Deploy*\n* Who sets {{KEY}}?\n** Answer: Ops\n") {
		t.Fatal("same comment not found")
	}
	if ticket.HasComment("Blockers found.\n\n*Deploy*\n* Who sets {{KEY}}?\n** Still open\n") {
		t.Fatal("changed comment found")
	}
}
