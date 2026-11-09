package publicref

import (
	"strings"
	"testing"
)

func TestNewGeneratesValidInvoiceAndPaymentLinkRefs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		create func() (string, error)
		prefix string
	}{
		{name: "invoice", create: NewInvoice, prefix: InvoicePrefix},
		{name: "payment link", create: NewPaymentLink, prefix: PaymentLinkPrefix},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ref, err := tt.create()
			if err != nil {
				t.Fatalf("create ref: %v", err)
			}
			if err := Validate(ref); err != nil {
				t.Fatalf("validate ref %q: %v", ref, err)
			}
			if !strings.HasPrefix(ref, tt.prefix+"_") {
				t.Fatalf("ref %q does not have prefix %q", ref, tt.prefix+"_")
			}
			if len(ref) != len(tt.prefix)+1+32 {
				t.Fatalf("ref %q has length %d", ref, len(ref))
			}
		})
	}
}

func TestValidateRejectsMalformedRefs(t *testing.T) {
	t.Parallel()

	for _, ref := range []string{
		"",
		"ci_",
		"ci_0f8d4c6b0d1e4a5f9b2c3d4e5f60718",
		"ci_0f8d4c6b0d1e4a5f9b2c3d4e5f60718200",
		"ci_0F8D4C6B0D1E4A5F9B2C3D4E5F607182",
		"pi_0f8d4c6b0d1e4a5f9b2c3d4e5f607182",
		"pl_0f8d4c6b0d1e4a5f9b2c3d4e5f60718z",
	} {
		t.Run(ref, func(t *testing.T) {
			t.Parallel()
			if err := Validate(ref); err == nil {
				t.Fatalf("Validate(%q) succeeded, want error", ref)
			}
		})
	}
}

func TestNewDoesNotRepeatAcrossSmallSample(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{}, 1024)
	for range 1024 {
		ref, err := NewInvoice()
		if err != nil {
			t.Fatalf("create invoice ref: %v", err)
		}
		if _, ok := seen[ref]; ok {
			t.Fatalf("duplicate public ref generated: %s", ref)
		}
		seen[ref] = struct{}{}
	}
}
