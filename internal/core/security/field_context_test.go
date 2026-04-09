package security

import (
	"context"
	"testing"
)

func TestWithFieldPolicies_AndGetFieldPolicy(t *testing.T) {
	policies := map[string]*FieldPolicy{
		"goods_receipt:read": {
			EntityName:    "goods_receipt",
			Action:        "read",
			AllowedFields: []string{"*", "-unit_price"},
		},
		"goods_receipt:write": {
			EntityName:    "goods_receipt",
			Action:        "write",
			AllowedFields: []string{},
		},
	}

	ctx := context.Background()
	ctx = WithFieldPolicies(ctx, policies)

	// GetFieldPolicy — existing policy
	p := GetFieldPolicy(ctx, "goods_receipt", "read")
	if p == nil {
		t.Fatal("expected read policy")
	}
	if p.EntityName != "goods_receipt" {
		t.Fatalf("expected entity goods_receipt, got %s", p.EntityName)
	}

	// GetFieldPolicy — another existing policy
	p2 := GetFieldPolicy(ctx, "goods_receipt", "write")
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
	p := GetFieldPolicy(ctx, "goods_receipt", "read")
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
		"goods_receipt:read": {
			EntityName: "goods_receipt",
			Action:     "read",
		},
	}

	ctx := WithFieldPolicies(context.Background(), policies)
	got := GetFieldPolicies(ctx)
	if got == nil || len(got) != 1 {
		t.Fatal("expected 1 policy")
	}
}
