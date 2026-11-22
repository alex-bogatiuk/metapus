//go:build integration

package crypto_repo

import (
	"context"
	"errors"
	"sync"
	"testing"

	"metapus/internal/configuration/crypto/domain/crypto/aml"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
	"metapus/pkg/sdk/types"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func assertWithdrawalCancellationCounts(t *testing.T, f amlRoutingFixture, request id.ID, expected []int) {
	t.Helper()
	var facts, cancelled, released, refunds int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT
 (SELECT count(*) FROM reg_crypto_aml_withdrawal_cancellations c JOIN reg_crypto_aml_withdrawal_lines l ON l.id=c.line_id WHERE l.request_id=$1),
 (SELECT count(*) FROM reg_crypto_aml_withdrawal_lines WHERE request_id=$1 AND status='cancelled'),
 (SELECT count(*) FROM reg_crypto_balance_reservations r JOIN reg_crypto_aml_withdrawal_lines l ON l.reservation_id=r.id WHERE l.request_id=$1 AND r.status='released'),
 (SELECT count(*) FROM reg_crypto_merchant_balance_movements WHERE recorder_id=$1 AND record_type='receipt')`, request).Scan(&facts, &cancelled, &released, &refunds))
	require.Equal(t, expected, []int{facts, cancelled, released, refunds})
}

func TestAMLUnsignedWithdrawalCancellationChecksWholeAuditPayload(t *testing.T) {
	f := riskWithdrawalFixture(t, true)
	intent, err := reserveRiskWithdrawal(f, aml.Low, "150", false)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_aml_withdrawal_cancellations(line_id,reason,actor) VALUES($1,'original reason','operator')`, intent.SourceLines[0].LineID)
	require.NoError(t, err)
	tm := postgres.MustGetTxManager(f.ctx)
	cancel := func(reason, actor string) error {
		return tm.RunInTransaction(f.ctx, func(ctx context.Context) error {
			return f.repo.CancelUnsignedAMLWithdrawal(ctx, intent.BasisID, reason, actor)
		})
	}
	require.ErrorContains(t, cancel("different reason", "operator"), "original reason or actor")
	require.ErrorContains(t, cancel("original reason", "another operator"), "original reason or actor")
	assertWithdrawalCancellationCounts(t, f, intent.BasisID, []int{1, 0, 0, 0})
	require.NoError(t, cancel("original reason", "operator"))
	require.NoError(t, cancel("original reason", "operator"))
	require.ErrorContains(t, cancel("different reason", "operator"), "original reason or actor")
	assertWithdrawalCancellationCounts(t, f, intent.BasisID, []int{2, 2, 2, 0})
	// Compensation is deliberately owned by the enclosing rejection command.
	require.ErrorContains(t, f.repo.CancelUnsignedAMLWithdrawal(f.ctx, intent.BasisID, "original reason", "operator"), "compensation transaction")
}

func TestAMLWithdrawalRemainderRejectsPendingEvidenceWithoutPartialRefund(t *testing.T) {
	for _, reason := range []string{"original reason", "another reason"} {
		t.Run(reason, func(t *testing.T) {
			f := riskWithdrawalFixture(t, true)
			intent, err := reserveRiskWithdrawal(f, aml.Low, "150", false)
			require.NoError(t, err)
			_, err = f.pool.Exec(f.ctx, `UPDATE doc_withdrawal_requests SET status='approved' WHERE id=$1`, intent.BasisID)
			require.NoError(t, err)
			_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_aml_withdrawal_cancellations(line_id,reason,actor) VALUES($1,'original reason','merchant')`, intent.SourceLines[0].LineID)
			require.NoError(t, err)
			err = postgres.MustGetTxManager(f.ctx).RunInTransaction(f.ctx, func(ctx context.Context) error {
				_, err := f.repo.CancelAMLWithdrawalRemainder(ctx, f.input.Request.MerchantID, intent.BasisID, reason)
				return err
			})
			require.ErrorContains(t, err, "reconciliation required")
			assertWithdrawalCancellationCounts(t, f, intent.BasisID, []int{1, 0, 0, 0})
		})
	}
}

func TestAMLWithdrawalRemainderRollbackAndConcurrentRetryCreditOnce(t *testing.T) {
	f := riskWithdrawalFixture(t, true)
	intent, err := reserveRiskWithdrawal(f, aml.Low, "150", false)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE doc_withdrawal_requests SET status='approved' WHERE id=$1`, intent.BasisID)
	require.NoError(t, err)
	tm := postgres.MustGetTxManager(f.ctx)
	rollback := errors.New("enclosing command failed")
	err = tm.RunInTransaction(f.ctx, func(ctx context.Context) error {
		amount, err := f.repo.CancelAMLWithdrawalRemainder(ctx, f.input.Request.MerchantID, intent.BasisID, "cancel remainder")
		require.NoError(t, err)
		require.Equal(t, "150", amount.String())
		return rollback
	})
	require.ErrorIs(t, err, rollback)
	assertWithdrawalCancellationCounts(t, f, intent.BasisID, []int{0, 0, 0, 0})
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Go(func() {
			errs[i] = tm.RunInTransaction(f.ctx, func(ctx context.Context) error {
				amount, err := f.repo.CancelAMLWithdrawalRemainder(ctx, f.input.Request.MerchantID, intent.BasisID, "cancel remainder")
				if err == nil && !amount.Equal(types.MustRawAmount("150")) {
					return errors.New("unexpected refunded amount")
				}
				return err
			})
		})
	}
	wg.Wait()
	require.NotEqual(t, errs[0] == nil, errs[1] == nil)
	assertWithdrawalCancellationCounts(t, f, intent.BasisID, []int{2, 2, 2, 2})
	var balance string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT amount::text FROM reg_crypto_merchant_balance_balances WHERE merchant_id=$1 AND token_id=$2`, f.input.Request.MerchantID, f.input.Request.TokenID).Scan(&balance))
	require.Equal(t, "180", balance)
}

func TestAMLWithdrawalCancellationRejectsStaleOuterSnapshot(t *testing.T) {
	f := riskWithdrawalFixture(t, false)
	intent, err := reserveRiskWithdrawal(f, aml.Low, "20", false)
	require.NoError(t, err)
	tm := postgres.MustGetTxManager(f.ctx)
	options := postgres.DefaultTxOptions()
	options.IsolationLevel = pgx.RepeatableRead
	err = tm.RunInTransactionWithOptions(f.ctx, options, func(ctx context.Context) error {
		return f.repo.CancelUnsignedAMLWithdrawal(ctx, intent.BasisID, "cancel request", "merchant")
	})
	require.ErrorContains(t, err, "read committed")
	assertWithdrawalCancellationCounts(t, f, intent.BasisID, []int{0, 0, 0, 0})
}
