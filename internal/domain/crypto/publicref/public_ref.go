// Package publicref provides opaque customer-facing checkout references.
package publicref

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

const (
	InvoicePrefix     = "ci"
	PaymentLinkPrefix = "pl"

	randomBytes = 16
	hexLength   = randomBytes * 2
)

var refPattern = regexp.MustCompile(`^(ci|pl)_[a-f0-9]{32}$`)

// New generates a public reference in the form "<prefix>_<128-bit-hex>".
func New(prefix string) (string, error) {
	switch prefix {
	case InvoicePrefix, PaymentLinkPrefix:
	default:
		return "", fmt.Errorf("unsupported public ref prefix %q", prefix)
	}

	var b [randomBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate public ref entropy: %w", err)
	}
	return prefix + "_" + hex.EncodeToString(b[:]), nil
}

func NewInvoice() (string, error) {
	return New(InvoicePrefix)
}

func NewPaymentLink() (string, error) {
	return New(PaymentLinkPrefix)
}

// MustNew is intended only for constructors where returning an error would
// make the domain model unusable. crypto/rand failure is treated as fatal.
func MustNew(prefix string) string {
	ref, err := New(prefix)
	if err != nil {
		panic(err)
	}
	return ref
}

func MustNewInvoice() string {
	return MustNew(InvoicePrefix)
}

func MustNewPaymentLink() string {
	return MustNew(PaymentLinkPrefix)
}

// Validate checks the public wire format without consulting storage.
func Validate(ref string) error {
	if !refPattern.MatchString(ref) {
		return fmt.Errorf("public ref must match <ci|pl>_%d lowercase hex chars", hexLength)
	}
	return nil
}

func Prefix(ref string) (string, error) {
	if err := Validate(ref); err != nil {
		return "", err
	}
	return strings.SplitN(ref, "_", 2)[0], nil
}

// Hash returns the SHA-256 hex digest stored in meta DB. Raw refs are not
// stored there because checkout refs are bearer identifiers.
func Hash(ref string) (string, error) {
	if err := Validate(ref); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(ref))
	return hex.EncodeToString(sum[:]), nil
}

func Redact(ref string) string {
	if len(ref) <= 8 {
		return ref
	}
	return ref[:8] + "..."
}
