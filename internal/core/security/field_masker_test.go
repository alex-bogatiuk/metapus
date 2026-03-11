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

		assert.Equal(t, "Test", e.Name)   // allowed — kept
		assert.Equal(t, 500, e.Amount)     // allowed — kept
		assert.Equal(t, "", e.Status)      // denied — zeroed
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

		assert.Equal(t, "Test", e.Name)        // allowed
		assert.Equal(t, "active", e.Status)     // allowed
		assert.Equal(t, 500, e.Amount)          // allowed
		assert.Equal(t, "", e.OrganizationID)   // excluded — zeroed
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
