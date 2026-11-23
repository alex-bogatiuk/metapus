package keyset

import (
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/domain/cursor"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCursorBindsCompleteSortAndExactScalar(t *testing.T) {
	spec := SortSpec{Field: "amount", FieldType: "int8", Direction: "ASC", Nullable: true, Nulls: "LAST"}
	uid := id.New()
	for _, raw := range []any{nil, "9007199254740993", "9223372036854775807", "-9223372036854775808"} {
		token, err := BuildCursorFromRow(spec, raw, uid)
		require.NoError(t, err)
		values, err := DecodeCursor(token, spec)
		require.NoError(t, err)
		require.Equal(t, raw, values[0])
		require.Equal(t, uid.String(), values[1])
		for _, change := range []func(*cursor.KeysetPayload){
			func(p *cursor.KeysetPayload) { p.Version = 1 }, func(p *cursor.KeysetPayload) { p.Direction = "DESC" },
			func(p *cursor.KeysetPayload) { p.Field = "name" }, func(p *cursor.KeysetPayload) { p.Nulls = "FIRST" },
			func(p *cursor.KeysetPayload) { p.FieldType = "numeric" }, func(p *cursor.KeysetPayload) { p.Nullable = false },
			func(p *cursor.KeysetPayload) { p.NullRank = 1 - p.NullRank }, func(p *cursor.KeysetPayload) { p.ID = "invalid" },
		} {
			payload, err := cursor.DecodeKeyset(token)
			require.NoError(t, err)
			change(&payload)
			tampered, err := cursor.EncodeKeyset(payload)
			require.NoError(t, err)
			_, err = DecodeCursor(tampered, spec)
			require.Error(t, err)
		}
	}
	for _, input := range []string{"9223372036854775808", "1.5", "1e2", ""} {
		token, err := BuildCursorFromRow(spec, input, uid)
		require.NoError(t, err)
		_, err = DecodeCursor(token, spec)
		require.Error(t, err)
	}
}

func TestNullBoundaryDoesNotUseSentinelAndInvalidValuesFailToBuild(t *testing.T) {
	for _, direction := range []string{"ASC", "DESC"} {
		spec := SortSpec{Field: "value", FieldType: "text", Direction: direction, Nullable: true, Nulls: defaultNulls(direction)}
		for _, forward := range []bool{true, false} {
			condition := TupleCondition(spec, []any{nil, id.New()}, forward)
			sql, _, err := condition.ToSql()
			require.NoError(t, err)
			require.Contains(t, sql, "IS NULL")
			require.NotContains(t, sql, "COALESCE")
		}
		require.Contains(t, spec.OrderByClause(), "NULLS "+defaultNulls(direction))
		require.NotContains(t, spec.InvertedOrderByClause(), "NULLS "+defaultNulls(direction))
	}
	_, _, err := TupleCondition(SortSpec{}, nil, true).ToSql()
	require.Error(t, err)
	_, _, err = TupleCondition(SortSpec{Field: "name", Nullable: false}, []any{nil, id.New()}, true).ToSql()
	require.Error(t, err)
}
