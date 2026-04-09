package domain

import (
	"context"
	"testing"
)

func TestHookRegistry_Priority(t *testing.T) {
	registry := NewHookRegistry[string]()
	var results []string

	registry.OnWithPriority(BeforeCreate, 10, "last", func(ctx context.Context, e string) error {
		results = append(results, "last")
		return nil
	})

	registry.OnWithPriority(BeforeCreate, -10, "first", func(ctx context.Context, e string) error {
		results = append(results, "first")
		return nil
	})

	registry.On(BeforeCreate, func(ctx context.Context, e string) error {
		results = append(results, "middle")
		return nil
	})

	err := registry.Run(context.Background(), BeforeCreate, "test")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	expected := []string{"first", "middle", "last"}
	if len(results) != 3 {
		t.Fatalf("Expected 3 results, got %d", len(results))
	}

	for i, v := range expected {
		if results[i] != v {
			t.Errorf("Expected %s at index %d, got %s", v, i, results[i])
		}
	}
}
