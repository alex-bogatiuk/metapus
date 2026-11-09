//go:build integration

package catalog_repo_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/domain"
	"metapus/internal/domain/cursor"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
	"metapus/internal/infrastructure/storage/postgres/document_repo"
	"metapus/internal/infrastructure/storage/postgres/testsupport"
)

type nullableCursorRow struct {
	ID           id.ID      `db:"id"`
	Name         *string    `db:"name"`
	Score        *int64     `db:"score"`
	Stamp        *time.Time `db:"stamp"`
	Ref          id.ID      `db:"ref"` // Model's zero UUID must remain distinct from SQL NULL.
	DeletionMark bool       `db:"deletion_mark"`
}

func (r *nullableCursorRow) GetID() id.ID { return r.ID }

func TestNullableKeysetEveryDirectionAndExactBoundary(t *testing.T) {
	tenant := testsupport.NewSecurityTenants(t)[0]
	ctx := tenant.Context(appctx.UserContext{})
	_, err := tenant.Pool.Exec(ctx, `CREATE TABLE keyset_cases(id uuid PRIMARY KEY,name text,score bigint,stamp timestamptz,ref uuid,deletion_mark boolean NOT NULL DEFAULT false)`)
	require.NoError(t, err)
	ref := id.New()
	for index, score := range []any{nil, int64(-9223372036854775808), int64(9007199254740993), nil, int64(0), int64(0), int64(9223372036854775807), nil, int64(9007199254740992), int64(1), nil, int64(-1), int64(2)} {
		name, stamp, reference := any(nil), any(nil), any(nil)
		if index%3 != 0 {
			name = []string{"", "Я", "a", "a"}[index%4]
			stamp = time.Date(2026, 1, 1+index%3, 0, 0, 0, 123456000, time.UTC)
			reference = ref
		}
		if index == 2 {
			reference = id.Nil()
		}
		_, err := tenant.Pool.Exec(ctx, `INSERT INTO keyset_cases(id,name,score,stamp,ref) VALUES($1,$2,$3,$4,$5)`, id.New(), name, score, stamp, reference)
		require.NoError(t, err)
	}
	repo := catalog_repo.NewBaseCatalogRepo("keyset_cases", []string{"id", "name", "score", "stamp", "ref", "deletion_mark"}, func() *nullableCursorRow { return &nullableCursorRow{} }, false)
	ids := func(items []*nullableCursorRow) []id.ID {
		result := make([]id.ID, len(items))
		for i, row := range items {
			result[i] = row.ID
		}
		return result
	}
	docRepo := document_repo.NewBaseDocumentRepo("keyset_cases", []string{"id", "name", "score", "stamp", "ref", "deletion_mark"}, func() *nullableCursorRow { return &nullableCursorRow{} })
	for repoName, list := range map[string]func(context.Context, domain.ListFilter) (domain.CursorListResult[*nullableCursorRow], error){"catalog": repo.List, "document": docRepo.List} {
		for _, field := range []string{"name", "score", "stamp", "ref"} {
			for _, direction := range []string{"ASC", "DESC"} {
				t.Run(repoName+"/"+field+"/"+direction, func(t *testing.T) {
					order := field
					if direction == "DESC" {
						order = "-" + field
					}
					rows, err := tenant.Pool.Query(ctx, fmt.Sprintf("SELECT id FROM keyset_cases ORDER BY %s %s,id %s", field, direction, direction))
					require.NoError(t, err)
					var expected []id.ID
					for rows.Next() {
						var uid id.ID
						require.NoError(t, rows.Scan(&uid))
						expected = append(expected, uid)
					}
					require.NoError(t, rows.Err())
					rows.Close()
					filter := domain.ListFilter{OrderBy: order, Limit: 3}
					page, err := list(ctx, filter)
					require.NoError(t, err)
					all := ids(page.Items)
					for step := 0; page.HasMore; step++ {
						require.Less(t, step, 10, "pagination must terminate")
						require.NotEmpty(t, page.NextCursor)
						filter.CursorReq = &cursor.Request{Direction: cursor.DirAfter, Token: page.NextCursor}
						page, err = list(ctx, filter)
						require.NoError(t, err)
						all = append(all, ids(page.Items)...)
					}
					require.Equal(t, expected, all, "every row, including NULL, appears exactly once")
					backward := ids(page.Items)
					for step := 0; page.HasPrev; step++ {
						require.Less(t, step, 10)
						filter.CursorReq = &cursor.Request{Direction: cursor.DirBefore, Token: page.PrevCursor}
						page, err = list(ctx, filter)
						require.NoError(t, err)
						backward = append(ids(page.Items), backward...)
					}
					require.Equal(t, expected, backward)
					changed := filter
					changed.OrderBy = "-" + field
					if direction == "DESC" {
						changed.OrderBy = field
					}
					changed.CursorReq = &cursor.Request{Direction: cursor.DirAfter, Token: page.NextCursor}
					_, err = list(ctx, changed)
					require.ErrorContains(t, err, "cursor sort mismatch")
					for index, uid := range expected {
						filter.Limit = 4
						filter.CursorReq = &cursor.Request{Direction: cursor.DirAround, TargetID: uid.String()}
						window, err := list(ctx, filter)
						require.NoError(t, err)
						start, end := max(0, index-1), min(len(expected), index+3)
						require.Equal(t, expected[start:end], ids(window.Items))
						require.NotNil(t, window.TargetIndex)
						require.Equal(t, uid, window.Items[*window.TargetIndex].ID)
					}
				})
			}
		}
	}
}
