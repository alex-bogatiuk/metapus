package outboundapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type confirmationCycleFixture struct {
	t                                                           *testing.T
	hash                                                        string
	replacementErr, fundingErr                                  error
	replacementAssessed, claimReleased, settled, fundingChecked bool
	expectedHash                                                string
}

func (s *confirmationCycleFixture) RunBatch(context.Context, string, int) error {
	s.fundingChecked = true
	return s.fundingErr
}
func (s *confirmationCycleFixture) ClaimStuckReplacementAuthorizations(context.Context, string, int, time.Duration, time.Duration) ([]ReplacementCandidate, error) {
	return []ReplacementCandidate{{AuthorizationID: "new-generation"}}, nil
}
func (s *confirmationCycleFixture) ReleaseReplacementClaim(context.Context, string, string) error {
	s.claimReleased = true
	return nil
}
func (s *confirmationCycleFixture) Execute(context.Context, ReplacementCandidate) error {
	require.True(s.t, s.fundingChecked, "in-flight funding must still be examined")
	s.replacementAssessed = true
	if s.replacementErr != nil {
		return s.replacementErr
	}
	// A previous generation was mined while the latest was still pending.
	s.hash = "mined-old-generation"
	return nil
}
func (s *confirmationCycleFixture) ClaimOutboundSettlements(context.Context, string, int) ([]SettlementTask, error) {
	require.True(s.t, s.fundingChecked)
	require.Equal(s.t, s.expectedHash, s.hash, "claim must read the winning hash after replacement assessment")
	return []SettlementTask{{TxHash: s.hash}}, nil
}
func (s *confirmationCycleFixture) Settle(_ context.Context, task SettlementTask) error {
	require.Equal(s.t, s.expectedHash, task.TxHash)
	s.settled = true
	return nil
}
func (*confirmationCycleFixture) RecoverOrphanedResources(context.Context, int) error { return nil }

func TestConfirmationCycleAssessesReplacementBeforeSettlementClaim(t *testing.T) {
	temporary := errors.New("provider temporarily unavailable")
	tests := []struct {
		name                       string
		replacementErr, fundingErr error
		signingDisabled            bool
		wantHash                   string
	}{
		{name: "older generation wins", wantHash: "mined-old-generation"},
		{name: "replacement failure releases claim and keeps accounting running", replacementErr: temporary, wantHash: "pending-new-generation"},
		{name: "signing disabled still tracks accepted transactions", signingDisabled: true, wantHash: "pending-new-generation"},
		{name: "funding failure does not stop other finality", fundingErr: temporary, wantHash: "mined-old-generation"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := &confirmationCycleFixture{t: t, hash: "pending-new-generation", replacementErr: tt.replacementErr, fundingErr: tt.fundingErr, expectedHash: tt.wantHash}
			var replacement ReplacementExecutor = fixture
			if tt.signingDisabled {
				replacement = nil
			}
			cycle := NewConfirmationCycle(ConfirmationCycleConfig{Claims: fixture, Funding: fixture, Replacements: replacement, Settlement: fixture})
			require.NoError(t, cycle.RunBatch(t.Context(), "worker", 20))
			require.True(t, fixture.settled)
			require.Equal(t, !tt.signingDisabled, fixture.replacementAssessed)
			require.Equal(t, !tt.signingDisabled, fixture.claimReleased)
		})
	}
}
