package gitlab

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
)

// MergeQueueEntry is retained for the shared stack UI. GitLab reports queued
// merges through merge_when_pipeline_succeeds rather than a separate object.
type MergeQueueEntry struct {
	ID string
}

type AutoMergeRequest struct {
	EnabledAt string
}

// PullRequest is the provider-neutral shape consumed by the command and TUI
// layers. Number is the GitLab project-scoped merge request IID.
type PullRequest struct {
	ID               string
	Number           int
	State            string
	URL              string
	Title            string
	Body             string
	HeadRefName      string
	BaseRefName      string
	IsDraft          bool
	Merged           bool
	MergeQueueEntry  *MergeQueueEntry
	AutoMergeRequest *AutoMergeRequest
}

func (pr *PullRequest) IsQueued() bool {
	return pr != nil && pr.MergeQueueEntry != nil && pr.MergeQueueEntry.ID != ""
}

func (pr *PullRequest) IsAutoMergeEnabled() bool {
	return pr != nil && pr.AutoMergeRequest != nil
}

// Client talks directly to the GitLab v4 REST API. It does not require glab.
type Client struct {
	http    *http.Client
	baseURL string
	host    string
	owner   string
	repo    string
	project string
	token   string
}

// NewClient creates a GitLab client. GITLAB_TOKEN is preferred; GLAB_TOKEN is
// accepted for compatibility with glab environments.
func NewClient(host, owner, repo string) (*Client, error) {
	if host == "" {
		host = "gitlab.com"
	}
	token := os.Getenv("GITLAB_TOKEN")
	if token == "" {
		token = os.Getenv("GLAB_TOKEN")
	}
	if token == "" {
		return nil, fmt.Errorf("GitLab authentication required: set GITLAB_TOKEN or GLAB_TOKEN")
	}
	return &Client{
		http:    &http.Client{Timeout: 30 * time.Second},
		baseURL: "https://" + host + "/api/v4",
		host:    host,
		owner:   owner,
		repo:    repo,
		project: url.PathEscape(owner + "/" + repo),
		token:   token,
	}, nil
}

func PRURL(host, owner, repo string, number int) string {
	if host == "" {
		host = "gitlab.com"
	}
	return fmt.Sprintf("https://%s/%s/%s/-/merge_requests/%d", host, owner, repo, number)
}

func (c *Client) request(method, path string, query url.Values, body any, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding GitLab request: %w", err)
		}
		reader = bytes.NewReader(data)
	}
	u := c.baseURL + "/" + strings.TrimPrefix(path, "/")
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequest(method, u, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("MRIVATE-TOKEN", c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("calling GitLab: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		message := strings.TrimSpace(string(data))
		var payload struct {
			Message any `json:"message"`
		}
		if json.Unmarshal(data, &payload) == nil && payload.Message != nil {
			message = fmt.Sprint(payload.Message)
		}
		return &api.HTTPError{StatusCode: resp.StatusCode, Message: message}
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decoding GitLab response: %w", err)
	}
	return nil
}

type mergeRequestWire struct {
	ID                        int    `json:"id"`
	IID                       int    `json:"iid"`
	State                     string `json:"state"`
	WebURL                    string `json:"web_url"`
	Title                     string `json:"title"`
	Description               string `json:"description"`
	SourceBranch              string `json:"source_branch"`
	TargetBranch              string `json:"target_branch"`
	Draft                     bool   `json:"draft"`
	WorkInProgress            bool   `json:"work_in_progress"`
	MergeWhenPipelineSucceeds bool   `json:"merge_when_pipeline_succeeds"`
	MergedAt                  string `json:"merged_at"`
	SHA                       string `json:"sha"`
}

func pullRequestFromWire(m mergeRequestWire) *PullRequest {
	state := strings.ToUpper(m.State)
	merged := m.State == "merged" || m.MergedAt != ""
	if merged {
		state = "MERGED"
	}
	pr := &PullRequest{
		ID: strconv.Itoa(m.IID), Number: m.IID, State: state, URL: m.WebURL,
		Title: m.Title, Body: m.Description, HeadRefName: m.SourceBranch,
		BaseRefName: m.TargetBranch, IsDraft: m.Draft || m.WorkInProgress, Merged: merged,
	}
	if m.MergeWhenPipelineSucceeds {
		pr.AutoMergeRequest = &AutoMergeRequest{EnabledAt: "enabled"}
		pr.MergeQueueEntry = &MergeQueueEntry{ID: strconv.Itoa(m.IID)}
	}
	return pr
}

func (c *Client) listMergeRequests(values url.Values) ([]mergeRequestWire, error) {
	query := make(url.Values, len(values)+2)
	for key, items := range values {
		query[key] = append([]string(nil), items...)
	}
	query.Set("per_page", "100")

	var result []mergeRequestWire
	for page := 1; ; page++ {
		query.Set("page", strconv.Itoa(page))
		var batch []mergeRequestWire
		if err := c.request(http.MethodGet, fmt.Sprintf("projects/%s/merge_requests", c.project), query, nil, &batch); err != nil {
			return nil, err
		}
		result = append(result, batch...)
		if len(batch) < 100 {
			return result, nil
		}
	}
}

func (c *Client) FindPRForBranch(branch string) (*PullRequest, error) {
	mrs, err := c.listMergeRequests(url.Values{"source_branch": {branch}, "state": {"opened"}, "order_by": {"updated_at"}, "sort": {"desc"}})
	if err != nil || len(mrs) == 0 {
		return nil, err
	}
	return pullRequestFromWire(mrs[0]), nil
}

func (c *Client) FindPRByNumber(number int) (*PullRequest, error) {
	var mr mergeRequestWire
	err := c.request(http.MethodGet, fmt.Sprintf("projects/%s/merge_requests/%d", c.project, number), nil, nil, &mr)
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return pullRequestFromWire(mr), nil
}

func (c *Client) CreatePR(base, head, title, body string, draft bool) (*PullRequest, error) {
	payload := map[string]any{"source_branch": head, "target_branch": base, "title": title, "description": body, "draft": draft}
	var mr mergeRequestWire
	if err := c.request(http.MethodPost, fmt.Sprintf("projects/%s/merge_requests", c.project), nil, payload, &mr); err != nil {
		return nil, fmt.Errorf("creating merge request: %w", err)
	}
	return pullRequestFromWire(mr), nil
}

func (c *Client) UpdatePRBase(number int, base string) error {
	return c.request(http.MethodPut, fmt.Sprintf("projects/%s/merge_requests/%d", c.project, number), nil, map[string]any{"target_branch": base}, nil)
}

func (c *Client) MarkPRReadyForReview(prID string) error {
	iid, err := strconv.Atoi(prID)
	if err != nil {
		return fmt.Errorf("invalid merge request IID %q", prID)
	}
	return c.request(http.MethodPut, fmt.Sprintf("projects/%s/merge_requests/%d", c.project, iid), nil, map[string]any{"draft": false}, nil)
}

func (c *Client) DisableAutoMerge(prID string) error {
	iid, err := strconv.Atoi(prID)
	if err != nil {
		return fmt.Errorf("invalid merge request IID %q", prID)
	}
	return c.request(http.MethodPost, fmt.Sprintf("projects/%s/merge_requests/%d/cancel_merge_when_pipeline_succeeds", c.project, iid), nil, nil, nil)
}

type PRDetails struct {
	Number   int
	State    string
	URL      string
	Title    string
	Body     string
	IsDraft  bool
	Merged   bool
	IsQueued bool
}

func (c *Client) FindPRDetailsForBranch(branch string) (*PRDetails, error) {
	mrs, err := c.listMergeRequests(url.Values{"source_branch": {branch}, "scope": {"all"}, "order_by": {"updated_at"}, "sort": {"desc"}})
	if err != nil || len(mrs) == 0 {
		return nil, err
	}
	pr := pullRequestFromWire(mrs[0])
	return &PRDetails{Number: pr.Number, State: pr.State, URL: pr.URL, Title: pr.Title, Body: pr.Body, IsDraft: pr.IsDraft, Merged: pr.Merged, IsQueued: pr.IsQueued()}, nil
}

type RemoteStackBase struct {
	Ref string `json:"ref"`
	Sha string `json:"sha,omitempty"`
}
type RemoteStackPRHead struct {
	Ref string `json:"ref"`
	Sha string `json:"sha"`
}
type RemoteStackPR struct {
	Number   int               `json:"number"`
	State    string            `json:"state"`
	Draft    bool              `json:"draft"`
	MergedAt *string           `json:"merged_at"`
	Head     RemoteStackPRHead `json:"head"`
}

func (p RemoteStackPR) IsMerged() bool { return p.MergedAt != nil && *p.MergedAt != "" }

type RemoteStack struct {
	ID           int             `json:"id"`
	Number       int             `json:"number"`
	NodeID       string          `json:"node_id"`
	URL          string          `json:"url"`
	Base         RemoteStackBase `json:"base"`
	Open         bool            `json:"open"`
	CreatedAt    string          `json:"created_at"`
	PullRequests []int           `json:"-"`
	PRDetails    []RemoteStackPR `json:"-"`
}

func (s *RemoteStack) PRNumbers() []int { return s.PullRequests }

// stacksFromMergeRequests infers stacks from GitLab's native target/source
// branch chain. The bottom merge request IID is the stable stack number.
func stacksFromMergeRequests(mrs []mergeRequestWire) []RemoteStack {
	bySource := make(map[string]mergeRequestWire, len(mrs))
	targeted := make(map[string]bool, len(mrs))
	for _, mr := range mrs {
		bySource[mr.SourceBranch] = mr
		targeted[mr.TargetBranch] = true
	}
	var stacks []RemoteStack
	seen := map[int]bool{}
	for _, bottom := range mrs {
		if _, targetIsMR := bySource[bottom.TargetBranch]; targetIsMR {
			continue
		}
		chain := []mergeRequestWire{bottom}
		seen[bottom.IID] = true
		for {
			var next *mergeRequestWire
			for i := range mrs {
				if !seen[mrs[i].IID] && mrs[i].TargetBranch == chain[len(chain)-1].SourceBranch {
					next = &mrs[i]
					break
				}
			}
			if next == nil {
				break
			}
			chain = append(chain, *next)
			seen[next.IID] = true
		}
		st := RemoteStack{ID: bottom.IID, Number: bottom.IID, URL: bottom.WebURL, Base: RemoteStackBase{Ref: bottom.TargetBranch}, Open: false}
		for _, mr := range chain {
			var mergedAt *string
			if mr.MergedAt != "" {
				v := mr.MergedAt
				mergedAt = &v
			}
			st.PullRequests = append(st.PullRequests, mr.IID)
			st.PRDetails = append(st.PRDetails, RemoteStackPR{Number: mr.IID, State: mr.State, Draft: mr.Draft || mr.WorkInProgress, MergedAt: mergedAt, Head: RemoteStackPRHead{Ref: mr.SourceBranch, Sha: mr.SHA}})
			if mr.State == "opened" {
				st.Open = true
			}
		}
		stacks = append(stacks, st)
	}
	return stacks
}

func (c *Client) ListStacks() ([]RemoteStack, error) {
	mrs, err := c.listMergeRequests(url.Values{"scope": {"all"}, "state": {"all"}})
	if err != nil {
		return nil, err
	}
	return stacksFromMergeRequests(mrs), nil
}

func (c *Client) FindStackForPR(prNumber int) (*RemoteStack, error) {
	stacks, err := c.ListStacks()
	if err != nil {
		return nil, err
	}
	for i := range stacks {
		for _, n := range stacks[i].PullRequests {
			if n == prNumber {
				return &stacks[i], nil
			}
		}
	}
	return nil, nil
}

func (c *Client) GetStack(stackNumber int) (*RemoteStack, error) {
	stacks, err := c.ListStacks()
	if err != nil {
		return nil, err
	}
	for i := range stacks {
		if stacks[i].Number == stackNumber {
			return &stacks[i], nil
		}
	}
	return nil, &api.HTTPError{StatusCode: http.StatusNotFound, Message: "merge request stack not found"}
}

func (c *Client) CreateStack(prNumbers []int) (*RemoteStack, error) {
	if len(prNumbers) == 0 {
		return nil, fmt.Errorf("a stack requires at least one merge request")
	}
	for i, number := range prNumbers {
		pr, err := c.FindPRByNumber(number)
		if err != nil {
			return nil, err
		}
		if pr == nil {
			return nil, fmt.Errorf("merge request !%d not found", number)
		}
		if i > 0 {
			prev, prevErr := c.FindPRByNumber(prNumbers[i-1])
			if prevErr != nil {
				return nil, prevErr
			}
			if prev == nil {
				return nil, fmt.Errorf("merge request !%d not found", prNumbers[i-1])
			}
			if pr.BaseRefName != prev.HeadRefName {
				if err := c.UpdatePRBase(number, prev.HeadRefName); err != nil {
					return nil, err
				}
			}
		}
	}
	return c.FindStackForPR(prNumbers[0])
}

func (c *Client) AddToStack(stackNumber int, prNumbers []int) (*RemoteStack, error) {
	st, err := c.GetStack(stackNumber)
	if err != nil {
		return nil, err
	}
	all := append(append([]int(nil), st.PullRequests...), prNumbers...)
	return c.CreateStack(all)
}

func (c *Client) Unstack(stackNumber int) (*RemoteStack, bool, error) {
	st, err := c.GetStack(stackNumber)
	if err != nil {
		return nil, false, err
	}
	for i, number := range st.PullRequests {
		if i < len(st.PRDetails) && st.PRDetails[i].IsMerged() {
			continue
		}
		if err := c.UpdatePRBase(number, st.Base.Ref); err != nil {
			return nil, false, err
		}
	}
	return nil, true, nil
}

func isNotFound(err error) bool {
	if httpErr, ok := err.(*api.HTTPError); ok {
		return httpErr.StatusCode == http.StatusNotFound
	}
	return false
}
