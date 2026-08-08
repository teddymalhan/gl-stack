package gitlab

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepoMergeConfig(t *testing.T) {
	client := &Client{}
	config, err := client.RepoMergeConfig()
	require.NoError(t, err)
	assert.Equal(t, []string{MergeMethodMerge, MergeMethodSquash}, config.AllowedMethods())
	assert.True(t, config.Allows(MergeMethodMerge))
	assert.True(t, config.Allows(MergeMethodSquash))
	assert.False(t, config.Allows(MergeMethodRebase))
}

func TestAsyncMergeResultStates(t *testing.T) {
	assert.True(t, (&AsyncMergeResult{Status: AsyncMergeStatusMerged}).IsMerged())
	assert.True(t, (&AsyncMergeResult{Status: AsyncMergeStatusPending}).IsPending())
	assert.True(t, (&AsyncMergeResult{Status: AsyncMergeStatusFailed}).IsFailed())
	assert.True(t, (&AsyncMergeResult{Status: AsyncMergeStatusEnqueued}).IsEnqueued())
}
