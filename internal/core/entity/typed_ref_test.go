package entity_test

import (
	"context"
	"testing"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
)

func TestTypedRef_ValidateRef(t *testing.T) {
	ctx := context.Background()
	restricted := []string{"CashReceipt", "CashPayment"}

	t.Run("valid reference with restricted types", func(t *testing.T) {
		ref := entity.NewTypedRef("CashReceipt", id.New())
		if err := ref.ValidateRef(ctx, restricted); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	})

	t.Run("valid second restricted type", func(t *testing.T) {
		ref := entity.NewTypedRef("CashPayment", id.New())
		if err := ref.ValidateRef(ctx, restricted); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	})

	t.Run("empty ref type", func(t *testing.T) {
		ref := entity.NewTypedRef("", id.New())
		if err := ref.ValidateRef(ctx, restricted); err == nil {
			t.Fatal("expected validation error for empty type")
		}
	})

	t.Run("nil ref ID", func(t *testing.T) {
		ref := entity.NewTypedRef("CashReceipt", id.Nil())
		if err := ref.ValidateRef(ctx, restricted); err == nil {
			t.Fatal("expected validation error for nil ID")
		}
	})

	t.Run("disallowed type with restricted list", func(t *testing.T) {
		ref := entity.NewTypedRef("Invoice", id.New())
		if err := ref.ValidateRef(ctx, restricted); err == nil {
			t.Fatal("expected validation error for disallowed type")
		}
	})

	t.Run("nil allowedTypes accepts any type (universal/arbitrary)", func(t *testing.T) {
		ref := entity.NewTypedRef("AnyRandomType", id.New())
		if err := ref.ValidateRef(ctx, nil); err != nil {
			t.Fatalf("expected nil allowedTypes to accept any type, got: %v", err)
		}
	})

	t.Run("empty allowedTypes accepts any type", func(t *testing.T) {
		ref := entity.NewTypedRef("Counterparty", id.New())
		if err := ref.ValidateRef(ctx, []string{}); err != nil {
			t.Fatalf("expected empty allowedTypes to accept any type, got: %v", err)
		}
	})

	t.Run("catalog ref type with restricted catalog list", func(t *testing.T) {
		catalogTypes := []string{"Organization", "Counterparty"}
		ref := entity.NewTypedRef("Organization", id.New())
		if err := ref.ValidateRef(ctx, catalogTypes); err != nil {
			t.Fatalf("expected catalog type to be valid, got: %v", err)
		}
	})

	t.Run("mixed catalog and document types", func(t *testing.T) {
		mixedTypes := []string{"GoodsReceipt", "Counterparty", "CashPayment"}
		ref := entity.NewTypedRef("Counterparty", id.New())
		if err := ref.ValidateRef(ctx, mixedTypes); err != nil {
			t.Fatalf("expected mixed types to work, got: %v", err)
		}
	})
}

func TestTypedRef_IsRefType(t *testing.T) {
	ref := entity.NewTypedRef("CashReceipt", id.New())

	if !ref.IsRefType("CashReceipt") {
		t.Fatal("expected IsRefType to return true for matching type")
	}
	if ref.IsRefType("CashPayment") {
		t.Fatal("expected IsRefType to return false for non-matching type")
	}
}

func TestTypedRef_IsEmpty(t *testing.T) {
	t.Run("empty ref", func(t *testing.T) {
		ref := entity.TypedRef{}
		if !ref.IsEmpty() {
			t.Fatal("expected empty ref to return true")
		}
	})

	t.Run("non-empty ref", func(t *testing.T) {
		ref := entity.NewTypedRef("CashReceipt", id.New())
		if ref.IsEmpty() {
			t.Fatal("expected non-empty ref to return false")
		}
	})
}

func TestTypedRef_Getters(t *testing.T) {
	refID := id.New()
	ref := entity.NewTypedRef("GoodsReceipt", refID)

	if ref.GetRefType() != "GoodsReceipt" {
		t.Fatalf("expected GetRefType()=%q, got %q", "GoodsReceipt", ref.GetRefType())
	}
	if ref.GetRefID() != refID {
		t.Fatalf("expected GetRefID()=%v, got %v", refID, ref.GetRefID())
	}
}
