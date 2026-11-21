package crypto_invoice

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
)

// ReferenceRepository updates merchant reconciliation data independently of
// document posting and the payment lifecycle. A nil Metadata preserves it.
type ReferenceRepository interface {
	GetMerchantInvoiceForUpdate(context.Context, id.ID, id.ID) (*CryptoInvoice, error)
	IsPersistentTopUp(context.Context, id.ID, id.ID) (bool, error)
	UpdateMerchantReference(context.Context, ReferenceUpdate) (ReferenceUpdateResult, error)
}

type ReferenceUpdate struct {
	MerchantID id.ID
	InvoiceID  id.ID
	Version    int
	OrderID    string
	ExternalID string
	Metadata   map[string]string
}

type ReferenceUpdateResult struct {
	Version   int
	UpdatedAt time.Time
}

func ValidateOrderReference(orderID string) error {
	return validateBusinessReference(orderID, "orderId")
}

func ValidateExternalReference(externalID string) error {
	return validateBusinessReference(externalID, "externalId")
}

func validateBusinessReference(value, field string) error {
	if strings.ContainsRune(value, '\x00') {
		return apperror.NewValidation(field+" must not contain the NUL character").WithDetail("field", field)
	}
	if value == "" || strings.TrimSpace(value) != value || utf8.RuneCountInString(value) > 255 {
		return apperror.NewValidation(field+" must contain 1–255 characters without surrounding whitespace").WithDetail("field", field)
	}
	return nil
}

// ValidateMetadata is shared by document creation and reference binding.
func ValidateMetadata(metadata map[string]string) error {
	if len(metadata) > MaxMetadataPairs {
		return apperror.NewValidation("metadata must contain at most 50 entries").WithDetail("field", "metadata")
	}
	for key, value := range metadata {
		if strings.ContainsRune(key, '\x00') || strings.ContainsRune(value, '\x00') {
			return apperror.NewValidation("metadata keys and values must not contain the NUL character").WithDetail("field", "metadata")
		}
		if strings.TrimSpace(key) == "" || len(key) > MaxMetadataKey {
			return apperror.NewValidation("metadata key must be non-empty and at most 40 bytes").WithDetail("field", "metadata")
		}
		if len(value) > MaxMetadataValue {
			return apperror.NewValidation("metadata value must be at most 500 bytes").WithDetail("field", "metadata")
		}
	}
	return nil
}
