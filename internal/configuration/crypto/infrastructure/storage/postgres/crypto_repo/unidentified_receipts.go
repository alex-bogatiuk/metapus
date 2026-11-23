package crypto_repo

import (
	"context"
	"fmt"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/cryptoapp"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
	"github.com/georgysavva/scany/v2/pgxscan"
)

type UnidentifiedReceiptRepo struct{}

func NewUnidentifiedReceiptRepo() *UnidentifiedReceiptRepo { return &UnidentifiedReceiptRepo{} }

var _ cryptoapp.ReceiptJournalRepository = (*UnidentifiedReceiptRepo)(nil)

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

// receiptJournalColumns render one accounted receipt; d is the receipt with
// its remainder and audit status.
const receiptJournalColumns = `d.id::text AS receipt_id,e.id::text AS inbox_event_id,d.number,d.date AS received_at,
		       d.network_id::text,n.name AS network_name,d.token_id::text,t.symbol AS token_symbol,t.decimal_places,
		       d.wallet_id::text,w.address AS to_address,COALESCE(v.evidence->'event'->>'fromAddress','') AS from_address,
		       v.tx_hash,d.amount::text,d.remainder::text,d.reason_code AS reason,
		       CASE WHEN d.audit_status='orphaned' THEN 'orphaned'
		            WHEN p.id IS NOT NULL AND NOT p.posted THEN 'payment_reorged'
		            WHEN d.remainder=0 THEN 'credited'
		            WHEN c.status='prepared' OR f.status='prepared' OR rf.status IN ('pending_signature','pending_approval') THEN 'awaiting_approval' ELSE 'unattributed' END AS state,
		       d.custody_merchant_id::text,e.invoice_id::text,'' AS pending_error,
		       c.id::text AS credit_id,c.number AS credit_number,
		       CASE WHEN EXISTS(SELECT 1 FROM reg_crypto_merchant_credit_reversals cr WHERE cr.credit_id=c.id)
		            THEN 'reversed' ELSE c.status END AS credit_status,c.version AS credit_version,
		       c.merchant_id::text AS credit_merchant_id,m.name AS credit_merchant,c.net_amount::text AS credit_net,
		       c.processing_fee::text AS credit_fee,c.approval_required AS credit_approval,
		       c.prepared_by::text AS credit_prepared_by,c.decided_at AS credit_decided_at,c.decision_reason AS credit_reason,
		       p.id::text AS payment_id,p.number AS payment_number,p.status AS payment_status,
		       f.id::text AS funding_id,f.number AS funding_number,
		       CASE WHEN EXISTS(SELECT 1 FROM reg_crypto_platform_funding_reversals fr WHERE fr.funding_id=f.id)
		            THEN 'reversed' ELSE f.status END AS funding_status,f.version AS funding_version,f.reason AS funding_reason,
		       fm.name AS funding_merchant,f.merchant_link AS funding_merchant_link,f.prepared_by::text AS funding_prepared_by,
		       f.decided_at AS funding_decided_at,f.decision_reason AS funding_decision_reason,
		       tie.merchant_id::text AS tie_merchant_id,tm.name AS tie_merchant_name,tie.link AS tie_link,
		       rf.id::text AS refund_id,rf.number AS refund_number,rf.status AS refund_status,rf.amount::text AS refund_amount,
		       rf.withheld_amount::text AS refund_withheld,rf.dest_address AS refund_destination,rf.created_by::text AS refund_prepared_by`

// receiptJournalJoins attach the proof, catalogs, the latest merchant credit
// and the invoice payment of a receipt. An unposted payment that keeps its
// receipt was reversed by a reorg and blocks a manual credit.
const receiptJournalJoins = `
		JOIN reg_crypto_transfer_verifications v ON v.id=d.verification_id
		JOIN cat_blockchain_networks n ON n.id=d.network_id JOIN cat_tokens t ON t.id=d.token_id
		JOIN cat_wallets w ON w.id=d.wallet_id
		LEFT JOIN reg_crypto_inbound_chain_events e ON e.receipt_id=d.id
		LEFT JOIN LATERAL (SELECT * FROM doc_crypto_merchant_credits c WHERE c.receipt_id=d.id
		      ORDER BY c.prepared_at DESC,c.id DESC LIMIT 1) c ON TRUE
		LEFT JOIN cat_merchants m ON m.id=c.merchant_id
		LEFT JOIN LATERAL (SELECT p.id,p.number,p.status,p.posted FROM doc_crypto_payments p WHERE p.receipt_id=d.id
		      ORDER BY p.posted,p.created_at DESC,p.id DESC LIMIT 1) p ON TRUE
		LEFT JOIN LATERAL (SELECT * FROM doc_crypto_platform_fundings f WHERE f.receipt_id=d.id
		      ORDER BY f.prepared_at DESC,f.id DESC LIMIT 1) f ON TRUE
		LEFT JOIN cat_merchants fm ON fm.id=f.merchant_id
		LEFT JOIN LATERAL (SELECT * FROM doc_treasury_withdrawals r WHERE r.receipt_id=d.id
		      ORDER BY r.created_at DESC,r.id DESC LIMIT 1) rf ON TRUE
		LEFT JOIN LATERAL fn_crypto_receipt_merchant_link(d.id) tie ON TRUE
		LEFT JOIN cat_merchants tm ON tm.id=tie.merchant_id`

// List returns one page of the journal state the application validated.
func (*UnidentifiedReceiptRepo) List(ctx context.Context, state string, includeDust bool, limit, offset int) (cryptoapp.UnidentifiedReceiptList, error) {
	result := cryptoapp.UnidentifiedReceiptList{Items: []cryptoapp.UnidentifiedReceiptRow{}}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	if state == cryptoapp.ReceiptJournalPending {
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
			       NULL::text AS credit_reason,NULL::text AS payment_id,NULL::text AS payment_number,NULL::text AS payment_status,
			       NULL::text AS funding_id,NULL::text AS funding_number,NULL::text AS funding_status,NULL::int AS funding_version,
			       NULL::text AS funding_reason,NULL::text AS funding_merchant,NULL::text AS funding_merchant_link,
			       NULL::text AS funding_prepared_by,NULL::timestamptz AS funding_decided_at,NULL::text AS funding_decision_reason,
			       NULL::text AS tie_merchant_id,NULL::text AS tie_merchant_name,NULL::text AS tie_link,
			       NULL::text AS refund_id,NULL::text AS refund_number,NULL::text AS refund_status,NULL::text AS refund_amount,
			       NULL::text AS refund_withheld,NULL::text AS refund_destination,NULL::text AS refund_prepared_by
			FROM (SELECT e.* `+receiptPendingFrom+` ORDER BY e.created_at,e.id LIMIT $3 OFFSET $4) e
			JOIN cat_blockchain_networks n ON n.id=e.network_id JOIN cat_tokens t ON t.id=e.token_id
			JOIN cat_wallets w ON w.id=e.wallet_id ORDER BY e.created_at,e.id`,
			receiptEligibleReasons, includeDust, limit, offset)
		if err != nil {
			return result, fmt.Errorf("list pending receipts: %w", err)
		}
		return result, nil
	}
	if err := q.QueryRow(ctx, `SELECT count(*)`+receiptJournalFrom, state, includeDust).Scan(&result.Total); err != nil {
		return result, fmt.Errorf("count unidentified receipts: %w", err)
	}
	err := pgxscan.Select(ctx, q, &result.Items, `SELECT `+receiptJournalColumns+`
		FROM (SELECT d.*,s.amount AS remainder,a.status AS audit_status `+receiptJournalFrom+`
		      ORDER BY d.date,d.id LIMIT $3 OFFSET $4) d`+receiptJournalJoins+`
		ORDER BY d.date,d.id`, state, includeDust, limit, offset)
	if err != nil {
		return result, fmt.Errorf("list unidentified receipts: %w", err)
	}
	return result, nil
}

// Detail returns the card of one accounted receipt.
func (*UnidentifiedReceiptRepo) Detail(ctx context.Context, receiptID id.ID) (cryptoapp.ReceiptDetail, error) {
	var result cryptoapp.ReceiptDetail
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	err := pgxscan.Get(ctx, q, &result.Row, `SELECT `+receiptJournalColumns+`
		FROM (SELECT d.*,s.amount AS remainder,a.status AS audit_status FROM doc_crypto_custody_receipts d
		      JOIN reg_crypto_receipt_suspense_balances s ON s.receipt_id=d.id
		      JOIN reg_crypto_custody_receipt_audits a ON a.receipt_id=d.id WHERE d.id=$1) d`+receiptJournalJoins, receiptID)
	if pgxscan.NotFound(err) {
		return result, apperror.NewNotFound("custody receipt", receiptID)
	}
	if err != nil {
		return result, fmt.Errorf("read custody receipt: %w", err)
	}
	var sources []byte
	if err := q.QueryRow(ctx, `SELECT a.status,a.last_checked_at,a.next_check_at,a.attempts,a.last_error,a.orphaned_at,a.incident_id::text,
		       v.id::text,a.anchor_verification_id IS NULL,v.tx_hash,v.event_kind,v.event_index,v.block_number,v.block_hash,
		       v.confirmations,v.verified_at,COALESCE(v.evidence->'sources','[]'::jsonb)
		FROM reg_crypto_custody_receipt_audits a JOIN doc_crypto_custody_receipts d ON d.id=a.receipt_id
		JOIN reg_crypto_transfer_verifications v ON v.id=COALESCE(a.anchor_verification_id,d.verification_id)
		WHERE a.receipt_id=$1`, receiptID).Scan(&result.Audit.Status, &result.Audit.LastCheckedAt, &result.Audit.NextCheckAt,
		&result.Audit.Attempts, &result.Audit.LastError, &result.Audit.OrphanedAt, &result.Audit.IncidentID,
		&result.Proof.VerificationID, &result.Proof.Original, &result.Proof.TxHash, &result.Proof.EventKind, &result.Proof.EventIndex,
		&result.Proof.BlockNumber, &result.Proof.BlockHash, &result.Proof.Confirmations, &result.Proof.VerifiedAt, &sources); err != nil {
		return result, fmt.Errorf("read custody receipt proof: %w", err)
	}
	if err := json.Unmarshal(sources, &result.Proof.Sources); err != nil {
		return result, fmt.Errorf("decode custody receipt proof sources: %w", err)
	}
	result.Attributions = []cryptoapp.ReceiptAttribution{}
	// Outstanding = what the document consumed from the receipt minus the
	// reversals (reorg compensation, payment_reversal, the storno of a manual
	// credit at orphaning) returned to it. A reversed credit stays posted.
	err = pgxscan.Select(ctx, q, &result.Attributions, `
		WITH consumed AS (
			SELECT m.recorder_id,SUM(m.amount-COALESCE(back.amount,0)) AS amount
			FROM reg_crypto_receipt_suspense_movements m
			LEFT JOIN LATERAL (SELECT SUM(r.amount) AS amount FROM reg_crypto_receipt_suspense_movements r
			     WHERE r.reversal_of_line_id=m.line_id) back ON TRUE
			WHERE m.receipt_id=$1 AND m.record_type='expense' AND m.reversal_of_line_id IS NULL
			GROUP BY m.recorder_id)
		SELECT 'CryptoPayment' AS document_type,p.id::text AS document_id,p.number,p.date,p.status,p.posted,
		       p.merchant_id::text,m.name AS merchant_name,p.amount::text,'' AS net_amount,'' AS processing_fee,
		       COALESCE(c.amount,0)::text AS outstanding,p.fee_fixed,p.fee_percent_bp,p.fee_min,p.fee_max
		FROM doc_crypto_payments p JOIN cat_merchants m ON m.id=p.merchant_id
		LEFT JOIN consumed c ON c.recorder_id=p.id
		WHERE p.receipt_id=$1
		UNION ALL
		SELECT 'CryptoMerchantCredit',k.id::text,k.number,k.date,
		       CASE WHEN EXISTS(SELECT 1 FROM reg_crypto_merchant_credit_reversals r WHERE r.credit_id=k.id)
		            THEN 'reversed' ELSE k.status END,k.posted,k.merchant_id::text,m.name,
		       k.amount::text,k.net_amount::text,k.processing_fee::text,COALESCE(c.amount,0)::text,0,0,0,0
		FROM doc_crypto_merchant_credits k JOIN cat_merchants m ON m.id=k.merchant_id
		LEFT JOIN consumed c ON c.recorder_id=k.id
		WHERE k.receipt_id=$1
		UNION ALL
		SELECT 'CryptoPlatformFunding',f.id::text,f.number,f.date,
		       CASE WHEN EXISTS(SELECT 1 FROM reg_crypto_platform_funding_reversals r WHERE r.funding_id=f.id)
		            THEN 'reversed' ELSE f.status END,f.posted,'','',
		       f.amount::text,'','',COALESCE(c.amount,0)::text,0,0,0,0
		FROM doc_crypto_platform_fundings f
		LEFT JOIN consumed c ON c.recorder_id=f.id
		WHERE f.receipt_id=$1
		ORDER BY date,document_id`, receiptID)
	if err != nil {
		return result, fmt.Errorf("list custody receipt attributions: %w", err)
	}
	return result, nil
}
