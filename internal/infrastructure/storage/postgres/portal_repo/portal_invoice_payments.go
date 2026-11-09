package portal_repo

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/application/portalapp"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

type invoicePaymentSummary struct {
	Count      int
	Fee        types.CryptoAmount
	Net        types.CryptoAmount
	Payments   []portalapp.PortalInvoicePayment
	Received   types.CryptoAmount
	Confirmed  types.CryptoAmount
	PendingNet types.CryptoAmount
}

// loadInvoicePayments reads payment snapshots once for the whole invoice page.
// The projected net amount includes only current canonical receipts; reorged,
// replaced and failed chain events remain visible in detail but do not add money.
func (r *DashboardRepo) loadInvoicePayments(ctx context.Context, invoiceIDs []id.ID, includeDetails bool) (map[string]*invoicePaymentSummary, error) {
	summaries := make(map[string]*invoicePaymentSummary, len(invoiceIDs))
	for _, invoiceID := range invoiceIDs {
		summaries[invoiceID.String()] = &invoicePaymentSummary{
			Fee: types.ZeroCryptoAmount(), Net: types.ZeroCryptoAmount(),
			Received: types.ZeroCryptoAmount(), Confirmed: types.ZeroCryptoAmount(), PendingNet: types.ZeroCryptoAmount(),
			Payments: []portalapp.PortalInvoicePayment{},
		}
	}
	if len(invoiceIDs) == 0 {
		return summaries, nil
	}

	const query = `
		SELECT id, invoice_id, amount, fee_fixed, fee_percent_bp, fee_min, fee_max,
			status, canonicality, posted, tx_hash, from_address, confirmed_at,
			created_at, confirmations, required_confs
		FROM doc_crypto_payments
		WHERE invoice_id = ANY($1) AND _deleted_at IS NULL AND deletion_mark = FALSE
		ORDER BY created_at ASC, id ASC`
	rows, err := querier(ctx).Query(ctx, query, invoiceIDs)
	if err != nil {
		return nil, fmt.Errorf("portal invoice payments: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var paymentID, invoiceID id.ID
		var amount, feeFixed, feeMin, feeMax types.CryptoAmount
		var feePercentBP int
		var status, canonicality, txHash, fromAddress string
		var posted bool
		var confirmedAt *time.Time
		var createdAt time.Time
		var confirmations, requiredConfs int
		if err := rows.Scan(
			&paymentID, &invoiceID, &amount, &feeFixed, &feePercentBP, &feeMin, &feeMax,
			&status, &canonicality, &posted, &txHash, &fromAddress, &confirmedAt,
			&createdAt, &confirmations, &requiredConfs,
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

func (s *invoicePaymentSummary) addPayment(amount, feeFixed types.CryptoAmount, feePercentBP int, feeMin, feeMax types.CryptoAmount, status, canonicality string) (types.CryptoAmount, types.CryptoAmount) {
	s.Count++
	zero := types.ZeroCryptoAmount()
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
