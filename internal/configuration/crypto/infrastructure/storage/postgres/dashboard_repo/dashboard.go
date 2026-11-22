// Package dashboard_repo provides bounded administration projections. Money is
// aggregated in PostgreSQL as integer NUMERIC and crosses the wire as strings.
package dashboard_repo

import (
	"context"
	"time"

	"metapus/internal/configuration/crypto/domain/dashboard"
	"metapus/pkg/sdk/postgres"
)

type Repository struct{}

func New() *Repository { return &Repository{} }

var _ dashboard.Repository = (*Repository)(nil)

const queueProjection = `WITH queue AS (
 SELECT r.id, 'withdrawal'::text kind, r.number, r.status, COALESCE(m.name,'') merchant,
 r.token_id, t.symbol, n.name network, t.decimal_places, r.amount::text amount, r.created_at, 1 priority
 FROM doc_withdrawal_requests r JOIN cat_tokens t ON t.id=r.token_id
 JOIN cat_blockchain_networks n ON n.id=t.network_id LEFT JOIN cat_merchants m ON m.id=r.merchant_id
 WHERE r.status IN ('pending_signature','pending_approval') AND r.deletion_mark=FALSE AND r._deleted_at IS NULL
 UNION ALL
 SELECT d.id, 'inbound', d.number,
 CASE WHEN EXISTS(SELECT 1 FROM doc_crypto_merchant_credits c WHERE c.receipt_id=d.id AND c.status='prepared')
        OR EXISTS(SELECT 1 FROM doc_crypto_platform_fundings f WHERE f.receipt_id=d.id AND f.status='prepared')
      THEN 'awaiting_approval' ELSE 'unattributed' END,
 COALESCE(m.name,''), d.token_id, t.symbol, n.name, t.decimal_places, s.amount::text, d.date, 1
 FROM doc_crypto_custody_receipts d
 JOIN reg_crypto_receipt_suspense_balances s ON s.receipt_id=d.id AND s.amount>0
 JOIN reg_crypto_custody_receipt_audits a ON a.receipt_id=d.id AND a.status='canonical'
 JOIN cat_tokens t ON t.id=d.token_id JOIN cat_blockchain_networks n ON n.id=d.network_id
 LEFT JOIN cat_merchants m ON m.id=d.custody_merchant_id
 WHERE d.reason_code<>'dust'
 UNION ALL
 SELECT e.id, 'outflow', COALESCE(w.name,'Кошелёк'), e.status, '', e.token_id,
 COALESCE(t.symbol,''), n.name, COALESCE(t.decimal_places,0), e.amount::text, e.detected_at, 0
 FROM reg_crypto_external_outflows e LEFT JOIN cat_tokens t ON t.id=e.token_id
 JOIN cat_blockchain_networks n ON n.id=e.network_id LEFT JOIN cat_wallets w ON w.id=e.source_wallet_id
 WHERE e.status IN ('pending_review','classified')
) `

func (r *Repository) Queues(ctx context.Context, kind string, limit int) (dashboard.Queues, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	result := dashboard.Queues{Items: make([]dashboard.QueueItem, 0, limit)}
	err := q.QueryRow(ctx, queueProjection+`SELECT COUNT(*) FILTER(WHERE kind='withdrawal'),
 COUNT(*) FILTER(WHERE kind='inbound'), COUNT(*) FILTER(WHERE kind='outflow'), NOW() FROM queue`).Scan(&result.Withdrawals, &result.Inbound, &result.Outflows, &result.ObservedAt)
	if err != nil {
		return result, err
	}
	result.Total = result.Withdrawals + result.Inbound + result.Outflows
	rows, err := q.Query(ctx, queueProjection+`SELECT id::text,kind,number,status,merchant,COALESCE(token_id::text,''),symbol,network,decimal_places,amount,created_at
 FROM queue WHERE ($1='all' OR kind=$1) ORDER BY priority,created_at,id LIMIT $2`, kind, limit)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item dashboard.QueueItem
		if err := rows.Scan(&item.ID, &item.Kind, &item.Number, &item.Status, &item.Merchant, &item.TokenID, &item.Symbol, &item.Network, &item.DecimalPlaces, &item.Amount, &item.CreatedAt); err != nil {
			return result, err
		}
		result.Items = append(result.Items, item)
	}
	return result, rows.Err()
}

func (r *Repository) Assets(ctx context.Context) ([]dashboard.Asset, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	rows, err := q.Query(ctx, `WITH merchant_balances AS (
 SELECT token_id,SUM(amount) amount FROM reg_crypto_merchant_balance_balances GROUP BY token_id
), payouts AS (
 -- A request debits the merchant's available balance when posted, but the
 -- platform owes that money until a payout is recognised on the wallet. What
 -- is still owed is the net request debit less the principal its recognised
 -- payouts spent. Terminal requests carry no remainder: a failed or rejected
 -- request restores its debit before it closes.
 SELECT r.token_id,SUM(debit.amount-COALESCE(paid.amount,0)) amount,
 COUNT(*) FILTER (WHERE debit.amount-COALESCE(paid.amount,0)<>0) requests
 FROM doc_withdrawal_requests r
 CROSS JOIN LATERAL (SELECT COALESCE(SUM(CASE WHEN m.record_type='expense' THEN m.amount ELSE -m.amount END),0) amount
   FROM reg_crypto_merchant_balance_movements m WHERE m.recorder_id=r.id AND m.recorder_type='WithdrawalRequest' AND m.token_id=r.token_id) debit
 LEFT JOIN LATERAL (SELECT SUM(CASE WHEN m.record_type='expense' THEN m.amount ELSE -m.amount END) amount
   FROM reg_crypto_withdrawal_custody_bindings b JOIN reg_crypto_balance_movements m ON m.recorder_id=b.withdrawal_id AND m.recorder_type='CryptoWithdrawal' AND m.token_id=r.token_id
   WHERE b.request_id=r.id) paid ON TRUE
 WHERE r.status NOT IN ('confirmed','rejected','failed') GROUP BY r.token_id
), wallet_balances AS (
 SELECT token_id,SUM(amount) amount FROM reg_crypto_balance_balances GROUP BY token_id
), latest_checks AS (
 SELECT DISTINCT ON (wallet_id,token_id) wallet_id,token_id,ledger_amount,chain_amount,delta_amount,observed_at
 FROM reg_crypto_reconciliation_checks ORDER BY wallet_id,token_id,observed_at DESC,id DESC
), checks AS (
 SELECT t.id token_id,COUNT(w.id) wallets,COUNT(c.wallet_id) checked,MIN(c.observed_at) observed_at,COUNT(c.wallet_id) FILTER (WHERE c.delta_amount<>0) mismatched,
 SUM(c.ledger_amount) ledger_amount,SUM(c.chain_amount) chain_amount,SUM(c.delta_amount) delta_amount
 FROM cat_tokens t JOIN cat_wallets w ON w.network_id=t.network_id AND ((w.is_active=TRUE AND w.deletion_mark=FALSE AND w._deleted_at IS NULL) OR EXISTS(SELECT 1 FROM reg_crypto_balance_balances bb WHERE bb.wallet_id=w.id AND bb.token_id=t.id AND bb.amount<>0))
 LEFT JOIN latest_checks c ON c.wallet_id=w.id AND c.token_id=t.id GROUP BY t.id
)
 SELECT t.id::text,t.network_id::text,t.symbol,n.name,n.color,t.decimal_places,
 COALESCE(m.amount,0)::text,COALESCE(p.amount,0)::text,(COALESCE(m.amount,0)+COALESCE(p.amount,0))::text,COALESCE(p.requests,0),COALESCE(b.amount,0)::text,
 CASE WHEN c.wallets=c.checked AND c.checked>0 THEN c.ledger_amount::text END,
 CASE WHEN c.wallets=c.checked AND c.checked>0 THEN c.chain_amount::text END,
 CASE WHEN c.wallets=c.checked AND c.checked>0 THEN c.delta_amount::text END,
 COALESCE(c.wallets,0),COALESCE(c.checked,0),COALESCE(c.mismatched,0),c.observed_at,t.currency_id,COALESCE(curr.iso_code,'')
 FROM cat_tokens t JOIN cat_blockchain_networks n ON n.id=t.network_id
 LEFT JOIN merchant_balances m ON m.token_id=t.id LEFT JOIN payouts p ON p.token_id=t.id LEFT JOIN wallet_balances b ON b.token_id=t.id
 LEFT JOIN checks c ON c.token_id=t.id LEFT JOIN cat_currencies curr ON curr.id=t.currency_id
 WHERE (t._deleted_at IS NULL AND t.is_active=TRUE) OR COALESCE(m.amount,0)<>0 OR COALESCE(p.amount,0)<>0 OR COALESCE(b.amount,0)<>0 OR COALESCE(c.chain_amount,0)<>0 OR COALESCE(c.mismatched,0)>0
 ORDER BY t.symbol,n.name,t.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	assets := make([]dashboard.Asset, 0)
	for rows.Next() {
		var a dashboard.Asset
		if err := rows.Scan(&a.TokenID, &a.NetworkID, &a.Symbol, &a.Network, &a.NetworkColor, &a.DecimalPlaces, &a.MerchantMinor, &a.PayoutPendingMinor, &a.ObligationMinor, &a.PayoutPendingCount, &a.WalletMinor, &a.ReconciledLedgerMinor, &a.ChainMinor, &a.DeltaMinor, &a.WalletCount, &a.CheckedWalletCount, &a.MismatchedWalletCount, &a.ReconciledAt, &a.CurrencyID, &a.CurrencyCode); err != nil {
			return nil, err
		}
		assets = append(assets, a)
	}
	return assets, rows.Err()
}

func (r *Repository) Analytics(ctx context.Context, period string) (dashboard.Analytics, error) {
	days := 30
	if period == "7d" {
		days = 7
	}
	if period == "90d" {
		days = 90
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	result := dashboard.Analytics{Period: period, Assets: make([]dashboard.AnalyticsAsset, 0), Receipts: make([]dashboard.ReceiptPoint, 0)}
	var from, to time.Time
	if err := q.QueryRow(ctx, `SELECT (NOW() AT TIME ZONE 'UTC')::date-($1::int-1),(NOW() AT TIME ZONE 'UTC')::date,NOW()`, days).Scan(&from, &to, &result.ObservedAt); err != nil {
		return result, err
	}
	result.FromDay, result.ToDay = from.Format("2006-01-02"), to.Format("2006-01-02")
	start, end := from.UTC(), to.UTC().AddDate(0, 0, 1)
	rows, err := q.Query(ctx, `WITH invoices AS (
 SELECT token_id,COUNT(*) created,COUNT(*) FILTER(WHERE status IN ('completed','confirmed')) paid,
 COUNT(*) FILTER(WHERE status='underpaid') underpaid,COUNT(*) FILTER(WHERE status='expired') expired,
 COUNT(*) FILTER(WHERE status IN ('created','partially_paid')) pending,
 COUNT(*) FILTER(WHERE status='cancelled') cancelled,COUNT(*) FILTER(WHERE status='paid') awaiting_confirmation,COUNT(*) FILTER(WHERE status='reversed') reversed
 FROM doc_crypto_invoices WHERE created_at >= $1 AND created_at < $2 AND deletion_mark=FALSE AND _deleted_at IS NULL GROUP BY token_id
), receipt_tokens AS (
 SELECT DISTINCT token_id FROM doc_crypto_payments
 WHERE posted=TRUE AND status IN ('confirmed','settled') AND canonicality='canonical' AND deletion_mark=FALSE AND _deleted_at IS NULL
 AND COALESCE(confirmed_at,date) >= $1 AND COALESCE(confirmed_at,date) < $2
), fees AS (
 -- Revenue net of reversals. Treasury withdrawals spend fees already earned:
 -- they lower the withdrawable fee balance, not the revenue of any period.
 SELECT token_id,SUM(CASE WHEN record_type='receipt' THEN amount ELSE -amount END) amount
 FROM reg_crypto_fee_movements WHERE period >= $1 AND period < $2 AND fee_type='processing' AND recorder_type<>'TreasuryWithdrawal' GROUP BY token_id
)
 SELECT t.id::text,t.symbol,n.name,t.decimal_places,COALESCE(f.amount,0)::text,
 COALESCE(i.created,0),COALESCE(i.paid,0),COALESCE(i.underpaid,0),COALESCE(i.expired,0),COALESCE(i.pending,0),COALESCE(i.cancelled,0),COALESCE(i.awaiting_confirmation,0),COALESCE(i.reversed,0)
 FROM cat_tokens t JOIN cat_blockchain_networks n ON n.id=t.network_id
 LEFT JOIN invoices i ON i.token_id=t.id LEFT JOIN fees f ON f.token_id=t.id LEFT JOIN receipt_tokens rt ON rt.token_id=t.id
 WHERE (t._deleted_at IS NULL AND t.is_active=TRUE) OR i.token_id IS NOT NULL OR f.token_id IS NOT NULL OR rt.token_id IS NOT NULL
 ORDER BY t.symbol,n.name,t.id`, start, end)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var asset dashboard.AnalyticsAsset
		if err := rows.Scan(&asset.TokenID, &asset.Symbol, &asset.Network, &asset.DecimalPlaces, &asset.FeesMinor, &asset.Created, &asset.Paid, &asset.Underpaid, &asset.Expired, &asset.Pending, &asset.Cancelled, &asset.AwaitingConfirmation, &asset.Reversed); err != nil {
			rows.Close()
			return result, err
		}
		result.Assets = append(result.Assets, asset)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	rows, err = q.Query(ctx, `SELECT (COALESCE(p.confirmed_at,p.date) AT TIME ZONE 'UTC')::date,p.token_id::text,t.symbol,n.name,t.decimal_places,SUM(p.amount)::text
 FROM doc_crypto_payments p JOIN cat_tokens t ON t.id=p.token_id JOIN cat_blockchain_networks n ON n.id=t.network_id
 WHERE p.posted=TRUE AND p.status IN ('confirmed','settled') AND p.canonicality='canonical' AND p.deletion_mark=FALSE AND p._deleted_at IS NULL
 AND COALESCE(p.confirmed_at,p.date) >= $1 AND COALESCE(p.confirmed_at,p.date) < $2
 GROUP BY 1,p.token_id,t.symbol,n.name,t.decimal_places ORDER BY 1,p.token_id`, start, end)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var point dashboard.ReceiptPoint
		var day time.Time
		if err := rows.Scan(&day, &point.TokenID, &point.Symbol, &point.Network, &point.DecimalPlaces, &point.Deposits); err != nil {
			return result, err
		}
		point.Day = day.Format("2006-01-02")
		result.Receipts = append(result.Receipts, point)
	}
	return result, rows.Err()
}
