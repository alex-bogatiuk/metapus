package security

import (
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

func TestReadProjectionDoesNotMutateDomainOrNestedValues(t *testing.T) {
	type line struct {
		Amount types.MinorUnits `db:"amount" json:"amount"`
		Name   string           `db:"name" json:"name"`
	}
	type model struct {
		ID         id.ID            `db:"id" json:"id"`
		Secret     *string          `db:"secret" json:"secret"`
		Amount     types.MinorUnits `db:"amount" json:"amount"`
		Lines      []line           `db:"-" json:"lines"`
		Attributes map[string]any   `db:"attributes" json:"attributes"`
	}
	secret := "private"
	original := &model{ID: id.New(), Secret: &secret, Amount: 9007199254740993,
		Lines: []line{{Amount: 99, Name: "original"}}, Attributes: map[string]any{"nested": map[string]any{"value": "original"}},
	}
	projected := ProjectForRead(original, &FieldPolicy{AllowedFields: []string{"*", "-secret"}, TableParts: map[string][]string{"lines": {"name"}}})
	require.Nil(t, projected.Secret)
	require.Equal(t, types.MinorUnits(0), projected.Lines[0].Amount)
	projected.Lines[0].Name = "changed"
	projected.Attributes["nested"].(map[string]any)["value"] = "changed"
	require.Equal(t, "private", *original.Secret)
	require.Equal(t, types.MinorUnits(99), original.Lines[0].Amount)
	require.Equal(t, "original", original.Lines[0].Name)
	require.Equal(t, "original", original.Attributes["nested"].(map[string]any)["value"])
	require.Equal(t, types.MinorUnits(9007199254740993), projected.Amount)
}
