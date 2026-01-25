package postgres

import (
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type MockCatalog struct {
	entity.BaseCatalog
	Code string `db:"code" json:"code"`
	Name string `db:"name" json:"name"`
}

func TestExtractDBColumns_CDCFields(t *testing.T) {
	cols := ExtractDBColumns[MockCatalog]()

	expectedCols := []string{
		"id", "deletion_mark", "version", "attributes", "_deleted_at", "_txid", "code", "name",
	}

	for _, expected := range expectedCols {
		assert.Contains(t, cols, expected)
	}
}

func TestStructToMap_CDCFields(t *testing.T) {
	now := time.Now().UTC()
	cat := MockCatalog{
		BaseCatalog: entity.BaseCatalog{
			BaseEntity: entity.BaseEntity{
				ID:           id.New(),
				DeletionMark: true,
				Version:      5,
				CDCFields: entity.CDCFields{
					TxID:      12345,
					DeletedAt: &now,
				},
			},
		},
		Code: "TEST",
		Name: "Test Name",
	}

	m := StructToMap(cat)

	assert.Equal(t, cat.ID, m["id"])
	assert.Equal(t, true, m["deletion_mark"])
	assert.Equal(t, 5, m["version"])
	assert.Equal(t, int64(12345), m["_txid"])
	assert.Equal(t, &now, m["_deleted_at"])
	assert.Equal(t, "TEST", m["code"])
	assert.Equal(t, "Test Name", m["name"])
}
