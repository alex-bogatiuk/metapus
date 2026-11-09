package tx

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCommitHooksFollowOutermostOutcome(t *testing.T) {
	for _, rollback := range []bool{true, false} {
		outer, parent := WithCommitScope(context.Background())
		var called []string
		require.NoError(t, OnCommit(outer, func(context.Context) { called = append(called, "outer") }))
		released, child := WithCommitScope(outer)
		require.NoError(t, OnCommit(released, func(context.Context) { called = append(called, "released") }))
		require.NoError(t, child.Merge(outer))
		aborted, discarded := WithCommitScope(outer)
		require.NoError(t, OnCommit(aborted, func(context.Context) { called = append(called, "aborted") }))
		discarded.Discard()
		require.Empty(t, called)
		if rollback {
			parent.Discard()
			require.Empty(t, called)
		} else {
			parent.Commit(context.Background())
			require.Equal(t, []string{"outer", "released"}, called)
		}
		require.Error(t, OnCommit(outer, func(context.Context) {}))
	}
	require.Error(t, OnCommit(context.Background(), func(context.Context) {}))
}
