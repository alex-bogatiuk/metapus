package crypto_repo

import (
	"context"
	"fmt"
	"time"

	"github.com/georgysavva/scany/v2/pgxscan"
	"metapus/internal/core/apperror"
	"metapus/internal/infrastructure/storage/postgres"
)

// Journal states of the unidentified receipts list.
const (
	ReceiptJournalOpen     = "open"     // accounted, remainder not yet attributed
	ReceiptJournalCredited = "credited" // fully attributed
	ReceiptJournalOrphaned = "orphaned" // transfer left the canonical chain
	ReceiptJournalPending  = "pending"  // observed, waiting for finality or proof
)

// UnidentifiedReceiptRow is one journal line: an accounted receipt or, for the
// pending state, an observed transfer that is not accounted yet.
type UnidentifiedReceiptRow struct {
	ReceiptID         *string    `db:"receipt_id"`
	InboxEventID      *string    `db:"inbox_event_id"`
	Number            string     `db:"number"`
	ReceivedAt        time.Time  `db:"received_at"`
	NetworkID         string     `db:"network_id"`
	NetworkName       string     `db:"network_name"`
	TokenID           string     `db:"token_id"`
	TokenSymbol       string     `db:"token_symbol"`
	DecimalPlaces     int        `db:"decimal_places"`
	WalletID          string     `db:"wallet_id"`
	ToAddress         string     `db:"to_address"`
	FromAddress       string     `db:"from_address"`
	TxHash            string     `db:"tx_hash"`
	Amount            string     `db:"amount"`
	Remainder         string     `db:"remainder"`
	Reason            string     `db:"reason"`
	State             string     `db:"state"`
	CustodyMerchantID *string    `db:"custody_merchant_id"`
	InvoiceID         *string    `db:"invoice_id"`
	PendingError      string     `db:"pending_error"`
	CreditID          *string    `db:"credit_id"`
	CreditNumber      *string    `db:"credit_number"`
	CreditStatus      *string    `db:"credit_status"`
	CreditVersion     *int       `db:"credit_version"`
	CreditMerchantID  *string    `db:"credit_merchant_id"`
	CreditMerchant    *string    `db:"credit_merchant"`
	CreditNet         *string    `db:"credit_net"`
	CreditFee         *string    `db:"credit_fee"`
	CreditApproval    *bool      `db:"credit_approval"`
	CreditPreparedBy  *string    `db:"credit_prepared_by"`
	CreditDecidedAt   *time.Time `db:"credit_decided_at"`
	CreditReason      *string    `db:"credit_reason"`
}

type UnidentifiedReceiptList struct {
	Items []UnidentifiedReceiptRow
	Total int
}

type UnidentifiedReceiptRepo struct{}

func NewUnidentifiedReceiptRepo() *UnidentifiedReceiptRepo { return &UnidentifiedReceiptRepo{} }

const receiptJournalFrom = `
	FROM doc_crypto_custody_receipts d
	JOIN reg_crypto_receipt_suspense_balances s ON s.receipt_id=d.id
	JOIN reg_crypto_custody_receipt_audits a ON a.receipt_id=d.id
	WHERE CASE $1 WHEN 'open' THEN a.status='canonical' AND s.amount>0
	               WHEN 'credited' THEN a.status='canonical' AND s.amount=0
	               ELSE a.status='orphaned' END
	  AND ($2 OR d.reason_code<>'dust')
	  -- An invoice payment attributes its own receipt; it returns to the
	  -- journal only when that attribution is compensated or the chain drops it.
	  AND (d.reason_code<>'matched_receipt' OR s.amount>0 OR a.status='orphaned')`

const receiptPendingFrom = `
	FROM reg_crypto_inbound_chain_events e
	WHERE e.receipt_id IS NULL AND e._deleted_at IS NULL AND e.match_status IN ('unmatched','needs_review')
	  AND e.match_reason=ANY($1) AND e.wallet_id IS NOT NULL AND e.token_id IS NOT NULL
	  AND e.direction IN ('incoming','') AND e.canonicality<>'orphaned'
	  AND ($2 OR e.match_reason<>'dust')`

// List returns one page of the journal. Dust is accounted like any supported
// asset but hidden unless includeDust is set: it is almost always spam.
func (*UnidentifiedReceiptRepo) List(ctx context.Context, state string, includeDust bool, limit, offset int) (UnidentifiedReceiptList, error) {
	result := UnidentifiedReceiptList{Items: []UnidentifiedReceiptRow{}}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	switch state {
	case ReceiptJournalPending:
		if err := q.QueryRow(ctx, `SELECT count(*)`+receiptPendingFrom, receiptEligibleReasons, includeDust).Scan(&result.Total); err != nil {
			return result, fmt.Errorf("count pending receipts: %w", err)
		}
		err := pgxscan.Select(ctx, q, &result.Items, `
			SELECT NULL::text AS receipt_id,e.id::text AS inbox_event_id,'' AS number,e.created_at AS received_at,
			       e.network_id::text,n.name AS network_name,e.token_id::text,t.symbol AS token_symbol,t.decimal_places,
			       e.wallet_id::text,e.to_address,e.from_address,e.tx_hash,e.amount::text,e.amount::text AS remainder,
			       e.match_reason AS reason,'pending' AS state,w.custody_merchant_id::text,e.invoice_id::text,
			       e.receipt_error AS pending_error,
			       NULL::text AS credit_id,NULL::text AS credit_number,NULL::text AS credit_status,NULL::int AS credit_version,
			       NULL::text AS credit_merchant_id,NULL::text AS credit_merchant,NULL::text AS credit_net,NULL::text AS credit_fee,
			       NULL::boolean AS credit_approval,NULL::text AS credit_prepared_by,NULL::timestamptz AS credit_decided_at,
			       NULL::text AS credit_reason
			FROM (SELECT e.* `+receiptPendingFrom+` ORDER BY e.created_at,e.id LIMIT $3 OFFSET $4) e
			JOIN cat_blockchain_networks n ON n.id=e.network_id JOIN cat_tokens t ON t.id=e.token_id
			JOIN cat_wallets w ON w.id=e.wallet_id ORDER BY e.created_at,e.id`,
			receiptEligibleReasons, includeDust, limit, offset)
		if err != nil {
			return result, fmt.Errorf("list pending receipts: %w", err)
		}
		return result, nil
	case ReceiptJournalOpen, ReceiptJournalCredited, ReceiptJournalOrphaned:
	default:
		return result, apperror.NewValidation("invalid receipt journal state")
	}
	if err := q.QueryRow(ctx, `SELECT count(*)`+receiptJournalFrom, state, includeDust).Scan(&result.Total); err != nil {
		return result, fmt.Errorf("count unidentified receipts: %w", err)
	}
	err := pgxscan.Select(ctx, q, &result.Items, `
		SELECT d.id::text AS receipt_id,e.id::text AS inbox_event_id,d.number,d.date AS received_at,
		       d.network_id::text,n.name AS network_name,d.token_id::text,t.symbol AS token_symbol,t.decimal_places,
		       d.wallet_id::text,w.address AS to_address,COALESCE(v.evidence->'event'->>'fromAddress','') AS from_address,
		       v.tx_hash,d.amount::text,d.remainder::text,d.reason_code AS reason,
		       CASE WHEN d.audit_status='orphaned' THEN 'orphaned' WHEN d.remainder=0 THEN 'credited'
		            WHEN c.status='prepared' THEN 'awaiting_approval' ELSE 'unattributed' END AS state,
		       d.custody_merchant_id::text,e.invoice_id::text,'' AS pending_error,
		       c.id::text AS credit_id,c.number AS credit_number,c.status AS credit_status,c.version AS credit_version,
		       c.merchant_id::text AS credit_merchant_id,m.name AS credit_merchant,c.net_amount::text AS credit_net,
		       c.processing_fee::text AS credit_fee,c.approval_required AS credit_approval,
		       c.prepared_by::text AS credit_prepared_by,c.decided_at AS credit_decided_at,c.decision_reason AS credit_reason
		FROM (SELECT d.*,s.amount AS remainder,a.status AS audit_status `+receiptJournalFrom+`
		      ORDER BY d.date,d.id LIMIT $3 OFFSET $4) d
		JOIN reg_crypto_transfer_verifications v ON v.id=d.verification_id
		JOIN cat_blockchain_networks n ON n.id=d.network_id JOIN cat_tokens t ON t.id=d.token_id
		JOIN cat_wallets w ON w.id=d.wallet_id
		LEFT JOIN reg_crypto_inbound_chain_events e ON e.receipt_id=d.id
		LEFT JOIN LATERAL (SELECT * FROM doc_crypto_merchant_credits c WHERE c.receipt_id=d.id
		      ORDER BY c.prepared_at DESC,c.id DESC LIMIT 1) c ON TRUE
		LEFT JOIN cat_merchants m ON m.id=c.merchant_id
		ORDER BY d.date,d.id`, state, includeDust, limit, offset)
	if err != nil {
		return result, fmt.Errorf("list unidentified receipts: %w", err)
	}
	return result, nil
}
