// Package jira reads and writes refinement tickets through the Jira Cloud REST API.
package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Field ids of the three refinement fields.
const (
	FieldAcceptanceCriteria      = "customfield_10042"
	FieldTechnicalImplementation = "customfield_10043"
	FieldStoryPointCriteria      = "customfield_10284"
	FieldStoryPoints             = "customfield_10026"
	FieldSprint                  = "customfield_10020"
)

// maxResults caps the home list; a refinement column never holds more.
const maxResults = 100

// Issue is a ticket with the fields refinement reads.
type Issue struct {
	Key                     string
	Summary                 string
	Status                  string
	AcceptanceCriteria      string
	TechnicalImplementation string
	StoryPointCriteria      string
}

// Ticket is an issue with everything a refinement prompt needs.
type Ticket struct {
	Issue
	Description string
	// DescriptionMarkdown is the description with its formatting, for
	// showing; prompts use Description.
	DescriptionMarkdown string
	// Parent is the epic or parent ticket's key, empty when it has none.
	Parent   string
	Points   *float64
	Labels   []string
	Comments []Comment
	Links    []Link
}

// Link is another ticket linked to this one, read from this ticket's side,
// for example "is blocked by".
type Link struct {
	// Type is the link type's name, e.g. "Blocks" or "Relates".
	Type     string `json:"type"`
	Relation string `json:"relation"`
	Key      string `json:"key"`
	Summary  string `json:"summary"`
	Status   string `json:"status"`
	// IssueType is the other ticket's type, e.g. "Task".
	IssueType string `json:"issue_type"`
}

// Refinement is what refine writes back to a ticket.
type Refinement struct {
	AcceptanceCriteria      string
	TechnicalImplementation string
	StoryPointCriteria      string
	Points                  int
}

// SplitTarget is what each split ticket copies from its parent: the project,
// labels, priority and assignee, and the sprint it goes into.
type SplitTarget struct {
	Parent     string
	Summary    string
	Project    string
	Labels     []string
	PriorityID string
	AssigneeID string
	SprintID   int
}

// NewChild is one split ticket to create.
type NewChild struct {
	Summary     string
	Description string
	Refinement
}

// Comment is one comment on a ticket.
type Comment struct {
	Author string
	Body   string
}

// Client talks to one Jira site with basic auth.
type Client struct {
	BaseURL string
	Email   string
	Token   string
	HTTP    *http.Client
}

// New returns a client for a site such as "geekee.atlassian.net".
func New(site, email, token string) *Client {
	base := site
	if !strings.Contains(base, "://") {
		base = "https://" + base
	}
	return &Client{
		BaseURL: strings.TrimRight(base, "/"),
		Email:   email,
		Token:   token,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

var keyPattern = regexp.MustCompile(`(?i)\b([a-z][a-z0-9_]*)-([0-9]+)\b`)

// ParseKey reads a ticket key from "GEEK-123", "geek-123", a browse URL, or
// a bare number in the given project.
func ParseKey(input, project string) (string, bool) {
	input = strings.TrimSpace(input)
	if match := keyPattern.FindStringSubmatch(input); match != nil {
		return strings.ToUpper(match[1]) + "-" + match[2], true
	}
	if _, err := strconv.Atoi(input); err == nil && project != "" && !strings.HasPrefix(input, "-") {
		return strings.ToUpper(project) + "-" + input, true
	}
	return "", false
}

// ListJQL lists the current user's unresolved tickets in a sprint, most
// recently updated first.
func ListJQL(sprint string) string {
	return fmt.Sprintf(`assignee = currentUser() AND resolution = Unresolved AND sprint = %s ORDER BY updated DESC`, quote(sprint))
}

// ReadyJQL lists the current user's unresolved tickets in the ready sprint,
// leaving out epics, most recently updated first.
func ReadyJQL(sprint string) string {
	return fmt.Sprintf(`assignee = currentUser() AND resolution = Unresolved AND sprint = %s AND issuetype != Epic ORDER BY updated DESC`, quote(sprint))
}

// CandidatesJQL lists the current user's unresolved tickets in the given
// sprints other than key: the tickets a blocker or overlap is looked for in.
func CandidatesJQL(key string, sprints ...string) string {
	quoted := make([]string, len(sprints))
	for i, sprint := range sprints {
		quoted[i] = quote(sprint)
	}
	return fmt.Sprintf(`assignee = currentUser() AND resolution = Unresolved AND sprint in (%s) AND key != %s ORDER BY key ASC`, strings.Join(quoted, ", "), quote(key))
}

// Tickets runs a JQL query and returns the matching tickets with their
// descriptions, points and links, but no comments.
func (c *Client) Tickets(ctx context.Context, jql string) ([]Ticket, error) {
	return c.search(ctx, jql, "description", "issuelinks")
}

// Linked runs a JQL query and returns the matching tickets with their links,
// but no descriptions.
func (c *Client) Linked(ctx context.Context, jql string) ([]Ticket, error) {
	return c.search(ctx, jql, "issuelinks")
}

func (c *Client) search(ctx context.Context, jql string, extra ...string) ([]Ticket, error) {
	body, err := json.Marshal(map[string]any{
		"jql":        jql,
		"maxResults": maxResults,
		"fields": append([]string{
			"summary", "status", FieldStoryPoints,
			FieldAcceptanceCriteria, FieldTechnicalImplementation, FieldStoryPointCriteria,
		}, extra...),
	})
	if err != nil {
		return nil, err
	}

	var page struct {
		Issues []struct {
			Key    string `json:"key"`
			Fields struct {
				Summary string `json:"summary"`
				Status  struct {
					Name string `json:"name"`
				} `json:"status"`
				Description             json.RawMessage `json:"description"`
				Points                  *float64        `json:"customfield_10026"`
				AcceptanceCriteria      json.RawMessage `json:"customfield_10042"`
				TechnicalImplementation json.RawMessage `json:"customfield_10043"`
				StoryPointCriteria      json.RawMessage `json:"customfield_10284"`
				IssueLinks              []rawLink       `json:"issuelinks"`
			} `json:"fields"`
		} `json:"issues"`
	}
	if err := c.do(ctx, http.MethodPost, "/rest/api/3/search/jql", body, &page); err != nil {
		return nil, err
	}

	tickets := make([]Ticket, 0, len(page.Issues))
	for _, raw := range page.Issues {
		ticket := Ticket{
			Issue: Issue{
				Key:                     raw.Key,
				Summary:                 raw.Fields.Summary,
				Status:                  raw.Fields.Status.Name,
				AcceptanceCriteria:      fieldText(raw.Fields.AcceptanceCriteria),
				TechnicalImplementation: fieldText(raw.Fields.TechnicalImplementation),
				StoryPointCriteria:      fieldText(raw.Fields.StoryPointCriteria),
			},
			Description: fieldText(raw.Fields.Description),
			Points:      raw.Fields.Points,
			Links:       links(raw.Fields.IssueLinks),
		}
		tickets = append(tickets, ticket)
	}
	return tickets, nil
}

// Ticket reads one ticket with its description, points and comments.
func (c *Client) Ticket(ctx context.Context, key string) (Ticket, error) {
	fields := strings.Join([]string{
		"summary", "status", "description", "labels", "comment", "issuelinks", "parent", FieldStoryPoints,
		FieldAcceptanceCriteria, FieldTechnicalImplementation, FieldStoryPointCriteria,
	}, ",")

	var raw struct {
		Key    string `json:"key"`
		Fields struct {
			Summary string `json:"summary"`
			Status  struct {
				Name string `json:"name"`
			} `json:"status"`
			Description json.RawMessage `json:"description"`
			Labels      []string        `json:"labels"`
			Parent      *struct {
				Key string `json:"key"`
			} `json:"parent"`
			Points                  *float64        `json:"customfield_10026"`
			AcceptanceCriteria      json.RawMessage `json:"customfield_10042"`
			TechnicalImplementation json.RawMessage `json:"customfield_10043"`
			StoryPointCriteria      json.RawMessage `json:"customfield_10284"`
			Comment                 struct {
				Comments []struct {
					Author struct {
						DisplayName string `json:"displayName"`
					} `json:"author"`
					Body json.RawMessage `json:"body"`
				} `json:"comments"`
			} `json:"comment"`
			IssueLinks []rawLink `json:"issuelinks"`
		} `json:"fields"`
	}
	path := "/rest/api/3/issue/" + url.PathEscape(key) + "?fields=" + url.QueryEscape(fields)
	if err := c.do(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return Ticket{}, err
	}

	ticket := Ticket{
		Issue: Issue{
			Key:                     raw.Key,
			Summary:                 raw.Fields.Summary,
			Status:                  raw.Fields.Status.Name,
			AcceptanceCriteria:      fieldText(raw.Fields.AcceptanceCriteria),
			TechnicalImplementation: fieldText(raw.Fields.TechnicalImplementation),
			StoryPointCriteria:      fieldText(raw.Fields.StoryPointCriteria),
		},
		Description:         fieldText(raw.Fields.Description),
		DescriptionMarkdown: fieldMarkdown(raw.Fields.Description),
		Points:              raw.Fields.Points,
		Labels:              raw.Fields.Labels,
	}
	if raw.Fields.Parent != nil {
		ticket.Parent = raw.Fields.Parent.Key
	}
	for _, comment := range raw.Fields.Comment.Comments {
		ticket.Comments = append(ticket.Comments, Comment{
			Author: comment.Author.DisplayName,
			Body:   fieldText(comment.Body),
		})
	}
	ticket.Links = links(raw.Fields.IssueLinks)
	return ticket, nil
}

// rawLink is an issue link as Jira returns it.
type rawLink struct {
	Type struct {
		Name    string `json:"name"`
		Inward  string `json:"inward"`
		Outward string `json:"outward"`
	} `json:"type"`
	InwardIssue  *linkedIssue `json:"inwardIssue"`
	OutwardIssue *linkedIssue `json:"outwardIssue"`
}

// links reads each link from this ticket's side.
func links(raw []rawLink) []Link {
	var out []Link
	for _, link := range raw {
		relation, other := link.Type.Outward, link.OutwardIssue
		if other == nil {
			relation, other = link.Type.Inward, link.InwardIssue
		}
		if other == nil {
			continue
		}
		out = append(out, Link{
			Type:      link.Type.Name,
			Relation:  relation,
			Key:       other.Key,
			Summary:   other.Fields.Summary,
			Status:    other.Fields.Status.Name,
			IssueType: other.Fields.IssueType.Name,
		})
	}
	return out
}

// linkedIssue is the other ticket in an issue link.
type linkedIssue struct {
	Key    string `json:"key"`
	Fields struct {
		Summary string `json:"summary"`
		Status  struct {
			Name string `json:"name"`
		} `json:"status"`
		IssueType struct {
			Name string `json:"name"`
		} `json:"issuetype"`
	} `json:"fields"`
}

// Update writes the refinement fields to a ticket. The text fields render as
// plain text, so it goes through v2, which stores strings exactly as given
// where v3 would want Atlassian Document Format.
func (c *Client) Update(ctx context.Context, key string, r Refinement) error {
	body, err := json.Marshal(map[string]any{"fields": map[string]any{
		FieldAcceptanceCriteria:      r.AcceptanceCriteria,
		FieldTechnicalImplementation: r.TechnicalImplementation,
		FieldStoryPointCriteria:      r.StoryPointCriteria,
		FieldStoryPoints:             r.Points,
	}})
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPut, "/rest/api/2/issue/"+url.PathEscape(key), body, nil)
}

// Relate links two tickets with a Relates link, unless they are linked
// already.
func (c *Client) Relate(ctx context.Context, from, to string) error {
	ticket, err := c.Ticket(ctx, from)
	if err != nil {
		return err
	}
	if ticket.LinkedTo(to) {
		return nil
	}
	body, err := json.Marshal(map[string]any{
		"type":         map[string]string{"name": "Relates"},
		"inwardIssue":  map[string]string{"key": from},
		"outwardIssue": map[string]string{"key": to},
	})
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPost, "/rest/api/2/issueLink", body, nil)
}

// LinkedTo reports whether the ticket has any link to key.
func (t Ticket) LinkedTo(key string) bool {
	for _, link := range t.Links {
		if link.Key == key {
			return true
		}
	}
	return false
}

// Refined reports whether the issue has its Acceptance Criteria and
// Technical Implementation filled.
func (i Issue) Refined() bool {
	return strings.TrimSpace(i.AcceptanceCriteria) != "" && strings.TrimSpace(i.TechnicalImplementation) != ""
}

// Comment adds a comment to a ticket. Through v2 the body is wiki markup.
func (c *Client) Comment(ctx context.Context, key, body string) error {
	data, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPost, "/rest/api/2/issue/"+url.PathEscape(key)+"/comment", data, nil)
}

// HasComment reports whether the ticket has a comment saying body. Comments
// come back as plain text, so only their letters and digits are compared.
func (t Ticket) HasComment(body string) bool {
	want := alnum(body)
	for _, comment := range t.Comments {
		if alnum(comment.Body) == want {
			return true
		}
	}
	return false
}

func alnum(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, s)
}

// MoveToSprint moves a ticket to the sprint with the given name, on the board
// of the sprint the ticket is in now.
func (c *Client) MoveToSprint(ctx context.Context, key, sprint string) error {
	id, err := c.sprintOf(ctx, key, sprint)
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{"issues": []string{key}})
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPost, "/rest/agile/1.0/sprint/"+strconv.Itoa(id)+"/issue", body, nil)
}

// Split reads what split tickets of parent copy from it, with the id of the
// named sprint on the parent's board.
func (c *Client) Split(ctx context.Context, parent, sprint string) (SplitTarget, error) {
	var raw struct {
		Key    string `json:"key"`
		Fields struct {
			Summary string `json:"summary"`
			Project struct {
				Key string `json:"key"`
			} `json:"project"`
			Labels   []string `json:"labels"`
			Priority *struct {
				ID string `json:"id"`
			} `json:"priority"`
			Assignee *struct {
				AccountID string `json:"accountId"`
			} `json:"assignee"`
		} `json:"fields"`
	}
	path := "/rest/api/3/issue/" + url.PathEscape(parent) + "?fields=summary,project,labels,priority,assignee"
	if err := c.do(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return SplitTarget{}, err
	}
	id, err := c.sprintOf(ctx, parent, sprint)
	if err != nil {
		return SplitTarget{}, err
	}
	target := SplitTarget{
		Parent:   raw.Key,
		Summary:  raw.Fields.Summary,
		Project:  raw.Fields.Project.Key,
		Labels:   raw.Fields.Labels,
		SprintID: id,
	}
	if raw.Fields.Priority != nil {
		target.PriorityID = raw.Fields.Priority.ID
	}
	if raw.Fields.Assignee != nil {
		target.AssigneeID = raw.Fields.Assignee.AccountID
	}
	return target, nil
}

// CreateChild creates a split ticket as a Task in the target's sprint, linked
// to the parent with Relates rather than as a subtask, so a carried-over
// child never holds the parent open. It returns the new key. Story points are
// not on the create screen, so they are written after; when that fails the
// ticket exists, and its key comes back with the error.
//
// A Task already related to the parent with the same summary is taken as
// this child, created by an earlier try whose response never arrived, and
// only its points are written.
func (c *Client) CreateChild(ctx context.Context, target SplitTarget, child NewChild) (string, error) {
	key, err := c.existingChild(ctx, target.Parent, child.Summary)
	if err != nil {
		return "", err
	}
	if key == "" {
		if key, err = c.createChild(ctx, target, child); err != nil {
			return "", err
		}
	}
	points, err := json.Marshal(map[string]any{"fields": map[string]any{FieldStoryPoints: child.Points}})
	if err != nil {
		return key, err
	}
	if err := c.do(ctx, http.MethodPut, "/rest/api/2/issue/"+url.PathEscape(key), points, nil); err != nil {
		return key, fmt.Errorf("created %s, but could not set its points: %w", key, err)
	}
	return key, nil
}

// existingChild finds a Task related to parent with the given summary.
func (c *Client) existingChild(ctx context.Context, parent, summary string) (string, error) {
	var raw struct {
		Fields struct {
			IssueLinks []rawLink `json:"issuelinks"`
		} `json:"fields"`
	}
	if err := c.do(ctx, http.MethodGet, "/rest/api/3/issue/"+url.PathEscape(parent)+"?fields=issuelinks", nil, &raw); err != nil {
		return "", err
	}
	for _, link := range links(raw.Fields.IssueLinks) {
		if link.Type == "Relates" && link.IssueType == "Task" && link.Summary == summary {
			return link.Key, nil
		}
	}
	return "", nil
}

// createChild creates the split ticket and returns its key.
func (c *Client) createChild(ctx context.Context, target SplitTarget, child NewChild) (string, error) {
	fields := map[string]any{
		"project":                    map[string]string{"key": target.Project},
		"issuetype":                  map[string]string{"name": "Task"},
		"summary":                    child.Summary,
		"description":                child.Description,
		FieldAcceptanceCriteria:      child.AcceptanceCriteria,
		FieldTechnicalImplementation: child.TechnicalImplementation,
		FieldSprint:                  target.SprintID,
	}
	if len(target.Labels) > 0 {
		fields["labels"] = target.Labels
	}
	if child.StoryPointCriteria != "" {
		fields[FieldStoryPointCriteria] = child.StoryPointCriteria
	}
	if target.PriorityID != "" {
		fields["priority"] = map[string]string{"id": target.PriorityID}
	}
	if target.AssigneeID != "" {
		fields["assignee"] = map[string]string{"accountId": target.AssigneeID}
	}
	body, err := json.Marshal(map[string]any{
		"fields": fields,
		"update": map[string]any{"issuelinks": []any{map[string]any{"add": map[string]any{
			"type":         map[string]string{"name": "Relates"},
			"outwardIssue": map[string]string{"key": target.Parent},
		}}}},
	})
	if err != nil {
		return "", err
	}
	var created struct {
		Key string `json:"key"`
	}
	if err := c.do(ctx, http.MethodPost, "/rest/api/2/issue", body, &created); err != nil {
		return "", err
	}
	return created.Key, nil
}

// sprintOf finds the open sprint with the given name on the board of the
// ticket's latest sprint.
func (c *Client) sprintOf(ctx context.Context, key, sprint string) (int, error) {
	board, err := c.boardOf(ctx, key)
	if err != nil {
		return 0, err
	}
	return c.sprintID(ctx, board, sprint)
}

// boardOf is the board of the ticket's latest sprint.
func (c *Client) boardOf(ctx context.Context, key string) (int, error) {
	var raw struct {
		Fields struct {
			Sprints []struct {
				BoardID int `json:"boardId"`
			} `json:"customfield_10020"`
		} `json:"fields"`
	}
	path := "/rest/api/3/issue/" + url.PathEscape(key) + "?fields=" + FieldSprint
	if err := c.do(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return 0, err
	}
	sprints := raw.Fields.Sprints
	if len(sprints) == 0 || sprints[len(sprints)-1].BoardID == 0 {
		return 0, fmt.Errorf("%s is in no sprint, so its board is unknown", key)
	}
	return sprints[len(sprints)-1].BoardID, nil
}

// sprintID finds an open sprint on a board by name.
func (c *Client) sprintID(ctx context.Context, board int, name string) (int, error) {
	for start := 0; ; {
		var page struct {
			IsLast bool `json:"isLast"`
			Values []struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
			} `json:"values"`
		}
		path := fmt.Sprintf("/rest/agile/1.0/board/%d/sprint?state=active,future&startAt=%d", board, start)
		if err := c.do(ctx, http.MethodGet, path, nil, &page); err != nil {
			return 0, err
		}
		for _, sprint := range page.Values {
			if sprint.Name == name {
				return sprint.ID, nil
			}
		}
		if page.IsLast || len(page.Values) == 0 {
			return 0, fmt.Errorf("no open sprint named %q on board %d", name, board)
		}
		start += len(page.Values)
	}
}

// Myself returns the display name of the token's user.
func (c *Client) Myself(ctx context.Context) (string, error) {
	var user struct {
		DisplayName string `json:"displayName"`
	}
	if err := c.do(ctx, http.MethodGet, "/rest/api/3/myself", nil, &user); err != nil {
		return "", err
	}
	return user.DisplayName, nil
}

// do sends an authenticated request and decodes a successful response into
// out, when out is set.
func (c *Client) do(ctx context.Context, method, path string, body []byte, out any) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.Email, c.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		// Read to EOF so the connection is reused.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return responseError(resp)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		route, _, _ := strings.Cut(path, "?")
		return fmt.Errorf("decode %s: %w", route, err)
	}
	return nil
}

// APIError is a response from Jira outside 2xx. Check for it with errors.As
// to tell a missing ticket (404) from a bad token (401) or a rate limit (429).
type APIError struct {
	StatusCode int
	// Messages are Jira's error messages, then its field errors in field
	// order, so the text is the same on every run.
	Messages []string
}

func (e *APIError) Error() string {
	status := fmt.Sprintf("%d %s", e.StatusCode, http.StatusText(e.StatusCode))
	if len(e.Messages) == 0 {
		return "jira: " + status
	}
	return "jira: " + status + ": " + strings.Join(e.Messages, "; ")
}

func responseError(resp *http.Response) error {
	var body struct {
		ErrorMessages []string          `json:"errorMessages"`
		Errors        map[string]string `json:"errors"`
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	_ = json.Unmarshal(data, &body)

	messages := body.ErrorMessages
	for _, field := range slices.Sorted(maps.Keys(body.Errors)) {
		messages = append(messages, body.Errors[field])
	}
	return &APIError{StatusCode: resp.StatusCode, Messages: messages}
}

func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
