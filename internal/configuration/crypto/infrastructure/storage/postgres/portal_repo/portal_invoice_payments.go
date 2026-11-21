package portal_repo

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/configuration/crypto/application/portalapp"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

type invoicePaymentSummary struct {
	Count      int
	Fee        types.RawAmount
	Net        types.RawAmount
	Payments   []portalapp.PortalInvoicePayment
	Received   types.RawAmount
	Confirmed  types.RawAmount
	PendingNet types.RawAmount
}

// loadInvoicePayments reads payment snapshots once for the whole invoice page.
// The projected net amount includes only current canonical receipts; reorged,
// replaced and failed chain events remain visible in detail but do not add money.
func (r *DashboardRepo) loadInvoicePayments(ctx context.Context, invoiceIDs []id.ID, includeDetails bool) (map[string]*invoicePaymentSummary, error) {
	summaries := make(map[string]*invoicePaymentSummary, len(invoiceIDs))
	for _, invoiceID := range invoiceIDs {
		summaries[invoiceID.String()] = &invoicePaymentSummary{
			Fee: types.ZeroRawAmount(), Net: types.ZeroRawAmount(),
			Received: types.ZeroRawAmount(), Confirmed: types.ZeroRawAmount(), PendingNet: types.ZeroRawAmount(),
			Payments: []portalapp.PortalInvoicePayment{},
		}
	}
	if len(invoiceIDs) == 0 {
		return summaries, nil
	}

	// Refunds are withdrawal requests based on the payment: what they hold is
	// their net debit (a rejected or failed one restored it), what they paid
	// out is the principal their recognised payouts spent.
	const query = `
		SELECT p.id, p.invoice_id, p.amount, p.fee_fixed, p.fee_percent_bp, p.fee_min, p.fee_max,
			p.status, p.canonicality, p.posted, p.tx_hash, p.from_address, p.confirmed_at,
			p.created_at, p.confirmations, p.required_confs, refunds.committed, refunds.paid
		FROM doc_crypto_payments p
		CROSS JOIN LATERAL (
			SELECT COALESCE(SUM(x.debit),0) committed, COALESCE(SUM(x.paid),0) paid FROM (
				SELECT (SELECT COALESCE(SUM(CASE WHEN m.record_type='expense' THEN m.amount ELSE -m.amount END),0)
				          FROM reg_crypto_merchant_balance_movements m WHERE m.recorder_id=r.id AND m.recorder_type='WithdrawalRequest') debit,
				       (SELECT COALESCE(SUM(CASE WHEN m.record_type='expense' THEN m.amount ELSE -m.amount END),0)
				          FROM reg_crypto_withdrawal_custody_bindings b JOIN reg_crypto_balance_movements m
				            ON m.recorder_id=b.withdrawal_id AND m.recorder_type='CryptoWithdrawal' AND m.token_id=r.token_id
				         WHERE b.request_id=r.id) paid
				FROM doc_withdrawal_requests r WHERE r.basis_type='CryptoPayment' AND r.basis_id=p.id
			) x
		) refunds
		WHERE p.invoice_id = ANY($1) AND p._deleted_at IS NULL AND p.deletion_mark = FALSE
		ORDER BY p.created_at ASC, p.id ASC`
	rows, err := querier(ctx).Query(ctx, query, invoiceIDs)
	if err != nil {
		return nil, fmt.Errorf("portal invoice payments: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var paymentID, invoiceID id.ID
		var amount, feeFixed, feeMin, feeMax, refundCommitted, refundPaid types.RawAmount
		var feePercentBP int
		var status, canonicality, txHash, fromAddress string
		var posted bool
		var confirmedAt *time.Time
		var createdAt time.Time
		var confirmations, requiredConfs int
		if err := rows.Scan(
			&paymentID, &invoiceID, &amount, &feeFixed, &feePercentBP, &feeMin, &feeMax,
			&status, &canonicality, &posted, &txHash, &fromAddress, &confirmedAt,
			&createdAt, &confirmations, &requiredConfs, &refundCommitted, &refundPaid,
		); err != nil {
			return nil, fmt.Errorf("scan portal invoice payment: %w", err)
		}
		summary := summaries[invoiceID.String()]
		if summary == nil {
			continue
		}
		fee, net := summary.addPayment(amount, feeFixed, feePercentBP, feeMin, feeMax, status, canonicality)
		if canonicality == "canonical" {
			if status == "detected" || status == "confirming" || status == "confirmed" || status == "settled" {
				summary.Received = summary.Received.Add(amount)
			}
			if status == "confirmed" || status == "settled" {
				summary.Confirmed = summary.Confirmed.Add(amount)
			}
			if !posted {
				summary.PendingNet = summary.PendingNet.Add(net)
			}
		}
		if includeDetails {
			payment := portalapp.PortalInvoicePayment{
				ID: paymentID.String(), Amount: amount.String(), ProcessingFee: fee.String(),
				NetAmount: net.String(), Status: status, Canonicality: canonicality,
				Posted: posted, TxHash: txHash, FromAddress: fromAddress,
				CreatedAt: createdAt.Format(time.RFC3339), Confirmations: confirmations, RequiredConfs: requiredConfs,
				RefundedAmount: refundPaid.String(), RefundPendingAmount: refundCommitted.Sub(refundPaid).String(), RefundableAmount: "0",
			}
			if posted && canonicality == "canonical" && (status == "confirmed" || status == "settled") && amount.Cmp(refundCommitted) > 0 {
				payment.RefundableAmount = amount.Sub(refundCommitted).String()
			}
			if confirmedAt != nil {
				formatted := confirmedAt.Format(time.RFC3339)
				payment.ConfirmedAt = &formatted
			}
			summary.Payments = append(summary.Payments, payment)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read portal invoice payments: %w", err)
	}
	return summaries, nil
}

func (s *invoicePaymentSummary) addPayment(amount, feeFixed types.RawAmount, feePercentBP int, feeMin, feeMax types.RawAmount, status, canonicality string) (types.RawAmount, types.RawAmount) {
	s.Count++
	zero := types.ZeroRawAmount()
	if canonicality != "canonical" || (status != "detected" && status != "confirming" && status != "confirmed" && status != "settled") {
		return zero, zero
	}
	fee := calculateFee(feeFixed, feePercentBP, feeMin, feeMax, amount)
	net := zero
	if amount.Cmp(fee) > 0 {
		net = amount.Sub(fee)
	}
	s.Fee = s.Fee.Add(fee)
	s.Net = s.Net.Add(net)
	return fee, net
}

func applyPaymentSummary(item *portalapp.PortalInvoiceItem, summary *invoicePaymentSummary) {
	item.ProcessingFee = "0"
	item.NetAmount = "0"
	if summary == nil {
		return
	}
	item.PaymentCount = summary.Count
	item.ProcessingFee = summary.Fee.String()
	item.NetAmount = summary.Net.String()
}
