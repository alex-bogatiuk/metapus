//go:build integration

package crypto_repo_test

import (
	"context"
	"strings"
	"testing"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/outbound_repo"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/json"
	"metapus/pkg/sdk/postgres"

	"github.com/stretchr/testify/require"
)

func allocationSnapshot(t *testing.T, f rentalReplayFixture, key id.ID) string {
	t.Helper()
	var snapshot string
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT to_jsonb(a)::text FROM reg_crypto_resource_allocations a WHERE id=$1`, key).Scan(&snapshot))
	return snapshot
}

func reserveRentalAllocation(t *testing.T, f rentalReplayFixture) (outbound_repo.ResourceRequest, *outbound_repo.ResourceAllocationRecord) {
	t.Helper()
	require.NoError(t, f.activate("1000"))
	req := outbound_repo.ResourceRequest{BasisType: "CryptoAMLWithdrawalLine", BasisID: f.Auth.BasisID, Purpose: "withdrawal", NetworkID: f.Auth.NetworkID, OperationTokenID: f.asset, SourceWalletID: f.Source}
	a, _, err := outbound_repo.NewOutboundRepo().TryReserveResource(f.Ctx, req, 100, 0)
	require.NoError(t, err)
	require.NotNil(t, a)
	return req, a
}

func TestResourceReservationReplayChecksStoredPayloadAndParentLock(t *testing.T) {
	f := newRentalReplayFixture(t)
	req, a := reserveRentalAllocation(t, f)
	repo := outbound_repo.NewOutboundRepo()
	before := allocationSnapshot(t, f, a.ID)
	replay, _, err := repo.TryReserveResource(f.Ctx, req, 100, 0)
	require.NoError(t, err)
	require.Equal(t, a.ID, replay.ID)
	for _, field := range []string{"network", "wallet", "token", "purpose", "quantity"} {
		changed := req
		quantity := int64(100)
		switch field {
		case "network":
			changed.NetworkID = id.New()
		case "wallet":
			changed.SourceWalletID = id.New()
		case "token":
			changed.OperationTokenID = f.native
		case "purpose":
			changed.Purpose = "sweep"
		case "quantity":
			quantity++
		}
		_, _, err := repo.TryReserveResource(f.Ctx, changed, quantity, 0)
		require.ErrorContains(t, err, "original owner, asset or quantity", field)
	}
	require.Equal(t, before, allocationSnapshot(t, f, a.ID))
	tm := postgres.MustGetTxManager(f.Ctx)
	f.AssertLockOrder(t, func(ctx context.Context) error {
		_, err := tm.GetQuerier(ctx).Exec(ctx, `SELECT id FROM doc_crypto_resource_rentals WHERE id=$1 FOR UPDATE`, f.rental)
		return err
	},
		func(ctx context.Context) error {
			_, err := tm.GetQuerier(ctx).Exec(ctx, `SELECT id FROM reg_crypto_resource_allocations WHERE id=$1 FOR UPDATE`, a.ID)
			return err
		},
		func(ctx context.Context) error { _, _, err := repo.TryReserveResource(ctx, req, 100, 0); return err })
}

func signedRentalAllocation(t *testing.T) (rentalReplayFixture, *outbound_repo.ResourceAllocationRecord, outbound_repo.ResourceBroadcastCommand) {
	t.Helper()
	f := newRentalReplayFixture(t)
	req, a := reserveRentalAllocation(t, f)
	repo := outbound_repo.NewOutboundRepo()
	_, err := repo.InsertSigningAuthorization(f.Ctx, f.Auth, "synthetic-jws", f.Unsigned, f.Bundle)
	require.NoError(t, err)
	hash := strings.Repeat("c", 64)
	// Synthetic signed storage evidence; signer/permit boundaries have their
	// own contract suites. The real repository publishes the exact effect.
	require.NoError(t, repo.MarkSigningAuthorizationSigned(f.Ctx, f.Auth.AuthorizationID, crypto.SignedTxEnvelope{Family: crypto.ChainFamilyTRON, SchemaVersion: 1, Generation: 1, UnsignedFingerprint: f.Unsigned.UnsignedFingerprint, TxHash: hash, Payload: json.RawMessage(`{"synthetic":true}`)}))
	return f, a, outbound_repo.ResourceBroadcastCommand{AllocationID: a.ID, BasisType: req.BasisType, BasisID: req.BasisID, TxHash: hash}
}

func TestResourceBroadcastReplayPreservesStateAndValidatesEvent(t *testing.T) {
	f, a, command := signedRentalAllocation(t)
	repo := outbound_repo.NewOutboundRepo()
	require.NoError(t, repo.MarkResourceAllocationBroadcast(f.Ctx, command))
	before := allocationSnapshot(t, f, a.ID)
	require.NoError(t, repo.MarkResourceAllocationBroadcast(f.Ctx, command))
	require.Equal(t, before, allocationSnapshot(t, f, a.ID))
	var count int
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT count(*) FROM reg_crypto_resource_allocation_events WHERE allocation_id=$1 AND event_type IN ('broadcast','broadcast_recovered')`, a.ID).Scan(&count))
	require.Equal(t, 1, count)
	changed := command
	changed.BasisID = id.New()
	require.ErrorContains(t, repo.MarkResourceAllocationBroadcast(f.Ctx, changed), "allocation basis")
	receipt := crypto.ChainReceipt{NetworkID: f.Auth.NetworkID, TxHash: command.TxHash, Status: crypto.ReceiptSucceeded, Canonicality: crypto.CanonicalityCanonical, Finality: crypto.FinalitySolidified}
	require.NoError(t, repo.SettleResourceAllocation(f.Ctx, command.TxHash, receipt))
	confirmed := allocationSnapshot(t, f, a.ID)
	require.NoError(t, repo.MarkResourceAllocationBroadcast(f.Ctx, command))
	require.Equal(t, confirmed, allocationSnapshot(t, f, a.ID))
}

func TestResourceRecoveredBroadcastPreservesHistoricalNoOpEvent(t *testing.T) {
	f, a, command := signedRentalAllocation(t)
	repo := outbound_repo.NewOutboundRepo()
	require.NoError(t, repo.ReleaseResourceAllocation(f.Ctx, a.ID, "reclaim before broadcast acknowledgement"))
	require.NoError(t, repo.MarkResourceAllocationBroadcast(f.Ctx, command))
	before := allocationSnapshot(t, f, a.ID)
	// The old code could add a broadcast->broadcast no-op after a distinct
	// broadcast_recovered event. Preserve that valid historical pair.
	_, err := f.Pool.Exec(f.Ctx, `INSERT INTO reg_crypto_resource_allocation_events(allocation_id,event_type,old_status,new_status,details) VALUES($1,'broadcast','broadcast','broadcast',jsonb_build_object('txHash',$2::text,'exactAuthorization',true))`, a.ID, command.TxHash)
	require.NoError(t, err)
	require.NoError(t, repo.MarkResourceAllocationBroadcast(f.Ctx, command))
	require.Equal(t, before, allocationSnapshot(t, f, a.ID))
}

func TestResourceBroadcastEventCollisionRollsBackPublication(t *testing.T) {
	f, a, command := signedRentalAllocation(t)
	before := allocationSnapshot(t, f, a.ID)
	_, err := f.Pool.Exec(f.Ctx, `INSERT INTO reg_crypto_resource_allocation_events(allocation_id,event_type,old_status,new_status,details) VALUES($1,'broadcast','reserved','broadcast','{"txHash":"another","exactAuthorization":true}')`, a.ID)
	require.NoError(t, err)
	require.ErrorContains(t, outbound_repo.NewOutboundRepo().MarkResourceAllocationBroadcast(f.Ctx, command), "original signed transaction")
	require.Equal(t, before, allocationSnapshot(t, f, a.ID))
}

func TestResourceBroadcastRejectsMissingFieldsAlongsideValidHistory(t *testing.T) {
	f, a, command := signedRentalAllocation(t)
	repo := outbound_repo.NewOutboundRepo()
	require.NoError(t, repo.ReleaseResourceAllocation(f.Ctx, a.ID, "released before broadcast acknowledgement"))
	require.NoError(t, repo.MarkResourceAllocationBroadcast(f.Ctx, command))
	before := allocationSnapshot(t, f, a.ID)
	_, err := f.Pool.Exec(f.Ctx, `INSERT INTO reg_crypto_resource_allocation_events(allocation_id,event_type,old_status,new_status,details) VALUES($1,'broadcast','broadcast','broadcast','{}')`, a.ID)
	require.NoError(t, err)
	require.ErrorContains(t, repo.MarkResourceAllocationBroadcast(f.Ctx, command), "original signed transaction")
	require.Equal(t, before, allocationSnapshot(t, f, a.ID))
}
