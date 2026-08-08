package gitlab

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPRURL(t *testing.T) {
	assert.Equal(t, "https://gitlab.com/group/project/-/merge_requests/42", PRURL("", "group", "project", 42))
	assert.Equal(t, "https://gitlab.example/group/project/-/merge_requests/7", PRURL("gitlab.example", "group", "project", 7))
}

func TestStacksFromMergeRequests(t *testing.T) {
	mrs := []mergeRequestWire{
		{IID: 10, State: "opened", WebURL: "https://gitlab.test/mr/10", SourceBranch: "feature-a", TargetBranch: "main"},
		{IID: 11, State: "opened", SourceBranch: "feature-b", TargetBranch: "feature-a"},
		{IID: 12, State: "opened", SourceBranch: "feature-c", TargetBranch: "feature-b"},
		{IID: 20, State: "opened", SourceBranch: "other", TargetBranch: "main"},
	}

	stacks := stacksFromMergeRequests(mrs)
	require.Len(t, stacks, 2)
	assert.Equal(t, 10, stacks[0].Number)
	assert.Equal(t, "main", stacks[0].Base.Ref)
	assert.Equal(t, []int{10, 11, 12}, stacks[0].PullRequests)
	assert.Equal(t, []int{20}, stacks[1].PullRequests)
}

func TestFindPRForBranchUsesGitLabAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "secret" {
			http.Error(w, `{"error":"Not Found"}`, http.StatusNotFound)
			return
		}
		assert.Equal(t, "/projects/group%2Fproject/merge_requests", r.URL.EscapedPath())
		assert.Equal(t, "feature", r.URL.Query().Get("source_branch"))
		_ = json.NewEncoder(w).Encode([]mergeRequestWire{{
			IID: 42, State: "opened", WebURL: "https://gitlab.test/group/project/-/merge_requests/42",
			Title: "Feature", SourceBranch: "feature", TargetBranch: "main",
		}})
	}))
	defer server.Close()

	client := &Client{http: server.Client(), baseURL: server.URL, project: "group%2Fproject", token: "secret"}
	mr, err := client.FindPRForBranch("feature")
	require.NoError(t, err)
	require.NotNil(t, mr)
	assert.Equal(t, 42, mr.Number)
	assert.Equal(t, "feature", mr.HeadRefName)
	assert.Equal(t, "main", mr.BaseRefName)
}
