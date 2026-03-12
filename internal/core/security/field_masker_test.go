package security

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
)

// testEntity is a sample struct that mimics a document for testing field masking.
type testEntity struct {
	ID             id.ID  `db:"id" json:"id"`
	Name           string `db:"name" json:"name"`
	Status         string `db:"status" json:"status"`
	OrganizationID string `db:"organization_id" json:"organizationId"`
	Amount         int    `db:"amount" json:"amount"`
}

func TestFieldMasker_MaskForRead(t *testing.T) {
	masker := NewFieldMasker()

	t.Run("nil policy — no masking", func(t *testing.T) {
		e := &testEntity{Name: "Test", Status: "active", Amount: 100}
		masker.MaskForRead(e, nil)
		assert.Equal(t, "Test", e.Name)
		assert.Equal(t, "active", e.Status)
		assert.Equal(t, 100, e.Amount)
	})

	t.Run("mask restricted fields", func(t *testing.T) {
		e := &testEntity{
			Name:           "Test",
			Status:         "active",
			OrganizationID: "org-1",
			Amount:         500,
		}
		policy := &FieldPolicy{
			AllowedFields: []string{"name", "amount"},
		}
		masker.MaskForRead(e, policy)

		assert.Equal(t, "Test", e.Name)       // allowed — kept
		assert.Equal(t, 500, e.Amount)        // allowed — kept
		assert.Equal(t, "", e.Status)         // denied — zeroed
		assert.Equal(t, "", e.OrganizationID) // denied — zeroed
	})

	t.Run("wildcard with exclusions", func(t *testing.T) {
		e := &testEntity{
			Name:           "Test",
			Status:         "active",
			OrganizationID: "org-1",
			Amount:         500,
		}
		policy := &FieldPolicy{
			AllowedFields: []string{"*", "-organization_id"},
		}
		masker.MaskForRead(e, policy)

		assert.Equal(t, "Test", e.Name)       // allowed
		assert.Equal(t, "active", e.Status)   // allowed
		assert.Equal(t, 500, e.Amount)        // allowed
		assert.Equal(t, "", e.OrganizationID) // excluded — zeroed
	})
}

// testLine mimics a document line for testing table part masking.
type testLine struct {
	LineID    string `db:"line_id" json:"lineId"`
	ProductID string `db:"product_id" json:"productId"`
	Quantity  int    `db:"quantity" json:"quantity"`
	UnitPrice int    `db:"unit_price" json:"unitPrice"`
	Amount    int    `db:"amount" json:"amount"`
}

// testDocWithLines mimics a document with a table part.
type testDocWithLines struct {
	Name   string     `db:"name" json:"name"`
	Status string     `db:"status" json:"status"`
	Amount int        `db:"amount" json:"amount"`
	Lines  []testLine `db:"-" json:"lines"`
}

func TestFieldMasker_MaskForRead_TableParts(t *testing.T) {
	masker := NewFieldMasker()

	t.Run("no TableParts in policy — lines untouched", func(t *testing.T) {
		doc := &testDocWithLines{
			Name: "Doc", Amount: 1000,
			Lines: []testLine{
				{LineID: "l1", Quantity: 10, UnitPrice: 500, Amount: 5000},
			},
		}
		policy := &FieldPolicy{AllowedFields: []string{"*"}}
		masker.MaskForRead(doc, policy)

		assert.Equal(t, 500, doc.Lines[0].UnitPrice)
		assert.Equal(t, 5000, doc.Lines[0].Amount)
	})

	t.Run("wildcard with exclusion masks line fields", func(t *testing.T) {
		doc := &testDocWithLines{
			Name: "Doc", Amount: 1000,
			Lines: []testLine{
				{LineID: "l1", ProductID: "p1", Quantity: 10, UnitPrice: 500, Amount: 5000},
				{LineID: "l2", ProductID: "p2", Quantity: 5, UnitPrice: 300, Amount: 1500},
			},
		}
		policy := &FieldPolicy{
			AllowedFields: []string{"*"},
			TableParts: map[string][]string{
				"lines": {"*", "-unit_price", "-amount"},
			},
		}
		masker.MaskForRead(doc, policy)

		// Header untouched
		assert.Equal(t, "Doc", doc.Name)
		assert.Equal(t, 1000, doc.Amount)

		// Line fields: unit_price and amount zeroed
		assert.Equal(t, "p1", doc.Lines[0].ProductID)
		assert.Equal(t, 10, doc.Lines[0].Quantity)
		assert.Equal(t, 0, doc.Lines[0].UnitPrice) // masked
		assert.Equal(t, 0, doc.Lines[0].Amount)    // masked

		assert.Equal(t, "p2", doc.Lines[1].ProductID)
		assert.Equal(t, 5, doc.Lines[1].Quantity)
		assert.Equal(t, 0, doc.Lines[1].UnitPrice) // masked
		assert.Equal(t, 0, doc.Lines[1].Amount)    // masked
	})

	t.Run("explicit inclusion list masks non-listed line fields", func(t *testing.T) {
		doc := &testDocWithLines{
			Name: "Doc",
			Lines: []testLine{
				{LineID: "l1", ProductID: "p1", Quantity: 10, UnitPrice: 500, Amount: 5000},
			},
		}
		policy := &FieldPolicy{
			AllowedFields: []string{"*"},
			TableParts: map[string][]string{
				"lines": {"line_id", "product_id", "quantity"},
			},
		}
		masker.MaskForRead(doc, policy)

		assert.Equal(t, "l1", doc.Lines[0].LineID)
		assert.Equal(t, "p1", doc.Lines[0].ProductID)
		assert.Equal(t, 10, doc.Lines[0].Quantity)
		assert.Equal(t, 0, doc.Lines[0].UnitPrice) // not in list — zeroed
		assert.Equal(t, 0, doc.Lines[0].Amount)    // not in list — zeroed
	})

	t.Run("empty lines slice — no panic", func(t *testing.T) {
		doc := &testDocWithLines{
			Name: "Doc", Lines: []testLine{},
		}
		policy := &FieldPolicy{
			AllowedFields: []string{"*"},
			TableParts:    map[string][]string{"lines": {"*", "-unit_price"}},
		}
		masker.MaskForRead(doc, policy) // should not panic
	})

	t.Run("combined header and line masking", func(t *testing.T) {
		doc := &testDocWithLines{
			Name: "Doc", Status: "posted", Amount: 1000,
			Lines: []testLine{
				{LineID: "l1", Quantity: 10, UnitPrice: 500, Amount: 5000},
			},
		}
		policy := &FieldPolicy{
			AllowedFields: []string{"*", "-amount"},
			TableParts: map[string][]string{
				"lines": {"*", "-unit_price"},
			},
		}
		masker.MaskForRead(doc, policy)

		assert.Equal(t, "Doc", doc.Name)
		assert.Equal(t, 0, doc.Amount) // header amount masked

		assert.Equal(t, 10, doc.Lines[0].Quantity)
		assert.Equal(t, 0, doc.Lines[0].UnitPrice) // line unit_price masked
		assert.Equal(t, 5000, doc.Lines[0].Amount) // line amount NOT masked (only header excluded)
	})
}

func TestFieldMasker_ValidateWrite(t *testing.T) {
	masker := NewFieldMasker()

	readOnlyPolicy := &FieldPolicy{
		AllowedFields: []string{"*", "-status", "-organization_id"},
	}

	t.Run("no changes — passes", func(t *testing.T) {
		old := &testEntity{Name: "Test", Status: "draft", OrganizationID: "org-1"}
		new := &testEntity{Name: "Test", Status: "draft", OrganizationID: "org-1"}
		err := masker.ValidateWrite(old, new, readOnlyPolicy)
		require.NoError(t, err)
	})

	t.Run("allowed field changed — passes", func(t *testing.T) {
		old := &testEntity{Name: "Old", Status: "draft"}
		new := &testEntity{Name: "New", Status: "draft"}
		err := masker.ValidateWrite(old, new, readOnlyPolicy)
		require.NoError(t, err)
	})

	t.Run("restricted field changed — blocked", func(t *testing.T) {
		old := &testEntity{Name: "Test", Status: "draft"}
		new := &testEntity{Name: "Test", Status: "posted"}
		err := masker.ValidateWrite(old, new, readOnlyPolicy)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "status")
	})

	t.Run("restricted field unchanged — passes (Approach B)", func(t *testing.T) {
		old := &testEntity{Name: "Old", Status: "draft", OrganizationID: "org-1"}
		new := &testEntity{Name: "New", Status: "draft", OrganizationID: "org-1"}
		err := masker.ValidateWrite(old, new, readOnlyPolicy)
		require.NoError(t, err)
	})

	t.Run("nil policy — no restrictions", func(t *testing.T) {
		old := &testEntity{Name: "Old", Status: "draft"}
		new := &testEntity{Name: "New", Status: "posted"}
		err := masker.ValidateWrite(old, new, nil)
		require.NoError(t, err)
	})
}
