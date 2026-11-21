package crypto_repo

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"metapus/internal/configuration/crypto/domain/crypto/aml"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
)

// ReadMerchantRiskBalance uses one read-only repeatable snapshot for the ledger,
// commitments and physical/risk sources. No account is debited or reserved here.
func (r *AMLRepository) ReadMerchantRiskBalance(ctx context.Context, merchantID id.ID) (time.Time, []aml.MerchantBalanceEvidence, error) {
	tm := postgres.MustGetTxManager(ctx)
	options := postgres.DefaultTxOptions()
	options.AccessMode = pgx.ReadOnly
	options.IsolationLevel = pgx.RepeatableRead
	var now time.Time
	var result []aml.MerchantBalanceEvidence
	err := tm.RunInTransactionWithOptions(ctx, options, func(ctx context.Context) error {
		q := tm.GetQuerier(ctx)
		if err := q.QueryRow(ctx, `SELECT transaction_timestamp()`).Scan(&now); err != nil {
			return err
		}
		rows, err := q.Query(ctx, `WITH tokens AS (
 SELECT token_id FROM reg_crypto_merchant_balance_balances WHERE merchant_id=$1
 UNION SELECT token_id FROM reg_crypto_merchant_balance_movements WHERE merchant_id=$1
 UNION SELECT token_id FROM sys_crypto_aml_routing_scopes WHERE merchant_id=$1 AND enabled
 UNION SELECT token_id FROM doc_withdrawal_requests WHERE merchant_id=$1 AND status<>'confirmed'
 UNION SELECT token_id FROM doc_crypto_payments WHERE merchant_id=$1 AND NOT posted AND canonicality='canonical'
   AND status IN ('detected','confirming','confirmed') AND _deleted_at IS NULL AND NOT deletion_mark
 ), financial AS (
 SELECT token_id,SUM(CASE WHEN record_type='receipt' THEN amount ELSE -amount END) amount
 FROM reg_crypto_merchant_balance_movements WHERE merchant_id=$1 GROUP BY token_id
 ), pending AS (
 SELECT token_id,SUM(amount) amount FROM doc_crypto_payments WHERE merchant_id=$1 AND NOT posted AND canonicality='canonical'
 AND status IN ('detected','confirming','confirmed') AND _deleted_at IS NULL AND NOT deletion_mark GROUP BY token_id
 ) SELECT t.id,t.network_id,t.symbol,n.name,t.decimal_places,COALESCE(scope.enabled,FALSE),COALESCE(scope.version,0),
 COALESCE(financial.amount,0),COALESCE(b.amount,0)=COALESCE(financial.amount,0),COALESCE(pending.amount,0),
 EXISTS(SELECT 1 FROM reg_crypto_wallet_risk_states state JOIN cat_wallets w ON w.id=state.wallet_id AND w.custody_merchant_id=$1
   JOIN reg_crypto_balance_balances physical ON physical.wallet_id=w.id AND physical.token_id=state.token_id AND physical.amount>0
   WHERE state.merchant_id=$1 AND state.token_id=t.id AND state.risk_class='high')
 FROM tokens JOIN cat_tokens t ON t.id=tokens.token_id JOIN cat_blockchain_networks n ON n.id=t.network_id
 LEFT JOIN reg_crypto_merchant_balance_balances b ON b.merchant_id=$1 AND b.token_id=t.id
 LEFT JOIN financial ON financial.token_id=t.id LEFT JOIN pending ON pending.token_id=t.id
 LEFT JOIN sys_crypto_aml_routing_scopes scope ON scope.merchant_id=$1 AND scope.network_id=t.network_id AND scope.token_id=t.id
 ORDER BY t.network_id,t.id`, merchantID)
		if err != nil {
			return err
		}
		index := make(map[id.ID]int)
		for rows.Next() {
			entry := aml.MerchantBalanceEvidence{Scope: aml.BalanceScope{MerchantID: merchantID}}
			if err := rows.Scan(&entry.Scope.TokenID, &entry.Scope.NetworkID, &entry.Symbol, &entry.Network, &entry.DecimalPlaces,
				&entry.RoutingEnabled, &entry.ScopeVersion, &entry.Ledger, &entry.LedgerMatches, &entry.PendingChain, &entry.HasHighExposure); err != nil {
				rows.Close()
				return err
			}
			index[entry.Scope.TokenID] = len(result)
			result = append(result, entry)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		// Durable ownership facts preserve the exact net/fee split through partial
		// internal transfers. Coverage revalidates journal, financial and chain
		// evidence even before a background projection catches up.
		rows, err = q.Query(ctx, `SELECT p.wallet_id,p.token_id,p.principal,COALESCE(state.risk_class,'unknown'),p.revision,COALESCE(state.revision,0),COALESCE(state.scope_version,0),
 COALESCE(state.inventory_covered,FALSE) AND fn_crypto_aml_total_inventory_covered(p.wallet_id,p.token_id),state.expires_at,
 EXISTS(SELECT 1 FROM reg_crypto_balance_reservations reserve WHERE reserve.wallet_id=p.wallet_id AND reserve.token_id=p.token_id AND reserve.status='active') OR
 EXISTS(SELECT 1 FROM reg_crypto_fee_reservations fee WHERE fee.payer_wallet_id=p.wallet_id AND fee.native_token_id=p.token_id AND fee.status IN ('reserved','broadcast','held')) OR
 EXISTS(SELECT 1 FROM reg_crypto_aml_internal_allocations plan WHERE plan.token_id=p.token_id AND plan.status='reserved' AND (plan.source_wallet_id=p.wallet_id OR plan.destination_wallet_id=p.wallet_id)) OR
 EXISTS(SELECT 1 FROM reg_crypto_aml_resource_budgets budget WHERE budget.wallet_id=p.wallet_id AND budget.token_id=p.token_id AND budget.status='reserved') OR
 EXISTS(SELECT 1 FROM reg_crypto_aml_batches batch WHERE batch.collector_wallet_id=p.wallet_id AND batch.token_id=p.token_id AND batch.execution_held)
 FROM reg_crypto_aml_inventory_positions p LEFT JOIN reg_crypto_wallet_risk_states state ON state.wallet_id=p.wallet_id AND state.token_id=p.token_id
 WHERE p.merchant_id=$1 AND p.principal>0 ORDER BY p.token_id,p.wallet_id`, merchantID)
		if err != nil {
			return fmt.Errorf("read AML net principal sources: %w", err)
		}
		for rows.Next() {
			var tokenID id.ID
			var source aml.PrincipalPosition
			if err := rows.Scan(&source.WalletID, &tokenID, &source.Amount, &source.Class, &source.Revision, &source.RiskRevision, &source.ScopeVersion, &source.Covered, &source.ExpiresAt, &source.Held); err != nil {
				rows.Close()
				return err
			}
			if i, ok := index[tokenID]; ok {
				result[i].Sources = append(result[i].Sources, source)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		rows, err = q.Query(ctx, `SELECT request.id,request.token_id,GREATEST(COALESCE(debit.amount,0)-COALESCE((SELECT SUM(l.amount) FROM reg_crypto_aml_withdrawal_lines l WHERE l.request_id=request.id AND l.status='confirmed'),0),0),request.status NOT IN ('failed','rejected','partially_completed')
 FROM doc_withdrawal_requests request JOIN LATERAL(
 SELECT SUM(CASE WHEN record_type='expense' THEN amount ELSE -amount END) amount FROM reg_crypto_merchant_balance_movements
 WHERE recorder_id=request.id AND recorder_type='WithdrawalRequest' AND merchant_id=$1 AND token_id=request.token_id
 ) debit ON TRUE WHERE request.merchant_id=$1 AND request.status<>'confirmed'
 AND (debit.amount>0 OR request.status NOT IN ('failed','rejected')) ORDER BY request.token_id,request.id`, merchantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var tokenID id.ID
			var commitment aml.DebitCommitment
			if err := rows.Scan(&commitment.RequestID, &tokenID, &commitment.Amount, &commitment.Executable); err != nil {
				return err
			}
			if i, ok := index[tokenID]; ok {
				result[i].Commitments = append(result[i].Commitments, commitment)
			}
		}
		return rows.Err()
	})
	return now, result, err
}

var _ aml.MerchantBalanceRepository = (*AMLRepository)(nil)
