package watcherlease

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHeartbeatRenewsIndependentlyCancelsAndJoins(t *testing.T) {
	var calls atomic.Int32
	lost := errors.New("lease lost")
	ctx, stop := Start(t.Context(), 5*time.Millisecond, time.Second, func(ctx context.Context) error {
		require.NoError(t, ctx.Err())
		if calls.Add(1) == 3 {
			return lost
		}
		return nil
	})
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("blocked work was not cancelled")
	}
	require.ErrorIs(t, context.Cause(ctx), lost)
	stop()
	require.EqualValues(t, 3, calls.Load(), "stop joins the owner before returning")
}
