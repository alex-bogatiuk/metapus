//go:build integration

package crypto_worker

import (
	"context"
	"strings"
	"testing"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/registers/crypto_balance"
	"metapus/internal/domain/registers/crypto_receipt_suspense"
	"metapus/internal/domain/registers/cryptoposting"
	infraNumerator "metapus/internal/infrastructure/numerator"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_register_repo"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"

	"github.com/stretchr/testify/require"
)

// receiptProofDriver proves exactly the transfer it is asked about as final.
type receiptProofDriver struct {
	fundingReceiptDriver
	calls *int
}

func (d receiptProofDriver) VerifyTransfer(ctx context.Context, event crypto.BlockchainEvent) (crypto.TransferVerification, error) {
	*d.calls++
	event.Finality, event.Canonicality, event.Confirmations = crypto.FinalitySolidified, crypto.CanonicalityCanonical, 20
	event.DecoderVersion = crypto.TRONTransferEvidenceSchema
	return d.fundingReceiptDriver.VerifyTransfer(ctx, event)
}

func seedUnmatchedTransfer(t *testing.T, ctx context.Context, txm *postgres.TxManager, f sweepCoordinationFixture, reason string, age time.Duration) id.ID {
	t.Helper()
	rowID := id.New()
	hash := strings.ReplaceAll(rowID.String()+rowID.String(), "-", "")
	var contract string
	require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, "SELECT contract_address FROM cat_tokens WHERE id=$1", f.token).Scan(&contract))
	_, err := txm.GetQuerier(ctx).Exec(ctx, `
		INSERT INTO sys_crypto_block_ingestion(network_id,start_height,start_hash,start_timestamp,mode,decoder_version)
		VALUES($1,0,'fixture',0,'shadow',2) ON CONFLICT DO NOTHING`, f.network)
	require.NoError(t, err)
	_, err = txm.GetQuerier(ctx).Exec(ctx, `
		INSERT INTO reg_crypto_inbound_chain_events(id,network_id,tx_hash,event_kind,event_index,from_address,to_address,
			token_contract,token_id,wallet_id,amount,block_number,block_hash,block_timestamp,confirmations,required_confs,
			finality_status,canonicality,event_type,direction,match_status,match_reason,created_at)
		VALUES($1,$2,$3,'contract_log',0,$4,$5,$6,$7,$8,25,100,repeat('b',64),NOW(),20,1,'solidified','canonical',
			'transfer','incoming','unmatched',$9,NOW()-$10::interval)`,
		rowID, f.network, hash, testTRONAddress(), f.walletAddress, contract, f.token, f.wallet, reason,
		(age + time.Second).String())
	require.NoError(t, err)
	return rowID
}

func receiptError(t *testing.T, ctx context.Context, txm *postgres.TxManager, rowID id.ID) string {
	var reason string
	require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, "SELECT receipt_error FROM reg_crypto_inbound_chain_events WHERE id=$1", rowID).Scan(&reason))
	return reason
}

func receiptIntakeForTest(t *testing.T, f sweepCoordinationFixture) (*receiptIntake, *crypto.ChainDriverRegistry, *int) {
	t.Helper()
	calls := 0
	registry := crypto.NewChainDriverRegistry()
	require.NoError(t, registry.Register(receiptProofDriver{
		fundingReceiptDriver: fundingReceiptDriver{network: crypto.NetworkRef{ID: f.network, Code: "TEST", Family: crypto.ChainFamilyTRON,
			NetworkIdentifier: "nile", NativeTokenID: f.native, NativeDecimals: 6, RequiredConfirmations: 1, FinalityMode: crypto.FinalityByConfirmations},
			receipt: crypto.ChainReceipt{ObservedAt: time.Now().UTC()}},
		calls: &calls,
	}))
	engine := posting.NewEngine(nil)
	engine.AddVisitor(&cryptoposting.CryptoBalanceVisitor{})
	engine.AddVisitor(&cryptoposting.CryptoReceiptSuspenseVisitor{})
	engine.AddRecorder(cryptoposting.NewCryptoBalanceRecorder(crypto_balance.NewService(crypto_register_repo.NewCryptoBalanceRepo())))
	engine.AddRecorder(cryptoposting.NewCryptoReceiptSuspenseRecorder(crypto_receipt_suspense.NewService(crypto_register_repo.NewCryptoReceiptSuspenseRepo())))
	return newReceiptIntake(engine, infraNumerator.New()), registry, &calls
}

func TestReceiptIntakeAccountsFinalUnmatchedTransferOnce(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, txm *postgres.TxManager, f sweepCoordinationFixture) {
		rowID := seedUnmatchedTransfer(t, ctx, txm, f, crypto.MatchReasonWrongToken, 0)
		intake, registry, calls := receiptIntakeForTest(t, f)
		recorded, err := intake.poll(ctx, registry)
		require.NoError(t, err)
		require.Equal(t, 1, recorded, receiptError(t, ctx, txm, rowID))
		require.Equal(t, 1, *calls)

		var receiptID id.ID
		var amount, suspense, reason string
		require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, `SELECT i.receipt_id,d.amount::text,s.amount::text,d.reason_code
			FROM reg_crypto_inbound_chain_events i JOIN doc_crypto_custody_receipts d ON d.id=i.receipt_id
			JOIN reg_crypto_receipt_suspense_balances s ON s.receipt_id=d.id WHERE i.id=$1`, rowID).
			Scan(&receiptID, &amount, &suspense, &reason))
		require.Equal(t, "25", amount)
		require.Equal(t, "25", suspense)
		require.Equal(t, crypto.MatchReasonWrongToken, reason)
		var custody types.RawAmount
		require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, `SELECT amount FROM reg_crypto_balance_movements
			WHERE recorder_id=$1 AND recorder_type='CryptoCustodyReceipt' AND wallet_id=$2 AND token_id=$3 AND record_type='receipt'`,
			receiptID, f.wallet, f.token).Scan(&custody))
		require.Equal(t, "25", custody.String())

		// A second pass and a replayed chain event never account the transfer again.
		recorded, err = intake.poll(ctx, registry)
		require.NoError(t, err)
		require.Zero(t, recorded)
		var receipts int
		require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, `SELECT count(*) FROM doc_crypto_custody_receipts WHERE wallet_id=$1`, f.wallet).Scan(&receipts))
		require.Equal(t, 1, receipts)
		_, err = crypto_repo.NewChainEventInboxRepo().ListUnmatchedForReplay(ctx, crypto.MatchReasonWrongToken, 10)
		require.NoError(t, err)
	})
}

func TestReceiptIntakeWaitsForLateInvoiceMatchingWindow(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, txm *postgres.TxManager, f sweepCoordinationFixture) {
		fresh := seedUnmatchedTransfer(t, ctx, txm, f, crypto.MatchReasonNoAllocation, 0)
		intake, registry, calls := receiptIntakeForTest(t, f)
		recorded, err := intake.poll(ctx, registry)
		require.NoError(t, err)
		require.Zero(t, recorded, "an unallocated transfer may still match a late invoice")
		require.Zero(t, *calls)

		old := seedUnmatchedTransfer(t, ctx, txm, f, crypto.MatchReasonNoAllocation, _receiptUnallocatedGrace)
		recorded, err = intake.poll(ctx, registry)
		require.NoError(t, err)
		require.Equal(t, 1, recorded)
		// Once accounted, the transfer leaves automatic invoice matching.
		replay, err := crypto_repo.NewChainEventInboxRepo().ListUnmatchedForReplay(ctx, crypto.MatchReasonNoAllocation, 50)
		require.NoError(t, err)
		for _, row := range replay {
			require.NotEqual(t, old, row.ID)
		}
		var linked bool
		require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, `SELECT receipt_id IS NOT NULL FROM reg_crypto_inbound_chain_events WHERE id=$1`, fresh).Scan(&linked))
		require.False(t, linked)
	})
}

func TestReceiptIntakeDefersWithoutIndependentProof(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, txm *postgres.TxManager, f sweepCoordinationFixture) {
		rowID := seedUnmatchedTransfer(t, ctx, txm, f, crypto.MatchReasonDust, 0)
		intake, _, _ := receiptIntakeForTest(t, f)
		recorded, err := intake.poll(ctx, crypto.NewChainDriverRegistry())
		require.NoError(t, err)
		require.Zero(t, recorded)
		var attempts int
		var reason string
		var next time.Time
		require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, `SELECT receipt_attempts,receipt_error,receipt_check_at FROM reg_crypto_inbound_chain_events WHERE id=$1`, rowID).
			Scan(&attempts, &reason, &next))
		require.Equal(t, 1, attempts)
		require.NotEmpty(t, reason)
		require.True(t, next.After(time.Now()))
	})
}

// orphaningDriver proves the accounted transfer absent from the canonical chain.
type orphaningDriver struct{ receiptProofDriver }

func (d orphaningDriver) VerifyTransfer(ctx context.Context, event crypto.BlockchainEvent) (crypto.TransferVerification, error) {
	proof, err := d.receiptProofDriver.VerifyTransfer(ctx, event)
	proof.Event.Canonicality, proof.Event.EventType = crypto.CanonicalityOrphaned, crypto.EventTypeReorg
	proof.Outcome, proof.AccountingIdentity = "absent", nil
	return proof, err
}

func TestReceiptAuditOrphanOpensIncidentAndBlocksAttribution(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, txm *postgres.TxManager, f sweepCoordinationFixture) {
		rowID := seedUnmatchedTransfer(t, ctx, txm, f, crypto.MatchReasonWrongToken, 0)
		intake, registry, calls := receiptIntakeForTest(t, f)
		recorded, err := intake.poll(ctx, registry)
		require.NoError(t, err)
		require.Equal(t, 1, recorded, receiptError(t, ctx, txm, rowID))
		var receiptID id.ID
		require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, `SELECT receipt_id FROM reg_crypto_inbound_chain_events WHERE id=$1`, rowID).Scan(&receiptID))

		// Not due yet: a fresh receipt is audited after its first interval.
		orphaned, err := intake.auditCanonicality(ctx, registry, "audit-test")
		require.NoError(t, err)
		require.Zero(t, orphaned)
		_, err = txm.GetQuerier(ctx).Exec(ctx, `UPDATE reg_crypto_custody_receipt_audits SET next_check_at=NOW() WHERE receipt_id=$1`, receiptID)
		require.NoError(t, err)

		// Still canonical: the audit only reschedules.
		orphaned, err = intake.auditCanonicality(ctx, registry, "audit-test")
		require.NoError(t, err)
		require.Zero(t, orphaned)
		var status, lastError string
		var next time.Time
		require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, `SELECT status,last_error,next_check_at FROM reg_crypto_custody_receipt_audits WHERE receipt_id=$1`, receiptID).Scan(&status, &lastError, &next))
		require.Equal(t, "canonical", status)
		require.Empty(t, lastError)
		require.True(t, next.After(time.Now().Add(10*time.Minute)))

		_, err = txm.GetQuerier(ctx).Exec(ctx, `UPDATE reg_crypto_custody_receipt_audits SET next_check_at=NOW() WHERE receipt_id=$1`, receiptID)
		require.NoError(t, err)
		reorged := crypto.NewChainDriverRegistry()
		require.NoError(t, reorged.Register(orphaningDriver{receiptProofDriver{
			fundingReceiptDriver: fundingReceiptDriver{network: crypto.NetworkRef{ID: f.network, Code: "TEST", Family: crypto.ChainFamilyTRON,
				NetworkIdentifier: "nile", NativeTokenID: f.native, NativeDecimals: 6, RequiredConfirmations: 1, FinalityMode: crypto.FinalityByConfirmations},
				receipt: crypto.ChainReceipt{ObservedAt: time.Now().UTC()}},
			calls: calls,
		}}))
		orphaned, err = intake.auditCanonicality(ctx, reorged, "audit-test")
		require.NoError(t, err)
		require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, `SELECT last_error FROM reg_crypto_custody_receipt_audits WHERE receipt_id=$1`, receiptID).Scan(&lastError))
		require.Equal(t, 1, orphaned, lastError)

		var incidents, networkHolds, walletHolds, movements int
		var suspense string
		require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, `SELECT
			(SELECT count(*) FROM reg_crypto_security_incidents WHERE network_id=$1 AND incident_kind='deep_reorg' AND details->>'receiptId'=$3::uuid::text),
			(SELECT count(*) FROM reg_crypto_network_holds WHERE network_id=$1 AND reason='deep_reorg' AND status='open'),
			(SELECT count(*) FROM reg_crypto_wallet_allocation_holds WHERE wallet_id=$2 AND reason='deep_reorg' AND status='open'),
			(SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=$3::uuid),
			(SELECT amount::text FROM reg_crypto_receipt_suspense_balances WHERE receipt_id=$3::uuid)`,
			f.network, f.wallet, receiptID).Scan(&incidents, &networkHolds, &walletHolds, &movements, &suspense))
		require.Equal(t, []int{1, 1, 1, 1}, []int{incidents, networkHolds, walletHolds, movements}, "no automatic ledger reversal")
		require.Equal(t, "25", suspense)

		// The outcome is terminal and a repeated audit pass does nothing.
		orphaned, err = intake.auditCanonicality(ctx, reorged, "audit-test")
		require.NoError(t, err)
		require.Zero(t, orphaned)
		_, err = txm.GetQuerier(ctx).Exec(ctx, `UPDATE reg_crypto_custody_receipt_audits SET status='canonical',orphaned_at=NULL,incident_id=NULL WHERE receipt_id=$1`, receiptID)
		require.ErrorContains(t, err, "immutable")

		// An orphaned receipt cannot be attributed, even by direct SQL.
		_, err = crypto_repo.NewMerchantCreditRepo().LockReceiptRemainder(ctx, receiptID)
		require.ErrorContains(t, err, "transaction")
		require.NoError(t, txm.RunInTransaction(ctx, func(ctx context.Context) error {
			remainder, err := crypto_repo.NewMerchantCreditRepo().LockReceiptRemainder(ctx, receiptID)
			require.NoError(t, err)
			require.True(t, remainder.Orphaned)
			return nil
		}))
	})
}
