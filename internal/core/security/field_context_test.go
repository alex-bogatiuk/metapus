package security

import (
	"context"
	"testing"
)

func TestWithFieldPolicies_AndGetFieldPolicy(t *testing.T) {
	policies := map[string]*FieldPolicy{
		"crypto_invoice:read": {
			EntityName:    "crypto_invoice",
			Action:        "read",
			AllowedFields: []string{"*", "-unit_price"},
		},
		"crypto_invoice:write": {
			EntityName:    "crypto_invoice",
			Action:        "write",
			AllowedFields: []string{},
		},
	}

	ctx := context.Background()
	ctx = WithFieldPolicies(ctx, policies)

	// GetFieldPolicy — existing policy
	p := GetFieldPolicy(ctx, "crypto_invoice", "read")
	if p == nil {
		t.Fatal("expected read policy")
		return
	}
	if p.EntityName != "crypto_invoice" {
		t.Fatalf("expected entity crypto_invoice, got %s", p.EntityName)
	}

	// GetFieldPolicy — another existing policy
	p2 := GetFieldPolicy(ctx, "crypto_invoice", "write")
	if p2 == nil {
		t.Fatal("expected write policy")
	}

	// GetFieldPolicy — non-existent
	p3 := GetFieldPolicy(ctx, "counterparty", "read")
	if p3 != nil {
		t.Fatal("expected nil for non-existent policy")
	}
}

func TestGetFieldPolicy_NoPoliciesInContext(t *testing.T) {
	ctx := context.Background()
	p := GetFieldPolicy(ctx, "crypto_invoice", "read")
	if p != nil {
		t.Fatal("expected nil when no policies in context")
	}
}

func TestWithFieldPolicies_Nil(t *testing.T) {
	ctx := context.Background()
	ctx2 := WithFieldPolicies(ctx, nil)
	// Should not panic and context should remain usable
	p := GetFieldPolicy(ctx2, "anything", "read")
	if p != nil {
		t.Fatal("expected nil")
	}
}

func TestGetFieldPolicies(t *testing.T) {
	policies := map[string]*FieldPolicy{
		"crypto_invoice:read": {
			EntityName: "crypto_invoice",
			Action:     "read",
		},
	}

	ctx := WithFieldPolicies(context.Background(), policies)
	got := GetFieldPolicies(ctx)
	if got == nil || len(got) != 1 {
		t.Fatal("expected 1 policy")
	}
}
