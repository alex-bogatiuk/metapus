package content

import (
	"metapus/internal/application/checkout"
	"metapus/internal/domain/crypto"
)

// Services are crypto-configuration dependencies that some document
// registrations need beyond the platform v1.DocumentDeps. A zero value is
// valid: checkout indexing is skipped and approvals have no signer provider.
type Services struct {
	// CheckoutRefs owns customer-facing public checkout references.
	CheckoutRefs *checkout.Service
	// MoneyMovementSigners signs and verifies withdrawal approval artifacts.
	MoneyMovementSigners crypto.MoneyMovementApprovalSignerProvider
}
