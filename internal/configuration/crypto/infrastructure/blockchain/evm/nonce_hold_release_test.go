package evm

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/id"
)

type recoveringNonces struct {
	NonceManager
	report NonceStartupReport
	events *[]string
}

func (s recoveringNonces) ReconcileStartup(context.Context, id.ID, NonceStartupObserver) ([]NonceDivergence, error) {
	return s.report.Divergences, nil
}
func (s recoveringNonces) ReconcileStartupReport(context.Context, id.ID, NonceStartupObserver) (NonceStartupReport, error) {
	return s.report, nil
}
func (s recoveringNonces) ReleaseVerifiedHold(_ context.Context, _ id.ID, hold VerifiedNonceHold) error {
	*s.events = append(*s.events, "cursor:"+hold.SourceAddress+"@"+hold.NextNonce)
	return nil
}

type releasingHolds struct {
	events *[]string
	err    error
}

func (s releasingHolds) HoldNetwork(context.Context, id.ID, crypto.OperationalHoldReason, map[string]any) error {
	return nil
}
func (s releasingHolds) ReleaseNetworkHolds(_ context.Context, _ id.ID, reason crypto.OperationalHoldReason, subject map[string]string, _ string) error {
	if s.err != nil {
		return s.err
	}
	*s.events = append(*s.events, "network:"+string(reason)+":"+subject["sourceAddress"]+":"+subject["reason"])
	return nil
}

func TestStartupReleasesNetworkHoldBeforeVerifiedCursor(t *testing.T) {
	var events []string
	verified := VerifiedNonceHold{SourceAddress: "0x3d111ae105EbBA77c10F6542Fe3736C405067D47", NextNonce: "3"}
	driver := &Driver{network: crypto.NetworkRef{ID: id.New()},
		nonces:       recoveringNonces{report: NonceStartupReport{Verified: []VerifiedNonceHold{verified}}, events: &events},
		holdRecorder: releasingHolds{events: &events}}
	require.NoError(t, driver.ReconcileStartup(t.Context()))
	require.Equal(t, []string{
		"network:nonce_divergence:" + verified.SourceAddress + ":" + NonceProviderDisagreement,
		"cursor:" + verified.SourceAddress + "@3",
	}, events)
}

func TestStartupKeepsCursorHeldWhenNetworkReleaseFails(t *testing.T) {
	var events []string
	driver := &Driver{network: crypto.NetworkRef{ID: id.New()},
		nonces:       recoveringNonces{report: NonceStartupReport{Verified: []VerifiedNonceHold{{SourceAddress: "0xabc", NextNonce: "1"}}}, events: &events},
		holdRecorder: releasingHolds{events: &events, err: errors.New("database unavailable")}}
	require.ErrorContains(t, driver.ReconcileStartup(t.Context()), "database unavailable")
	require.Empty(t, events, "the cursor is released only after its network hold")
}
