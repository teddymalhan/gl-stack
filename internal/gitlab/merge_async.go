package gitlab

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
)

const (
	MergeMethodMerge  = "merge"
	MergeMethodSquash = "squash"
	MergeMethodRebase = "rebase"

	MergeActionDefault     = "default"
	MergeActionDirectMerge = "direct_merge"
	MergeActionMergeQueue  = "merge_queue"
)

var ErrAsyncMergeUnavailable = errors.New("stack merge is not available for this repository")

type RepoMergeConfig struct {
	MergeAllowed  bool
	SquashAllowed bool
	RebaseAllowed bool
	DefaultMethod string
}

func (c RepoMergeConfig) AllowedMethods() []string {
	methods := make([]string, 0, 3)
	if c.MergeAllowed {
		methods = append(methods, MergeMethodMerge)
	}
	if c.SquashAllowed {
		methods = append(methods, MergeMethodSquash)
	}
	if c.RebaseAllowed {
		methods = append(methods, MergeMethodRebase)
	}
	return methods
}

func (c RepoMergeConfig) Allows(method string) bool {
	switch method {
	case MergeMethodMerge:
		return c.MergeAllowed
	case MergeMethodSquash:
		return c.SquashAllowed
	case MergeMethodRebase:
		return c.RebaseAllowed
	default:
		return false
	}
}

type AsyncMergeDetails struct {
	Message         string `json:"message"`
	UUID            string `json:"uuid"`
	MergeMethod     string `json:"merge_method"`
	MergeAction     string `json:"merge_action"`
	ExpectedHeadSHA string `json:"expected_head_sha"`
	SHA             string `json:"sha"`
}

type AsyncMergeResult struct {
	Status  string            `json:"status"`
	Details AsyncMergeDetails `json:"details"`
}

const (
	AsyncMergeStatusPending  = "pending"
	AsyncMergeStatusMerged   = "merged"
	AsyncMergeStatusEnqueued = "enqueued"
	AsyncMergeStatusFailed   = "failed"
)

func (r *AsyncMergeResult) IsMerged() bool   { return r != nil && r.Status == AsyncMergeStatusMerged }
func (r *AsyncMergeResult) IsEnqueued() bool { return r != nil && r.Status == AsyncMergeStatusEnqueued }
func (r *AsyncMergeResult) IsFailed() bool   { return r != nil && r.Status == AsyncMergeStatusFailed }
func (r *AsyncMergeResult) IsPending() bool  { return r != nil && r.Status == AsyncMergeStatusPending }

func (c *Client) RepoMergeConfig() (*RepoMergeConfig, error) {
	// GitLab's merge endpoint supports regular and squash merges. A rebase is a
	// separate operation, so it is intentionally not offered as a merge method.
	return &RepoMergeConfig{MergeAllowed: true, SquashAllowed: true, DefaultMethod: MergeMethodMerge}, nil
}

func (c *Client) BaseBranchUsesMergeQueue(string) (bool, error) { return false, nil }

// MergeStackAsync preserves the command interface but performs GitLab merges
// synchronously. GitLab has no atomic stack-merge API: members are merged from
// bottom to top, retargeting each remaining MR to the trunk first.
func (c *Client) MergeStackAsync(prNumber int, method, _ string) (*AsyncMergeResult, error) {
	stack, err := c.FindStackForPR(prNumber)
	if err != nil {
		return nil, err
	}
	if stack == nil {
		return nil, fmt.Errorf("merge request !%d is not part of a stack", prNumber)
	}
	for _, number := range stack.PullRequests {
		if err := c.UpdatePRBase(number, stack.Base.Ref); err != nil {
			return nil, err
		}
		pr, err := c.FindPRByNumber(number)
		if err != nil {
			return nil, err
		}
		if pr == nil {
			return nil, fmt.Errorf("merge request !%d not found", number)
		}
		if pr.Merged {
			if number == prNumber {
				break
			}
			continue
		}
		payload := map[string]any{"should_remove_source_branch": false}
		if method == MergeMethodSquash {
			payload["squash"] = true
		}
		var response struct {
			SHA     string `json:"sha"`
			Message string `json:"message"`
		}
		if err := c.request(http.MethodPut, fmt.Sprintf("projects/%s/merge_requests/%d/merge", c.project, number), nil, payload, &response); err != nil {
			return nil, fmt.Errorf("merging merge request !%d: %w", number, err)
		}
		if number == prNumber {
			return &AsyncMergeResult{Status: AsyncMergeStatusMerged, Details: AsyncMergeDetails{SHA: response.SHA, MergeMethod: method, UUID: strconv.Itoa(prNumber)}}, nil
		}
	}
	return &AsyncMergeResult{Status: AsyncMergeStatusMerged, Details: AsyncMergeDetails{UUID: strconv.Itoa(prNumber), MergeMethod: method}}, nil
}

func (c *Client) GetAsyncMergeResult(_ int, uuid string) (*AsyncMergeResult, error) {
	return &AsyncMergeResult{Status: AsyncMergeStatusMerged, Details: AsyncMergeDetails{UUID: uuid}}, nil
}

func (c *Client) PRTitles(numbers []int) (map[int]string, error) {
	titles := make(map[int]string, len(numbers))
	for _, number := range numbers {
		pr, err := c.FindPRByNumber(number)
		if err != nil {
			return titles, err
		}
		if pr != nil {
			titles[number] = pr.Title
		}
	}
	return titles, nil
}
