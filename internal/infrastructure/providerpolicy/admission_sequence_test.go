package providerpolicy

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSequenceEligibilityRespectsWholeProofDeadlineWithoutConsumingQuota(t *testing.T) {
	tests := []struct {
		name         string
		giveInterval int
		giveRequests int
		giveBudget   time.Duration
		want         bool
	}{
		{name: "reserve cannot complete native proof", giveInterval: 30000, giveRequests: 6, giveBudget: 25 * time.Second},
		{name: "normal provider fits", giveInterval: 200, giveRequests: 6, giveBudget: 25 * time.Second, want: true},
		{name: "single header may use reserve", giveInterval: 30000, giveRequests: 1, giveBudget: time.Second, want: true},
		{name: "parent has less time", giveInterval: 200, giveRequests: 6, giveBudget: 500 * time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			admission, err := Acquire(t.Name(), Capacity{Concurrency: 8, BulkConcurrency: 4, RequestsPerSecond: 10, MinRequestIntervalMS: tt.giveInterval})
			require.NoError(t, err)
			defer Release(admission)
			ctx, cancel := context.WithTimeout(t.Context(), tt.giveBudget)
			defer cancel()
			tokens := admission.limiter.Tokens()
			require.Equal(t, tt.want, admission.CanCompleteSequence(ctx, tt.giveRequests))
			require.Equal(t, tokens, admission.limiter.Tokens(), "eligibility must not reserve or spend quota")
			cancel()
			require.False(t, admission.CanCompleteSequence(ctx, tt.giveRequests))
		})
	}
}
