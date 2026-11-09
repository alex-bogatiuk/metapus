package crypto_repo

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
)

type InboundReviewItem struct {
	ID              string     `json:"id"`
	InvoiceID       string     `json:"invoiceId"`
	MerchantID      string     `json:"merchantId"`
	NetworkID       string     `json:"networkId"`
	TokenID         string     `json:"tokenId"`
	TxHash          string     `json:"txHash"`
	Amount          string     `json:"amount"`
	FromAddress     string     `json:"fromAddress"`
	ToAddress       string     `json:"toAddress"`
	InvoiceStatus   string     `json:"invoiceStatus"`
	Status          string     `json:"status"`
	ReviewOutcome   string     `json:"reviewOutcome"`
	ReviewReference string     `json:"reviewReference"`
	ReviewNote      string     `json:"reviewNote"`
	ReviewedBy      string     `json:"reviewedBy"`
	CreatedAt       time.Time  `json:"createdAt"`
	ReviewedAt      *time.Time `json:"reviewedAt"`
	CanResolve      bool       `json:"canResolve"`
	MatchReason     string     `json:"matchReason"`
	CustomerID      string     `json:"customerId"`
}

type InboundReviewList struct {
	Items []InboundReviewItem `json:"items"`
	Total int                 `json:"total"`
}

type InboundReviewRepo struct{}

func NewInboundReviewRepo() *InboundReviewRepo { return &InboundReviewRepo{} }

func (r *InboundReviewRepo) List(ctx context.Context, status string, limit, offset int) (InboundReviewList, error) {
	if status != "needs_review" && status != "resolved" {
		return InboundReviewList{}, fmt.Errorf("invalid review status")
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var result InboundReviewList
	err := q.QueryRow(ctx, `
		SELECT COUNT(*) FROM reg_crypto_inbound_chain_events
		WHERE match_reason IN ('additional_single_payment','retired_deposit_address') AND match_status=$1 AND _deleted_at IS NULL
	`, status).Scan(&result.Total)
	if err != nil {
		return result, fmt.Errorf("count inbound reviews: %w", err)
	}
	result.Items = make([]InboundReviewItem, 0, limit)
	rows, err := q.Query(ctx, `
		SELECT e.id::text,COALESCE(e.invoice_id::text,''),COALESCE(i.merchant_id,w.merchant_id)::text,e.network_id::text,
		       COALESCE(e.token_id::text,''),e.tx_hash,e.amount::text,e.from_address,e.to_address,
		       COALESCE(i.status,''),e.match_status,e.review_outcome,e.review_reference,e.review_note,
	       COALESCE(e.reviewed_by::text,''),e.created_at,e.reviewed_at,
		       (e.match_status='needs_review' AND (e.match_reason='retired_deposit_address' OR i.status IN ('underpaid','completed','expired','cancelled','reversed'))),
		       e.match_reason,COALESCE(w.customer_id,i.customer_id,'')
		FROM reg_crypto_inbound_chain_events e
		LEFT JOIN doc_crypto_invoices i ON i.id=e.invoice_id
		LEFT JOIN cat_wallets w ON w.id=e.wallet_id
		WHERE e.match_reason IN ('additional_single_payment','retired_deposit_address') AND e.match_status=$1 AND e._deleted_at IS NULL
		ORDER BY e.created_at,e.id LIMIT $2 OFFSET $3
	`, status, limit, offset)
	if err != nil {
		return result, fmt.Errorf("list inbound reviews: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item InboundReviewItem
		if err := rows.Scan(&item.ID, &item.InvoiceID, &item.MerchantID, &item.NetworkID,
			&item.TokenID, &item.TxHash, &item.Amount, &item.FromAddress, &item.ToAddress,
			&item.InvoiceStatus, &item.Status, &item.ReviewOutcome, &item.ReviewReference,
			&item.ReviewNote, &item.ReviewedBy, &item.CreatedAt, &item.ReviewedAt, &item.CanResolve, &item.MatchReason, &item.CustomerID); err != nil {
			return result, fmt.Errorf("scan inbound review: %w", err)
		}
		result.Items = append(result.Items, item)
	}
	return result, rows.Err()
}

func (r *InboundReviewRepo) Resolve(ctx context.Context, eventID, actorID id.ID, outcome, reference, note string) (bool, error) {
	tag, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
		UPDATE reg_crypto_inbound_chain_events e
		SET match_status='resolved',review_outcome=$3,review_reference=$4,
		    review_note=$5,reviewed_by=$2,reviewed_at=NOW(),next_retry_at=NULL,
		    version=version+1
		WHERE e.id=$1 AND e.match_status='needs_review'
		  AND e._deleted_at IS NULL
		  AND ((e.match_reason='additional_single_payment' AND EXISTS (SELECT 1 FROM doc_crypto_invoices i WHERE i.id=e.invoice_id
		      AND i.status IN ('underpaid','completed','expired','cancelled','reversed')))
		    OR (e.match_reason='retired_deposit_address' AND ($3='not_attributable'
		      OR (e.canonicality='canonical' AND e.token_id IS NOT NULL AND
		          (e.finality_status IN ('solidified','finalized') OR (e.required_confs>0 AND e.confirmations>=e.required_confs))))))
	`, eventID, actorID, outcome, reference, note)
	if err != nil {
		return false, fmt.Errorf("resolve inbound review: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}
