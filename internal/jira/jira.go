// Package jira reads and writes refinement tickets through the Jira Cloud REST API.
package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
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
	Points      *float64
	Comments    []Comment
}

// Refinement is what refine writes back to a ticket.
type Refinement struct {
	AcceptanceCriteria      string
	TechnicalImplementation string
	StoryPointCriteria      string
	Points                  int
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

// AssignedJQL lists the current user's tickets in a sprint, newest first.
func AssignedJQL(sprint string) string {
	return fmt.Sprintf(`assignee = currentUser() AND sprint = %s ORDER BY updated DESC`, quote(sprint))
}

// Search runs a JQL query and returns the matching issues.
func (c *Client) Search(ctx context.Context, jql string) ([]Issue, error) {
	body, err := json.Marshal(map[string]any{
		"jql":        jql,
		"maxResults": maxResults,
		"fields": []string{
			"summary", "status",
			FieldAcceptanceCriteria, FieldTechnicalImplementation, FieldStoryPointCriteria,
		},
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
				AcceptanceCriteria      json.RawMessage `json:"customfield_10042"`
				TechnicalImplementation json.RawMessage `json:"customfield_10043"`
				StoryPointCriteria      json.RawMessage `json:"customfield_10284"`
			} `json:"fields"`
		} `json:"issues"`
	}
	if err := c.do(ctx, http.MethodPost, "/rest/api/3/search/jql", body, &page); err != nil {
		return nil, err
	}

	issues := make([]Issue, 0, len(page.Issues))
	for _, raw := range page.Issues {
		issues = append(issues, Issue{
			Key:                     raw.Key,
			Summary:                 raw.Fields.Summary,
			Status:                  raw.Fields.Status.Name,
			AcceptanceCriteria:      fieldText(raw.Fields.AcceptanceCriteria),
			TechnicalImplementation: fieldText(raw.Fields.TechnicalImplementation),
			StoryPointCriteria:      fieldText(raw.Fields.StoryPointCriteria),
		})
	}
	return issues, nil
}

// GetTicket reads one ticket with its description, points and comments.
func (c *Client) GetTicket(ctx context.Context, key string) (Ticket, error) {
	fields := strings.Join([]string{
		"summary", "status", "description", "comment", FieldStoryPoints,
		FieldAcceptanceCriteria, FieldTechnicalImplementation, FieldStoryPointCriteria,
	}, ",")

	var raw struct {
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
			Comment                 struct {
				Comments []struct {
					Author struct {
						DisplayName string `json:"displayName"`
					} `json:"author"`
					Body json.RawMessage `json:"body"`
				} `json:"comments"`
			} `json:"comment"`
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
		Description: fieldText(raw.Fields.Description),
		Points:      raw.Fields.Points,
	}
	for _, comment := range raw.Fields.Comment.Comments {
		ticket.Comments = append(ticket.Comments, Comment{
			Author: comment.Author.DisplayName,
			Body:   fieldText(comment.Body),
		})
	}
	return ticket, nil
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

// MoveToSprint moves a ticket to the sprint with the given name, on the board
// of the sprint the ticket is in now.
func (c *Client) MoveToSprint(ctx context.Context, key, sprint string) error {
	board, err := c.boardOf(ctx, key)
	if err != nil {
		return err
	}
	id, err := c.sprintID(ctx, board, sprint)
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{"issues": []string{key}})
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPost, "/rest/agile/1.0/sprint/"+strconv.Itoa(id)+"/issue", body, nil)
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
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return responseError(resp)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s: %w", strings.SplitN(path, "?", 2)[0], err)
	}
	return nil
}

func responseError(resp *http.Response) error {
	var body struct {
		ErrorMessages []string          `json:"errorMessages"`
		Errors        map[string]string `json:"errors"`
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	_ = json.Unmarshal(data, &body)

	messages := body.ErrorMessages
	for _, msg := range body.Errors {
		messages = append(messages, msg)
	}
	if len(messages) == 0 {
		return fmt.Errorf("jira: %s", resp.Status)
	}
	return fmt.Errorf("jira: %s: %s", resp.Status, strings.Join(messages, "; "))
}

func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
