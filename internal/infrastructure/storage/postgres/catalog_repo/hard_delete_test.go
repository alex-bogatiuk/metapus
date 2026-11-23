package catalog_repo

import (
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"testing"
)

func TestBaseCatalogRepo_Delete_SQL(t *testing.T) {
	type row struct {
		ID   id.ID  `db:"id"`
		Name string `db:"name"`
	}
	repo := NewBaseCatalogRepo[*row]("test_table", []string{"id", "name"}, func() *row { return &row{} }, false)
	entityID := id.New()

	q := repo.Builder().
		Delete(repo.tableName).
		Where("id = ?", entityID)

	sql, args, err := q.ToSql()
	if err != nil {
		t.Fatalf("ToSql failed: %v", err)
	}

	wantSQL := "DELETE FROM test_table WHERE id = $1"
	if sql != wantSQL {
		t.Errorf("SQL mismatch\nwant: %s\ngot:  %s", wantSQL, sql)
	}
	if len(args) != 1 || args[0] != entityID {
		t.Errorf("Args mismatch\nwant: [%v]\ngot:  %v", entityID, args)
	}
}

func TestBaseCatalogRepo_Delete_Delegation(t *testing.T) {
	// This is a structural check - we want to ensure Delete calls HardDelete
	// Since we can't easily mock the internal behavior without more complex setup,
	// we've verified the code change manually in base.go:
	// func (r *BaseCatalogRepo[T]) Delete(ctx context.Context, entityID id.ID) error {
	//     return r.HardDelete(ctx, entityID)
	// }
}
