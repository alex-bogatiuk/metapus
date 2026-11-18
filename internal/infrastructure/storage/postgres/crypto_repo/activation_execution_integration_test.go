//go:build integration

package crypto_repo_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"metapus/internal/application/outboundapp"
	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_network_operation"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/registers/crypto_balance"
	"metapus/internal/domain/registers/crypto_platform_cost"
	"metapus/internal/domain/registers/cryptoposting"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_document_repo"
	"metapus/internal/infrastructure/storage/postgres/crypto_register_repo"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/infrastructure/storage/postgres/outbound_repo"

	"github.com/stretchr/testify/require"
)

type activationExecutionFixture struct {
	crypto_repo.ActivationSelectorFixture
	task     outboundapp.AccountActivationTask
	auth     crypto.SigningAuthorization
	unsigned crypto.UnsignedTxEnvelope
	signed   crypto.SignedTxEnvelope
	fee      id.ID
}

func newActivationExecutionFixture(t *testing.T) activationExecutionFixture {
	t.Helper()
	f := activationExecutionFixture{ActivationSelectorFixture: crypto_repo.NewActivationSelectorFixture(t), fee: id.New()}
	f.SeedTasks(t)
	r := outbound_repo.NewOutboundRepo()
	rows, err := r.ClaimAccountActivationTasks(f.Ctx, "same-worker", 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	f.task = rows[0]
	require.False(t, id.IsNil(f.task.ClaimToken))
	require.NoError(t, r.ReserveAccountActivationBudget(f.Ctx, f.task))
	require.NoError(t, r.ApplyAccountActivationClaim(f.Ctx, f.task, outboundapp.ActivationClaimUpdate{Action: outboundapp.ActivationClaimSigning}))
	payload := []byte(`{"fixture":"activation"}`)
	fp, err := crypto.CanonicalUnsignedFingerprint(crypto.ChainFamilyTRON, 1, payload)
	require.NoError(t, err)
	f.unsigned = crypto.UnsignedTxEnvelope{Family: crypto.ChainFamilyTRON, SchemaVersion: 1, Generation: 1, UnsignedFingerprint: fp, Payload: payload}
	f.auth = crypto.SigningAuthorization{SchemaVersion: crypto.SigningAuthorizationSequenceLessSchemaVersion, AuthorizationID: id.New().String(), TenantID: id.New().String(), Purpose: crypto.SigningPurposeActivation, BasisType: "CryptoAccountActivationTask", BasisID: f.task.ActivationID, Attempt: 1, Generation: 1, ExpiresAt: time.Now().Add(time.Hour), NetworkID: f.Network, NetworkCode: f.Network.String(), ChainFamily: crypto.ChainFamilyTRON, NetworkIdentifier: f.Network.String(), SourceAddress: f.task.HotWallet.Address, FeePayerAddress: f.task.HotWallet.Address, VaultKeyID: f.task.HotWallet.VaultKeyID, DestinationAddress: f.task.Address, AssetStandard: "native", AmountMinorUnits: types.ZeroRawAmount(), MaxNetworkFeeMinor: types.MustRawAmount("10"), UnsignedFingerprint: fp, PolicyVersion: crypto.SigningAuthorizationPolicyVersion(crypto.ChainFamilyTRON)}
	_, err = f.Pool.Exec(f.Ctx, `INSERT INTO reg_crypto_fee_reservations(id,network_id,payer_wallet_id,native_token_id,operation_token_id,basis_type,basis_id,purpose,attempt,strategy,max_fee_minor,unsigned_fingerprint,status)
 VALUES($1,$2,$3,$4,$4,'CryptoAccountActivationTask',$5,'activation',1,'native_burn',10,$6,'reserved')`, f.fee, f.Network, f.Hot, f.Native, f.task.ActivationID, fp)
	require.NoError(t, err)
	_, err = r.InsertSigningAuthorization(f.Ctx, f.auth, "frozen-jws", f.unsigned)
	require.NoError(t, err)
	f.signed = crypto.SignedTxEnvelope{Family: crypto.ChainFamilyTRON, SchemaVersion: 1, Generation: 1, UnsignedFingerprint: fp, TxHash: strings.ReplaceAll(f.task.ActivationID.String()+f.task.ActivationID.String(), "-", ""), Payload: []byte(`{"raw":"signed bytes"}`)}
	return f
}
func (f *activationExecutionFixture) handoff(t *testing.T) outboundapp.AccountActivationTask {
	t.Helper()
	_, err := f.Pool.Exec(f.Ctx, `UPDATE reg_crypto_account_activations SET activation_lease_expires_at=clock_timestamp()-interval '1 second',activation_next_attempt_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.task.ActivationID)
	require.NoError(t, err)
	rows, err := outbound_repo.NewOutboundRepo().ClaimAccountActivationTasks(f.Ctx, "same-worker", 2)
	require.NoError(t, err)

	for _, task := range rows {
		if task.ActivationID == f.task.ActivationID {
			require.NotEqual(t, f.task.ClaimToken, task.ClaimToken)
			return task
		}
	}
	t.Fatal("expected a new claim for the original task")
	return outboundapp.AccountActivationTask{}
}

func TestActivationExecutionClaimRejectsStaleCommandsAndDispatch(t *testing.T) {
	f := newActivationExecutionFixture(t)
	r := outbound_repo.NewOutboundRepo()
	next := f.handoff(t)
	before := activationSnapshot(t, f.ActivationSelectorFixture, f.task.ActivationID)
	require.ErrorContains(t, postgres.MustGetTxManager(f.Ctx).RunInTransaction(f.Ctx, func(ctx context.Context) error { return r.PrepareAccountActivationState(ctx, f.task) }), "claim is missing or stale")
	require.ErrorContains(t, r.ReserveAccountActivationBudget(f.Ctx, f.task), "claim is missing or stale")
	for _, action := range []outboundapp.ActivationClaimAction{outboundapp.ActivationClaimSigning, outboundapp.ActivationClaimHeld, outboundapp.ActivationClaimFailed, outboundapp.ActivationClaimDeferred, outboundapp.ActivationClaimExternallyActive} {
		require.ErrorContains(t, r.ApplyAccountActivationClaim(f.Ctx, f.task, outboundapp.ActivationClaimUpdate{Action: action, Reason: "late result", Delay: time.Hour}), "claim is missing or stale")
	}
	for _, phase := range []string{"authorize", "sign", "broadcast"} {
		require.ErrorContains(t, r.BeginAccountActivationDispatch(f.Ctx, f.task, f.auth, phase), "claim is missing or stale")
	}
	require.ErrorContains(t, r.MarkAccountActivationHeld(f.Ctx, f.task.ActivationID, "unclaimed callback"), "owned by an execution claim")
	require.Equal(t, before, activationSnapshot(t, f.ActivationSelectorFixture, f.task.ActivationID))
	require.NoError(t, r.BeginAccountActivationDispatch(f.Ctx, next, f.auth, "authorize"))
	changed := f.auth
	changed.AmountMinorUnits = types.MustRawAmount("1")
	require.ErrorContains(t, r.BeginAccountActivationDispatch(f.Ctx, next, changed, "sign"), "original intent")
}

type executionNumerator struct{}

func (executionNumerator) GetNextNumber(context.Context, numerator.Config, time.Time) (string, error) {
	// Document numbers are unique, so every document gets its own.
	return "ACT-RECOVERY-" + id.Compact(id.New()), nil
}
func (executionNumerator) SetNextNumber(context.Context, numerator.Config, time.Time, int64) error {
	return nil
}

func TestActivationExecutionLateSignedAndBroadcastRecoveryKeepNewLease(t *testing.T) {
	f := newActivationExecutionFixture(t)
	r := outbound_repo.NewOutboundRepo()
	fees := outbound_repo.NewFeeBudgetManager(nil)
	require.NoError(t, r.BeginAccountActivationDispatch(f.Ctx, f.task, f.auth, "sign"))
	next := f.handoff(t)
	require.ErrorContains(t, r.BeginAccountActivationDispatch(f.Ctx, next, f.auth, "sign"), "already dispatched")
	require.ErrorContains(t, fees.ReleaseUnbroadcast(f.Ctx, f.fee, "missing signer response"), "signing exposure")
	require.ErrorContains(t, fees.Release(f.Ctx, f.fee, "missing signer response"), "signing exposure")
	require.ErrorContains(t, r.ApplyAccountActivationClaim(f.Ctx, next, outboundapp.ActivationClaimUpdate{Action: outboundapp.ActivationClaimExternallyActive}), "no signing exposure")
	require.ErrorContains(t, r.ApplyAccountActivationClaim(f.Ctx, next, outboundapp.ActivationClaimUpdate{Action: outboundapp.ActivationClaimFailed, Reason: "unknown signer"}), "signing exposure")
	// Sign returned after ownership changed. Its exact result is still durable.
	require.NoError(t, r.MarkSigningAuthorizationSigned(f.Ctx, f.auth.AuthorizationID, f.signed))
	evidence, found, err := r.GetActivationBroadcastEvidence(f.Ctx, f.task, f.signed.TxHash)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, f.fee, evidence.ReservationID)
	require.Equal(t, f.auth.AuthorizationID, evidence.AuthorizationID)
	require.Equal(t, f.auth.UnsignedFingerprint, evidence.Fingerprint)
	require.ErrorContains(t, r.BeginAccountActivationDispatch(f.Ctx, f.task, f.auth, "broadcast"), "claim is missing or stale")
	require.NoError(t, r.BeginAccountActivationDispatch(f.Ctx, next, f.auth, "broadcast"))
	before := activationSnapshot(t, f.ActivationSelectorFixture, f.task.ActivationID)
	tm := postgres.MustGetTxManager(f.Ctx)
	require.NoError(t, tm.RunInTransaction(f.Ctx, func(ctx context.Context) error {
		if err := r.PrepareActivationBroadcastRecovery(ctx, f.task, outboundapp.ActivationBroadcastEvidence{AuthorizationID: f.auth.AuthorizationID, ReservationID: f.fee, Fingerprint: f.auth.UnsignedFingerprint, TxHash: f.signed.TxHash}); err != nil {
			return err
		}
		if err := fees.MarkBroadcast(ctx, f.fee, f.auth.UnsignedFingerprint, f.signed.TxHash); err != nil {
			return err
		}
		return r.MarkSigningAuthorizationBroadcast(ctx, f.auth.AuthorizationID, f.signed.TxHash)
	}))
	engine := posting.NewEngine(postgres.NewDocLocker())
	engine.AddVisitor(&cryptoposting.CryptoBalanceVisitor{})
	engine.AddVisitor(&cryptoposting.CryptoPlatformCostVisitor{})
	engine.AddRecorder(cryptoposting.NewCryptoBalanceRecorder(crypto_balance.NewService(crypto_register_repo.NewCryptoBalanceRepo())))
	engine.AddRecorder(cryptoposting.NewCryptoPlatformCostRecorder(crypto_platform_cost.NewService(crypto_register_repo.NewCryptoPlatformCostRepo())))
	repo := crypto_document_repo.NewCryptoNetworkOperationRepo()
	service := crypto_network_operation.NewService(repo, engine, executionNumerator{}, nil)
	coordinator := outboundapp.NewNetworkOperationCoordinator(repo, service, fees, outbound_repo.ActivationWorkflowStore{}, tm, time.Now)
	_, err = coordinator.RecoverActivationBroadcast(f.Ctx, f.task, "another-hash")
	require.ErrorContains(t, err, "original signed transaction")
	operation, err := coordinator.RecoverActivationBroadcast(f.Ctx, f.task, f.signed.TxHash)
	require.NoError(t, err)
	var after string
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT (to_jsonb(a)-'network_operation_id'-'broadcast_at'-'updated_at')::text FROM reg_crypto_account_activations a WHERE id=$1`, f.task.ActivationID).Scan(&after))
	// First evidence binding updates its modification time. Every actor field,
	// including token, owner, lease and status, must remain unchanged.
	var original string
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT ($1::jsonb-'network_operation_id'-'broadcast_at'-'updated_at')::text`, before).Scan(&original))
	require.Equal(t, original, after)
	snapshot := activationSnapshot(t, f.ActivationSelectorFixture, f.task.ActivationID)
	repeated, err := coordinator.RecoverActivationBroadcast(f.Ctx, f.task, f.signed.TxHash)
	require.NoError(t, err)
	require.Equal(t, operation, repeated)
	require.Equal(t, snapshot, activationSnapshot(t, f.ActivationSelectorFixture, f.task.ActivationID))
	receipt := crypto.ChainReceipt{NetworkID: f.Network, TxHash: f.signed.TxHash, Status: crypto.ReceiptSucceeded, BlockHash: "final-block", BlockHeight: 10, Canonicality: crypto.CanonicalityCanonical, Finality: crypto.FinalitySolidified, ObservedAt: time.Now(), Fee: crypto.FeeSettlementReport{NativeAsset: crypto.AssetRef{TokenID: f.Native}, TotalFee: types.MustRawAmount("5"), Components: []crypto.FeeComponent{{Name: "energy", Amount: types.MustRawAmount("5")}}}}
	receipt.BlockHash, receipt.Confirmations = strings.Repeat("f", 64), 20
	var payer string
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT w.address_key FROM reg_crypto_fee_reservations r JOIN cat_wallets w ON w.id=r.payer_wallet_id WHERE r.id=$1`, f.fee).Scan(&payer))
	require.NoError(t, crypto_repo.ProveTestNetworkFee(f.Ctx, payer, &receipt))
	finalized, err := coordinator.FinalizeActivation(f.Ctx, f.task.ActivationID, f.fee, receipt)
	require.NoError(t, err)
	require.True(t, finalized.Posted)
	_, err = coordinator.FinalizeActivation(f.Ctx, f.task.ActivationID, f.fee, receipt)
	require.NoError(t, err)
	var money, cost int
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT (SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=$1 AND recorder_type='CryptoNetworkOperation'),(SELECT count(*) FROM reg_crypto_platform_cost_movements WHERE recorder_id=$1 AND recorder_type='CryptoNetworkOperation')`, operation).Scan(&money, &cost))
	require.Equal(t, 1, money)
	require.Equal(t, 1, cost)
	require.ErrorContains(t, r.ApplyAccountActivationClaim(f.Ctx, next, outboundapp.ActivationClaimUpdate{Action: outboundapp.ActivationClaimHeld, Reason: "late callback"}), "claim is missing or stale")
}

func TestActivationExecutionDispatchIsSingleAndImmutable(t *testing.T) {
	f := newActivationExecutionFixture(t)
	r := outbound_repo.NewOutboundRepo()
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		wg.Go(func() { errs <- r.BeginAccountActivationDispatch(f.Ctx, f.task, f.auth, "sign") })
	}
	wg.Wait()
	close(errs)
	success := 0
	for err := range errs {
		if err == nil {
			success++
		} else {
			require.ErrorContains(t, err, "already dispatched")
		}
	}
	require.Equal(t, 1, success)
	_, err := f.Pool.Exec(f.Ctx, `DELETE FROM reg_crypto_activation_dispatches WHERE activation_id=$1`, f.task.ActivationID)
	require.ErrorContains(t, err, "immutable")
	_, err = f.Pool.Exec(f.Ctx, `UPDATE reg_crypto_activation_dispatches SET unsigned_fingerprint='changed' WHERE activation_id=$1`, f.task.ActivationID)
	require.ErrorContains(t, err, "immutable")
	var count int
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT count(*) FROM reg_crypto_activation_dispatches WHERE activation_id=$1`, f.task.ActivationID).Scan(&count))
	require.Equal(t, 1, count)
}

func TestActivationExecutionClaimUsesWallClockAndReadCommitted(t *testing.T) {
	f := newActivationExecutionFixture(t)
	r := outbound_repo.NewOutboundRepo()
	tm := postgres.MustGetTxManager(f.Ctx)
	require.NoError(t, tm.RunInTransaction(f.Ctx, func(ctx context.Context) error {
		var started string
		require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, `SELECT now()::text`).Scan(&started))
		_, err := f.Pool.Exec(f.Ctx, `UPDATE reg_crypto_account_activations SET activation_lease_expires_at=clock_timestamp()-interval '1 microsecond' WHERE id=$1`, f.task.ActivationID)
		require.NoError(t, err)
		var oldClockWouldAccept bool
		require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, `SELECT activation_lease_expires_at>now() AND activation_lease_expires_at<clock_timestamp() FROM reg_crypto_account_activations WHERE id=$1`, f.task.ActivationID).Scan(&oldClockWouldAccept))
		require.True(t, oldClockWouldAccept)
		require.ErrorContains(t, r.PrepareAccountActivationState(ctx, f.task), "claim is missing or stale")
		return nil
	}))
	require.NoError(t, tm.RunInTransaction(f.Ctx, func(ctx context.Context) error {
		_, err := tm.GetQuerier(ctx).Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ`)
		require.NoError(t, err)
		require.ErrorContains(t, r.ApplyAccountActivationClaim(ctx, f.task, outboundapp.ActivationClaimUpdate{Action: outboundapp.ActivationClaimHeld, Reason: "stale snapshot"}), "read committed")
		return nil
	}))
}

func TestActivationExecutionResourcesPrecedeTaskRow(t *testing.T) {
	for _, resource := range []string{"policy", "payer"} {
		t.Run(resource, func(t *testing.T) {
			f := newActivationExecutionFixture(t)
			r := outbound_repo.NewOutboundRepo()
			tm := postgres.MustGetTxManager(f.Ctx)
			f.AssertLockOrder(t, func(ctx context.Context) error {
				query, key := `SELECT network_id FROM sys_crypto_wallet_pool_policies WHERE network_id=$1 FOR UPDATE`, f.Network
				if resource == "payer" {
					query, key = `SELECT id FROM cat_wallets WHERE id=$1 FOR UPDATE`, f.Hot
				}
				_, err := tm.GetQuerier(ctx).Exec(ctx, query, key)
				return err
			}, func(ctx context.Context) error {
				_, err := tm.GetQuerier(ctx).Exec(ctx, `SELECT id FROM reg_crypto_account_activations WHERE id=$1 FOR UPDATE`, f.task.ActivationID)
				return err
			}, func(ctx context.Context) error {
				return r.ApplyAccountActivationClaim(ctx, f.task, outboundapp.ActivationClaimUpdate{Action: outboundapp.ActivationClaimHeld, Reason: "wait edge"})
			})
		})
	}
}

func TestActivationSignedResultLocksWalletBeforeAuthorization(t *testing.T) {
	f := newActivationExecutionFixture(t)
	r := outbound_repo.NewOutboundRepo()
	tm := postgres.MustGetTxManager(f.Ctx)
	f.AssertLockOrder(t, func(ctx context.Context) error {
		_, err := tm.GetQuerier(ctx).Exec(ctx, `SELECT id FROM cat_wallets WHERE id=$1 FOR UPDATE`, f.Hot)
		return err
	}, func(ctx context.Context) error {
		_, err := tm.GetQuerier(ctx).Exec(ctx, `SELECT id FROM reg_crypto_signing_authorizations WHERE authorization_id=$1 FOR UPDATE`, f.auth.AuthorizationID)
		return err
	}, func(ctx context.Context) error {
		return r.MarkSigningAuthorizationSigned(ctx, f.auth.AuthorizationID, f.signed)
	})
}

func TestActivationCombinedReservationIsAtomicAndSerializable(t *testing.T) {
	f := crypto_repo.NewActivationSelectorFixture(t)
	f.SeedTasks(t)
	r := outbound_repo.NewOutboundRepo()
	fees := outbound_repo.NewFeeBudgetManager(nil)
	tasks, err := r.ClaimAccountActivationTasks(f.Ctx, "budget-actor", 1)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	task := tasks[0]
	req := outboundapp.FeeReservationRequest{NetworkID: f.Network, PayerWalletID: task.HotWallet.ID, NativeTokenID: f.Native, OperationTokenID: f.Native, BasisType: "CryptoAccountActivationTask", BasisID: task.ActivationID, Purpose: crypto.SigningPurposeActivation, Attempt: 1, Strategy: crypto.FeeStrategyNativeBurn, MaxFee: types.MustRawAmount("10")}
	before := activationSnapshot(t, f, task.ActivationID)
	// Fee policy starts disabled. A fee failure must roll back the pool slot.
	_, err = fees.ReserveActivation(f.Ctx, task, req)
	require.Error(t, err)
	require.Equal(t, before, activationSnapshot(t, f, task.ActivationID))
	var count int
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT count(*) FROM reg_crypto_fee_reservations WHERE basis_id=$1`, task.ActivationID).Scan(&count))
	require.Zero(t, count)
	_, err = f.Pool.Exec(f.Ctx, `UPDATE sys_crypto_network_policies SET enabled=true,per_transaction_max_fee_minor=10,daily_network_fee_budget_minor=100,fee_funding_mode='source_only',min_fee_wallet_balance_minor=0,min_source_wallet_residual_minor=0 WHERE network_id=$1`, f.Network)
	require.NoError(t, err)
	reservation, err := fees.ReserveActivation(f.Ctx, task, req)
	require.NoError(t, err)
	replay, err := fees.ReserveActivation(f.Ctx, task, req)
	require.NoError(t, err)
	require.Equal(t, reservation.ID, replay.ID)
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT count(*) FROM reg_crypto_fee_reservations WHERE basis_id=$1`, task.ActivationID).Scan(&count))
	require.Equal(t, 1, count)
	tm := postgres.MustGetTxManager(f.Ctx)
	require.NoError(t, tm.RunInTransaction(f.Ctx, func(ctx context.Context) error {
		_, err := fees.ReserveActivation(ctx, task, req)
		require.ErrorContains(t, err, "serializable")
		return nil
	}))
}

func TestActivationReservationLocksNetworkPolicyBeforeWallet(t *testing.T) {
	f := newActivationExecutionFixture(t)
	r := outbound_repo.NewOutboundRepo()
	tm := postgres.MustGetTxManager(f.Ctx)
	f.AssertSerializableLockOrder(t, func(ctx context.Context) error {
		_, err := tm.GetQuerier(ctx).Exec(ctx, `SELECT network_id FROM sys_crypto_network_policies WHERE network_id=$1 FOR UPDATE`, f.Network)
		return err
	}, func(ctx context.Context) error {
		_, err := tm.GetQuerier(ctx).Exec(ctx, `SELECT id FROM cat_wallets WHERE id=$1 FOR UPDATE`, f.Hot)
		return err
	}, func(ctx context.Context) error { return r.ReserveAccountActivationBudget(ctx, f.task) })
}
