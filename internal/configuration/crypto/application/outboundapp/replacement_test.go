package outboundapp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/json"
	"metapus/pkg/sdk/types"
)

type replacementStoreStub struct {
	ReplacementRepository
	t                 *testing.T
	current           SigningAuthorizationRecord
	generations       []SigningGeneration
	boundHash, winner string
	superseded        []string
	held, broadcasts  int
}

func (s *replacementStoreStub) GetSigningAttempt(context.Context, string, id.ID, crypto.SigningPurpose, int, int) (SigningAuthorizationRecord, bool, error) {
	return s.current, true, nil
}
func (s *replacementStoreStub) ListSigningGenerations(context.Context, ReplacementCandidate) ([]SigningGeneration, error) {
	return s.generations, nil
}
func (*replacementStoreStub) LoadSigningAsset(context.Context, crypto.SigningAuthorization) (crypto.AssetRef, error) {
	return crypto.AssetRef{TokenID: id.New()}, nil
}
func (s *replacementStoreStub) UpdateOutboundReplacementHash(ctx context.Context, _ string, _ id.ID, oldHash, newHash string) error {
	require.Equal(s.t, s.boundHash, oldHash)
	stageWorkflowEffect(s.t, ctx, func() { s.boundHash = newHash })
	return nil
}
func (s *replacementStoreStub) MarkSigningAuthorizationWinner(ctx context.Context, authorization, _ string) error {
	stageWorkflowEffect(s.t, ctx, func() { s.winner = authorization })
	return nil
}
func (s *replacementStoreStub) MarkSigningAuthorizationSuperseded(ctx context.Context, authorization, _ string) error {
	stageWorkflowEffect(s.t, ctx, func() { s.superseded = append(s.superseded, authorization) })
	return nil
}
func (s *replacementStoreStub) MarkSigningAuthorizationHeld(context.Context, string, string) error {
	s.held++
	return nil
}
func (s *replacementStoreStub) MarkSigningAuthorizationBroadcast(context.Context, string, string) error {
	s.broadcasts++
	return nil
}

type replacementFeeStub struct {
	ReplacementFees
	t                 *testing.T
	reservation       crypto.FeeReservation
	rebindErr         error
	fingerprint, hash string
	held              int
}

func (s *replacementFeeStub) Get(context.Context, string, id.ID, crypto.SigningPurpose, int) (crypto.FeeReservation, bool, error) {
	return s.reservation, true, nil
}
func (*replacementFeeStub) LoadPolicy(context.Context, id.ID) (crypto.NetworkPolicy, error) {
	return crypto.NetworkPolicy{PerTransactionMaxFee: types.NewRawAmountFromInt64(10)}, nil
}
func (s *replacementFeeStub) MarkReplacementBroadcast(ctx context.Context, _ id.ID, _, _, fingerprint, hash string, _ int) error {
	if s.rebindErr != nil {
		return s.rebindErr
	}
	stageWorkflowEffect(s.t, ctx, func() { s.fingerprint = fingerprint; s.hash = hash })
	return nil
}
func (s *replacementFeeStub) Hold(context.Context, id.ID, string) error { s.held++; return nil }

type replacementAuthorizationsStub struct {
	Authorizations
	custodyChecks int
}

func (s *replacementAuthorizationsStub) CheckCustodyDispatch(context.Context, crypto.SigningAuthorization, *crypto.FamilySignerRegistry) error {
	s.custodyChecks++
	return nil
}

func TestReplacementPreservesSavedEffectsAndBindsOnlyKnownWinner(t *testing.T) {
	rebindFailure := errors.New("fee rebind failed")
	tests := []struct {
		name                    string
		disposition             crypto.ReplacementDisposition
		winner                  string
		rebindErr               error
		wantErr                 string
		wantWinner, wantHash    string
		wantBroadcast, wantHold int
	}{
		{name: "older generation mined", disposition: crypto.ReplacementMined, winner: "hash-1", wantWinner: "auth-1", wantHash: "hash-1"},
		{name: "winner rebind rollback", disposition: crypto.ReplacementMined, winner: "hash-1", rebindErr: rebindFailure, wantErr: "fee rebind failed", wantHash: "hash-2"},
		{name: "unknown mined hash cannot change accounting", disposition: crypto.ReplacementMined, winner: "unknown", wantErr: "unknown mined replacement hash", wantHash: "hash-2"},
		{name: "uncertain rpc resubmits saved bytes", disposition: crypto.ReplacementUncertain, wantBroadcast: 1, wantHash: "hash-2"},
		{name: "consumed nonce prevents further sends", disposition: crypto.ReplacementNonceConsumed, wantHold: 1, wantErr: "sequence divergence", wantHash: "hash-2"},
		{name: "builder cannot change nonce", disposition: crypto.ReplacementStuck, wantHold: 1, wantErr: "sequence divergence", wantHash: "hash-2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			driver, drivers := newWorkflowDriver(t)
			driver.assessment = crypto.ReplacementAssessment{Disposition: tt.disposition, MinedTxHash: tt.winner}
			driver.broadcast = crypto.BroadcastOutcome{Status: crypto.BroadcastAmbiguous, TxHash: "hash-2"}
			driver.broadcastErr = errors.New("provider timeout after accepting bytes")
			driver.replacement = crypto.UnsignedTxEnvelope{Generation: 3, NetworkSequence: "another-nonce"}
			saved := crypto.SignedTxEnvelope{Family: crypto.ChainFamilyEVM, Generation: 2, NetworkSequence: "7", TxHash: "hash-2", UnsignedFingerprint: "fp-2", Payload: json.RawMessage(`{"rawTxHex":"exact-saved-bytes"}`)}
			current := SigningAuthorizationRecord{Authorization: crypto.SigningAuthorization{AuthorizationID: "auth-2", NetworkID: driver.network.ID, Generation: 2, NetworkSequence: "7", UnsignedFingerprint: "fp-2", Purpose: crypto.SigningPurposeWithdrawal, ChainFamily: crypto.ChainFamilyEVM}, Status: "broadcast", TxID: saved.TxHash, SignedTx: &saved}
			store := &replacementStoreStub{t: t, current: current, boundHash: "hash-2", generations: []SigningGeneration{
				{AuthorizationID: "auth-1", Generation: 1, TxHash: "hash-1", UnsignedFingerprint: "fp-1"},
				{AuthorizationID: "auth-2", Generation: 2, TxHash: "hash-2", UnsignedFingerprint: "fp-2"},
			}}
			fees := &replacementFeeStub{t: t, reservation: crypto.FeeReservation{ID: id.New()}, rebindErr: tt.rebindErr}
			authorizations := &replacementAuthorizationsStub{}
			txm := &workflowTx{}
			service := NewReplacementService(ReplacementConfig{Drivers: drivers, Signers: crypto.NewFamilySignerRegistry(), Repository: store, Authorizations: authorizations, Fees: fees, Transactions: txm})
			err := service.Execute(t.Context(), ReplacementCandidate{AuthorizationID: "auth-2", BasisType: "WithdrawalRequest", BasisID: id.New(), Purpose: crypto.SigningPurposeWithdrawal, Attempt: 1, Generation: 2})
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, []string{"hash-1", "hash-2"}, driver.observedHashes, "every persisted generation must be assessed")
			require.Equal(t, tt.wantHash, store.boundHash)
			require.Equal(t, tt.wantWinner, store.winner)
			require.Len(t, driver.submitted, tt.wantBroadcast)
			require.Equal(t, tt.wantHold, driver.divergenceHolds)
			require.Equal(t, tt.wantHold, fees.held)
			if tt.wantWinner != "" {
				require.Equal(t, "hash-1", fees.hash)
				require.Equal(t, "fp-1", fees.fingerprint)
				require.Equal(t, []string{"auth-2"}, store.superseded)
				require.Equal(t, 1, txm.commits)
			} else {
				require.Zero(t, txm.commits)
				require.Empty(t, store.superseded)
			}
			if tt.wantBroadcast > 0 {
				require.Equal(t, saved, driver.submitted[0], "uncertainty permits only the exact signed envelope")
				require.Equal(t, 1, store.broadcasts)
			}
			if tt.rebindErr != nil {
				require.ErrorIs(t, err, tt.rebindErr)
				require.Empty(t, fees.hash)
			}
		})
	}
}
