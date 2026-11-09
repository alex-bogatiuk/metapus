//go:build integration

package crypto_worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	oteltrace "go.opentelemetry.io/otel/trace"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_payment"
	"metapus/internal/domain/registers/crypto_balance"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/infrastructure/storage/postgres/document_repo"
	"metapus/internal/infrastructure/storage/postgres/register_repo"
	platformtelemetry "metapus/internal/platform/telemetry"
)

type remediationFixture struct {
	network, token, merchant, wallet id.ID
	code                             string
}

func withRemediationFixture(t *testing.T, fn func(context.Context, *postgres.TxManager, remediationFixture)) {
	t.Helper()
	ctx, txm, closePool := cryptoIntegrationTxManager(t)
	defer closePool()
	rollback := errors.New("rollback remediation fixture")
	err := txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		f := remediationFixture{id.New(), id.New(), id.New(), id.New(), "REMED-" + id.Compact(id.New())}
		q := txm.GetQuerier(txCtx)
		_, err := q.Exec(txCtx, `INSERT INTO cat_blockchain_networks(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals)
			VALUES($1,$2,'Remediation','evm',$2,'ETH',18)`, f.network, f.code)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `INSERT INTO cat_tokens(id,code,name,network_id,symbol,decimal_places,token_standard)
			VALUES($1,$2,'Remediation token',$3,'RMD',18,'native')`, f.token, f.code, f.network)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$2,'Remediation')`, f.merchant, f.code)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `INSERT INTO cat_wallets(id,code,name,network_id,address,tier,wallet_role,status,vault_key_id,chain_activation_status)
			VALUES($1,$2,'Remediation',$3,$2,'hot','liquidity_hot','free',$2,'active')`, f.wallet, f.code, f.network)
		require.NoError(t, err)
		fn(txCtx, txm, f)
		return rollback
	})
	require.ErrorIs(t, err, rollback)
}

func TestRemediationTokenIdentityCannotChangeAfterMovement(t *testing.T) {
	withRemediationFixture(t, func(ctx context.Context, txm *postgres.TxManager, f remediationFixture) {
		q := txm.GetQuerier(ctx)
		_, err := q.Exec(ctx, `INSERT INTO reg_crypto_balance_movements
			(line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
			VALUES($1,$2,'Remediation',1,NOW(),'receipt',$3,$4,100)`,
			id.New(), id.New(), f.wallet, f.token)
		require.NoError(t, err)
		_, err = q.Exec(ctx, `UPDATE cat_tokens SET decimal_places=6 WHERE id=$1`, f.token)
		require.Error(t, err)
	})
}

func TestRemediationReversalPreservesOriginalMovement(t *testing.T) {
	withRemediationFixture(t, func(ctx context.Context, txm *postgres.TxManager, f remediationFixture) {
		q := txm.GetQuerier(ctx)
		recorderID, lineID := id.New(), id.New()
		_, err := q.Exec(ctx, `INSERT INTO reg_crypto_balance_movements
			(line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
			VALUES($1,$2,'Remediation',1,NOW(),'receipt',$3,$4,100)`,
			lineID, recorderID, f.wallet, f.token)
		require.NoError(t, err)
		svc := crypto_balance.NewService(register_repo.NewCryptoBalanceRepo())
		require.NoError(t, svc.ReverseMovements(ctx, recorderID, 2))
		require.NoError(t, svc.ReverseMovements(ctx, recorderID, 2))
		var count int
		var signedSum string
		require.NoError(t, q.QueryRow(ctx, `
			SELECT COUNT(*), SUM(CASE WHEN record_type='receipt' THEN amount ELSE -amount END)::text
			FROM reg_crypto_balance_movements WHERE recorder_id=$1
		`, recorderID).Scan(&count, &signedSum))
		require.Equal(t, 2, count)
		require.Equal(t, "0", signedSum)
		var reversalOf id.ID
		require.NoError(t, q.QueryRow(ctx, `
			SELECT reversal_of_line_id FROM reg_crypto_balance_movements
			WHERE recorder_id=$1 AND recorder_type='CryptoLedgerReversal'
		`, recorderID).Scan(&reversalOf))
		require.Equal(t, lineID, reversalOf)
		err = txm.RunInSavepoint(ctx, func(txCtx context.Context) error {
			_, updateErr := txm.GetQuerier(txCtx).Exec(txCtx,
				`UPDATE reg_crypto_balance_movements SET amount=1 WHERE line_id=$1`, lineID)
			return updateErr
		})
		require.Error(t, err)
		err = txm.RunInSavepoint(ctx, func(txCtx context.Context) error {
			_, deleteErr := txm.GetQuerier(txCtx).Exec(txCtx,
				`DELETE FROM reg_crypto_balance_movements WHERE line_id=$1`, lineID)
			return deleteErr
		})
		require.Error(t, err)
	})
}

func TestRemediationSecondFinancialReorgCompensatesNewGeneration(t *testing.T) {
	withRemediationFixture(t, func(ctx context.Context, txm *postgres.TxManager, f remediationFixture) {
		q := txm.GetQuerier(ctx)
		paymentID, invoiceID := id.New(), id.New()
		txHash := "remediation-" + paymentID.String()
		_, err := q.Exec(ctx, `INSERT INTO doc_crypto_payments
			(id,invoice_id,merchant_id,network_id,token_id,wallet_id,tx_hash,amount,status,posted,posted_version)
			VALUES($1,$2,$3,$4,$5,$6,$7,100,'settled',TRUE,1)`,
			paymentID, invoiceID, f.merchant, f.network, f.token, f.wallet, txHash)
		require.NoError(t, err)
		p := &crypto_payment.CryptoPayment{
			NetworkID: f.network, InvoiceID: invoiceID, WalletID: f.wallet,
			TokenID: f.token, MerchantID: f.merchant, TxHash: txHash,
			EventKind: "native_value", Amount: types.NewCryptoAmountFromInt64(100),
		}
		p.ID = paymentID
		p.FeeFixed = types.NewCryptoAmountFromInt64(5)
		_, err = q.Exec(ctx, `UPDATE doc_crypto_payments SET fee_fixed=5 WHERE id=$1`, paymentID)
		require.NoError(t, err)
		repo := crypto_repo.NewFinancialReorgRepo()
		for version := 1; version <= 2; version++ {
			_, err = q.Exec(ctx, `INSERT INTO reg_crypto_balance_movements
				(line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
				VALUES($1,$2,'CryptoPayment',$3,NOW(),'receipt',$4,$5,100)`,
				id.New(), paymentID, version, f.wallet, f.token)
			require.NoError(t, err)
			_, err = q.Exec(ctx, `INSERT INTO reg_crypto_merchant_balance_movements
				(line_id,recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,amount)
				VALUES($1,$2,'CryptoPayment',$3,NOW(),'receipt',$4,$5,95)`,
				id.New(), paymentID, version, f.merchant, f.token)
			require.NoError(t, err)
			_, err = q.Exec(ctx, `INSERT INTO reg_crypto_fee_movements
				(line_id,recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,fee_type,amount)
				VALUES($1,$2,'CryptoPayment',$3,NOW(),'receipt',$4,$5,'processing',5)`,
				id.New(), paymentID, version, f.merchant, f.token)
			require.NoError(t, err)
			p.PostedVersion = version
			_, err = q.Exec(ctx, `UPDATE doc_crypto_payments SET posted_version=$2 WHERE id=$1`, paymentID, version)
			require.NoError(t, err)
			event := crypto.BlockchainEvent{BlockNumber: int64(version), BlockHash: "orphaned"}
			require.NoError(t, repo.RecordFinancialReorg(ctx, p, event))
			require.NoError(t, repo.RecordFinancialReorg(ctx, p, event))
		}
		var balance string
		require.NoError(t, q.QueryRow(ctx, `SELECT amount::text FROM reg_crypto_balance_balances
			WHERE wallet_id=$1 AND token_id=$2`, f.wallet, f.token).Scan(&balance))
		require.Equal(t, "0", balance)
		var compensations, linkedReversals int
		require.NoError(t, q.QueryRow(ctx, `SELECT COUNT(*) FROM reg_crypto_reorg_compensations
			WHERE payment_id=$1`, paymentID).Scan(&compensations))
		require.NoError(t, q.QueryRow(ctx, `SELECT COUNT(*) FROM reg_crypto_balance_movements
			WHERE recorder_type='CryptoPaymentReorg' AND reversal_of_line_id IS NOT NULL
			  AND reversal_reason='posted_payment_reorg'`).Scan(&linkedReversals))
		require.Equal(t, 2, compensations)
		require.Equal(t, 2, linkedReversals)
		for _, table := range []string{"reg_crypto_merchant_balance_movements", "reg_crypto_fee_movements"} {
			var count int
			query := `SELECT COUNT(*) FROM ` + table + ` WHERE recorder_type='CryptoPaymentReorg'
			    AND reversal_of_line_id IS NOT NULL AND reversal_reason='posted_payment_reorg'`
			require.NoError(t, q.QueryRow(ctx, query).Scan(&count))
			require.Equal(t, 2, count, table)
		}
		var merchantBalance, feeBalance string
		require.NoError(t, q.QueryRow(ctx, `SELECT amount::text FROM reg_crypto_merchant_balance_balances
			WHERE merchant_id=$1 AND token_id=$2`, f.merchant, f.token).Scan(&merchantBalance))
		require.NoError(t, q.QueryRow(ctx, `SELECT balance::text FROM reg_crypto_fee_balances
			WHERE merchant_id=$1 AND token_id=$2 AND fee_type='processing'`, f.merchant, f.token).Scan(&feeBalance))
		require.Equal(t, "0", merchantBalance)
		require.Equal(t, "0", feeBalance)
	})
}

func TestRemediationConfirmationClaimReachesNewPayment(t *testing.T) {
	withRemediationFixture(t, func(ctx context.Context, txm *postgres.TxManager, f remediationFixture) {
		q := txm.GetQuerier(ctx)
		_, err := q.Exec(ctx, `INSERT INTO doc_crypto_payments
			(id,invoice_id,merchant_id,network_id,token_id,wallet_id,tx_hash,amount,status)
			SELECT gen_random_uuid_v7(),$1,$2,$3,$4,$5,'remediation-'||$6||'-'||n,100,'detected'
			FROM generate_series(1,201) AS n`,
			id.New(), f.merchant, f.network, f.token, f.wallet, f.code)
		require.NoError(t, err)
		repo := document_repo.NewCryptoPaymentRepo()
		first, err := repo.ClaimForConfirmation(ctx, []crypto_payment.PaymentStatus{crypto_payment.PaymentStatusDetected},
			"remediation-worker", 200, 30*time.Second)
		require.NoError(t, err)
		require.Len(t, first, 200)
		for _, payment := range first {
			require.NoError(t, repo.ReleaseConfirmationClaim(ctx, payment.ID, "remediation-worker"))
		}
		second, err := repo.ClaimForConfirmation(ctx, []crypto_payment.PaymentStatus{crypto_payment.PaymentStatusDetected},
			"remediation-worker", 200, 30*time.Second)
		require.NoError(t, err)
		require.Len(t, second, 1)
	})
}

func TestRemediationSettledPaymentCanonicalityAuditIsDueAndLeased(t *testing.T) {
	withRemediationFixture(t, func(ctx context.Context, txm *postgres.TxManager, f remediationFixture) {
		q := txm.GetQuerier(ctx)
		ids := []id.ID{id.New(), id.New(), id.New()}
		for i, paymentID := range ids {
			_, err := q.Exec(ctx, `INSERT INTO doc_crypto_payments
				(id,invoice_id,merchant_id,network_id,token_id,wallet_id,tx_hash,amount,status,posted,posted_version,block_number,block_hash,next_canonicality_check_at)
				VALUES($1,$2,$3,$4,$5,$6,$7,100,'settled',TRUE,1,100,'0xcanonical',NOW()-INTERVAL '1 day'+$8::INT*INTERVAL '1 second')`,
				paymentID, id.New(), f.merchant, f.network, f.token, f.wallet, "audit-"+paymentID.String(), i)
			require.NoError(t, err)
		}
		repo := document_repo.NewCryptoPaymentRepo()
		claimed, err := repo.ClaimForCanonicalityAudit(ctx, "canonicality-worker", 2, time.Minute)
		require.NoError(t, err)
		require.Len(t, claimed, 2)
		require.NoError(t, repo.ReleaseCanonicalityAuditClaim(ctx, claimed[0].ID, "canonicality-worker", nil))
		require.NoError(t, repo.ReleaseCanonicalityAuditClaim(ctx, claimed[1].ID, "canonicality-worker", errors.New("rpc unavailable")))
		var checked bool
		require.NoError(t, q.QueryRow(ctx, `SELECT last_canonicality_checked_at IS NOT NULL
			FROM doc_crypto_payments WHERE id=$1`, claimed[0].ID).Scan(&checked))
		require.True(t, checked)
		require.NoError(t, q.QueryRow(ctx, `SELECT last_canonicality_checked_at IS NOT NULL
			FROM doc_crypto_payments WHERE id=$1`, claimed[1].ID).Scan(&checked))
		require.False(t, checked)
		next, err := repo.ClaimForCanonicalityAudit(ctx, "canonicality-worker", 2, time.Minute)
		require.NoError(t, err)
		require.Len(t, next, 1)
		require.Equal(t, ids[2], next[0].ID)
	})
}

func TestRemediationReconciliationPagesCoverEveryWallet(t *testing.T) {
	withRemediationFixture(t, func(ctx context.Context, txm *postgres.TxManager, f remediationFixture) {
		q := txm.GetQuerier(ctx)
		_, err := q.Exec(ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,tier,wallet_role,status,vault_key_id,chain_activation_status)
			SELECT gen_random_uuid_v7(),$1||'-'||n,'Reconciliation wallet',$2,$1||'-address-'||n,
			       'hot','liquidity_hot','free',$1,'active'
			FROM generate_series(1,2500) AS n`, f.code, f.network)
		require.NoError(t, err)
		processor := &CryptoProcessor{}
		upper, err := processor.reconciliationUpperBound(ctx, nil, f.code)
		require.NoError(t, err)
		require.NotNil(t, upper)
		_, err = q.Exec(ctx, `INSERT INTO cat_tokens
			(id,code,name,network_id,symbol,decimal_places,token_standard,contract_address,created_at)
			VALUES($1,$2,'Future token',$3,'FUT',6,'ERC-20',$4,$5)`,
			id.New(), f.code+"-future", f.network,
			"0x1111111111111111111111111111111111111111", upper.SnapshotAt.Add(time.Second))
		require.NoError(t, err)
		var cursor *reconciliationCursor
		count, pages := 0, 0
		for {
			page, err := processor.loadReconciliationWalletsPage(ctx, nil, f.code, cursor, upper)
			require.NoError(t, err)
			if len(page) == 0 {
				break
			}
			pages++
			count += len(page)
			last := page[len(page)-1]
			cursor = &reconciliationCursor{NetworkID: last.NetworkID, WalletID: last.WalletID, TokenID: last.TokenID}
		}
		require.Greater(t, pages, 4)
		require.Equal(t, 2501, count)
	})
}

func TestRemediationUnmatchedReplayDoesNotStarveNewEvent(t *testing.T) {
	withRemediationFixture(t, func(ctx context.Context, txm *postgres.TxManager, f remediationFixture) {
		q := txm.GetQuerier(ctx)
		_, err := q.Exec(ctx, `INSERT INTO reg_crypto_inbound_chain_events
			(id,network_id,tx_hash,event_kind,event_index,to_address,block_timestamp,match_status,match_reason)
			SELECT gen_random_uuid_v7(),$1,'remediation-unmatched-'||$2||'-'||n,'native_value',0,$2,NOW(),
			       'unmatched','no_allocation'
			FROM generate_series(1,51) AS n`, f.network, f.code)
		require.NoError(t, err)
		inbox := crypto_repo.NewChainEventInboxRepo()
		first, err := inbox.ListUnmatchedForReplay(ctx, crypto.MatchReasonNoAllocation, 50)
		require.NoError(t, err)
		require.Len(t, first, 50)
		second, err := inbox.ListUnmatchedForReplay(ctx, crypto.MatchReasonNoAllocation, 50)
		require.NoError(t, err)
		require.Len(t, second, 1)
		require.NoError(t, inbox.MarkTerminal(ctx, second[0].ID, crypto.InboxUnmatched,
			crypto.MatchReasonNoAllocation, nil))
		var dueInFuture bool
		require.NoError(t, q.QueryRow(ctx, `SELECT next_retry_at > NOW() FROM reg_crypto_inbound_chain_events
			WHERE id=$1`, second[0].ID).Scan(&dueInFuture))
		require.True(t, dueInFuture)
	})
}

func TestRemediationWatcherAcknowledgesDurableObservations(t *testing.T) {
	withRemediationFixture(t, func(ctx context.Context, txm *postgres.TxManager, f remediationFixture) {
		processor := &CryptoProcessor{}
		_, err := platformtelemetry.ConfigureTracing(ctx, "remediation-test", "dev", func(string) string { return "" })
		require.NoError(t, err)
		traceID, err := oteltrace.TraceIDFromHex("11111111111111111111111111111111")
		require.NoError(t, err)
		spanID, err := oteltrace.SpanIDFromHex("2222222222222222")
		require.NoError(t, err)
		ctx = oteltrace.ContextWithSpanContext(ctx, oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
			TraceID: traceID, SpanID: spanID, TraceFlags: oteltrace.FlagsSampled,
		}))
		event := crypto.BlockchainEvent{
			NetworkID: f.network, TxHash: "remediation-observation-" + f.code,
			EventKind: crypto.EventKindNativeValue, EventIndex: 0,
			BlockNumber: 100, BlockHash: "canonical-a", Timestamp: time.Now().UTC(),
		}
		require.NoError(t, processor.Process(ctx, event))
		require.NoError(t, processor.Process(ctx, event))
		event.BlockHash = "canonical-b"
		require.NoError(t, processor.Process(ctx, event))
		repo := crypto_repo.NewChainObservationRepo()
		claimed, err := repo.Claim(ctx, "remediation-observer", 10, time.Minute)
		require.NoError(t, err)
		require.Len(t, claimed, 2)
		require.Contains(t, claimed[0].Traceparent, traceID.String())
		require.Equal(t, claimed[0].CorrelationID, claimed[1].CorrelationID)
		require.NotEqual(t, claimed[0].CausationID, claimed[1].CausationID)
		require.NoError(t, repo.Complete(ctx, claimed[0].ID, "remediation-observer"))
		needsReview, err := repo.Fail(ctx, claimed[1].ID, "remediation-observer", errors.New("temporary business failure"))
		require.NoError(t, err)
		require.False(t, needsReview)
		var captured, completed, pending int
		require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, `SELECT COUNT(*),
			COUNT(*) FILTER (WHERE status='processed'),COUNT(*) FILTER (WHERE status='pending')
			FROM reg_crypto_chain_observations WHERE network_id=$1`, f.network).
			Scan(&captured, &completed, &pending))
		require.Equal(t, 2, captured)
		require.Equal(t, 1, completed)
		require.Equal(t, 1, pending)
	})
}

func TestRemediationWatcherFencingRejectsStaleOwner(t *testing.T) {
	withRemediationFixture(t, func(ctx context.Context, txm *postgres.TxManager, f remediationFixture) {
		repo := crypto_repo.NewWatcherStateRepo()
		candidate := &crypto.WatcherState{NetworkID: f.network, WatcherKey: "remediation", LastBlock: 100, LastTimestamp: 100}
		first, err := repo.Acquire(ctx, candidate, "owner-1")
		require.NoError(t, err)
		_, err = repo.Acquire(ctx, candidate, "owner-2")
		require.ErrorIs(t, err, crypto.ErrWatcherLeaseHeld)
		first.LastBlock, first.LastTimestamp = 101, 101
		require.NoError(t, repo.Save(ctx, first))
		require.NoError(t, repo.Release(ctx, first))
		second, err := repo.Acquire(ctx, candidate, "owner-2")
		require.NoError(t, err)
		require.Greater(t, second.FencingToken, first.FencingToken)
		first.LastBlock, first.LastTimestamp = 102, 102
		require.ErrorIs(t, repo.Save(ctx, first), crypto.ErrWatcherLeaseLost)
		second.LastBlock, second.LastTimestamp = 102, 102
		require.NoError(t, repo.Save(ctx, second))
		second.LastBlock, second.LastTimestamp, second.Fingerprint = 99, 99, "common-ancestor"
		require.NoError(t, repo.Rewind(ctx, second, "verified common ancestor hash"))
		var block, rewinds int
		require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, `SELECT last_block FROM sys_chain_watcher_state
			WHERE network_id=$1 AND watcher_key='remediation'`, f.network).Scan(&block))
		require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, `SELECT COUNT(*) FROM sys_chain_watcher_rewinds
			WHERE network_id=$1 AND watcher_key='remediation'`, f.network).Scan(&rewinds))
		require.Equal(t, 99, block)
		require.Equal(t, 1, rewinds)
	})
}

func TestRemediationFinancialAdjustmentRequiresDifferentChecker(t *testing.T) {
	withRemediationFixture(t, func(ctx context.Context, txm *postgres.TxManager, f remediationFixture) {
		q := txm.GetQuerier(ctx)
		paymentID, makerID, checkerID := id.New(), id.New(), id.New()
		_, err := q.Exec(ctx, `INSERT INTO doc_crypto_payments
			(id,invoice_id,merchant_id,network_id,token_id,wallet_id,tx_hash,amount,status,posted,posted_version)
			VALUES($1,$2,$3,$4,$5,$6,$7,100,'settled',TRUE,1)`,
			paymentID, id.New(), f.merchant, f.network, f.token, f.wallet,
			"adjustment-"+paymentID.String())
		require.NoError(t, err)
		_, err = q.Exec(ctx, `INSERT INTO reg_crypto_balance_movements
			(line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
			VALUES($1,$2,'CryptoPayment',1,NOW(),'receipt',$3,$4,100)`,
			id.New(), paymentID, f.wallet, f.token)
		require.NoError(t, err)
		_, err = q.Exec(ctx, `INSERT INTO reg_crypto_merchant_balance_movements
			(line_id,recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,amount)
			VALUES($1,$2,'CryptoPayment',1,NOW(),'receipt',$3,$4,95)`,
			id.New(), paymentID, f.merchant, f.token)
		require.NoError(t, err)
		_, err = q.Exec(ctx, `INSERT INTO reg_crypto_fee_movements
			(line_id,recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,fee_type,amount)
			VALUES($1,$2,'CryptoPayment',1,NOW(),'receipt',$3,$4,'processing',5)`,
			id.New(), paymentID, f.merchant, f.token)
		require.NoError(t, err)
		service := cryptoapp.NewFinancialAdjustmentService(postgres.NewContextTxManager(),
			crypto_repo.NewFinancialAdjustmentRepo())
		cmd := cryptoapp.ProposeFinancialAdjustmentCommand{
			PaymentID: paymentID, PostedVersion: 1, ReasonCode: "duplicate_financial_effect",
			Evidence:       json.RawMessage(`{"description":"remediation integration case"}`),
			IdempotencyKey: "adjustment-" + paymentID.String(), MakerID: makerID,
		}
		adjustment, err := service.Propose(ctx, cmd)
		require.NoError(t, err)
		require.False(t, adjustment.CreatedAt.IsZero())
		pending, err := service.List(ctx, "pending_approval", 100)
		require.NoError(t, err)
		require.Contains(t, adjustmentIDs(pending), adjustment.ID)
		duplicate, err := service.Propose(ctx, cmd)
		require.NoError(t, err)
		require.Equal(t, adjustment.ID, duplicate.ID)
		_, err = service.Approve(ctx, adjustment.ID, makerID, "self approval")
		require.Error(t, err)
		posted, err := service.Approve(ctx, adjustment.ID, checkerID, "verified duplicate effect")
		require.NoError(t, err)
		require.Equal(t, "posted", posted.Status)
		require.NotNil(t, posted.DecidedAt)
		pending, err = service.List(ctx, "pending_approval", 100)
		require.NoError(t, err)
		require.NotContains(t, adjustmentIDs(pending), adjustment.ID)
		approved, err := service.List(ctx, "posted", 100)
		require.NoError(t, err)
		require.Contains(t, adjustmentIDs(approved), adjustment.ID)
		_, err = service.Approve(ctx, adjustment.ID, checkerID, "repeat approval")
		require.Error(t, err)
		for _, table := range []string{"reg_crypto_balance_movements", "reg_crypto_merchant_balance_movements", "reg_crypto_fee_movements"} {
			var reversalCount int
			require.NoError(t, q.QueryRow(ctx, `SELECT COUNT(*) FROM `+table+`
				WHERE recorder_id=$1 AND recorder_type='CryptoAdjustment'
				  AND reversal_of_line_id IS NOT NULL`, adjustment.ID).Scan(&reversalCount))
			require.Equal(t, 1, reversalCount, table)
		}
	})
}

func adjustmentIDs(adjustments []cryptoapp.FinancialAdjustment) []id.ID {
	ids := make([]id.ID, 0, len(adjustments))
	for _, adjustment := range adjustments {
		ids = append(ids, adjustment.ID)
	}
	return ids
}
