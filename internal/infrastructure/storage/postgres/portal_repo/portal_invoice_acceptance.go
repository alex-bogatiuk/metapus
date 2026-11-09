package portal_repo

import "time"

func invoicePaymentAcceptanceReason(status string, accepts, single, selected bool, expiresAt, observedAt time.Time) string {
	if accepts {
		return "open"
	}
	switch status {
	case "completed":
		return "completed"
	case "expired":
		return "expired"
	case "cancelled":
		return "cancelled"
	case "reversed":
		return "reversed"
	case "paid":
		return "awaiting_confirmation"
	case "confirmed":
		return "awaiting_credit"
	case "underpaid":
		if single && selected {
			return "single_payment_selected"
		}
		if !expiresAt.IsZero() && !expiresAt.After(observedAt) {
			return "expired"
		}
	case "created", "partially_paid":
		if single && selected {
			return "single_payment_selected"
		}
	}
	return "closed"
}
