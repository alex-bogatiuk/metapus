package filter

import (
	"math"
	"testing"

	"github.com/Masterminds/squirrel"
	"github.com/stretchr/testify/require"

	"metapus/internal/core/json"
	"metapus/internal/domain/filter"
)

func TestBuildConditions(t *testing.T) {
	fields := map[string]filter.FieldSpec{
		"name":   {Kind: filter.NativeValue},
		"amount": {Kind: filter.MoneyValue},
	}
	items := []filter.Item{
		{Field: "name", Operator: filter.Equal, Value: "test"},
		{Field: "amount", Operator: filter.Greater, Value: "10050"},
	}

	conds, err := BuildConditions(items, fields, "test_table")
	require.NoError(t, err)
	require.Len(t, conds, 2)
}

func TestBuildSearchConditions(t *testing.T) {
	require.Nil(t, BuildSearchConditions("", []string{"name"}))
	require.Nil(t, BuildSearchConditions("foo", nil))
	require.Nil(t, BuildSearchConditions("   ", []string{"name"}))

	cond := BuildSearchConditions("красн", []string{"name", "code"})
	require.NotNil(t, cond)
	sql, args, err := cond.ToSql()
	require.NoError(t, err)
	require.Len(t, args, 2)
	for _, arg := range args {
		require.Equal(t, "%красн%", arg)
	}
	require.Contains(t, sql, "ILIKE")

	condMulti := BuildSearchConditions("красн авто", []string{"name"})
	require.NotNil(t, condMulti)
	_, argsMulti, err := condMulti.ToSql()
	require.NoError(t, err)
	require.Equal(t, []any{"%красн%", "%авто%"}, argsMulti)

	condEscape := BuildSearchConditions("100% сок", []string{"name"})
	require.NotNil(t, condEscape)
	_, argsEscape, err := condEscape.ToSql()
	require.NoError(t, err)
	require.Equal(t, `%100\%%`, argsEscape[0])
	require.Equal(t, "%сок%", argsEscape[1])

	condUnder := BuildSearchConditions("item_name", []string{"name"})
	require.NotNil(t, condUnder)
	_, argsUnder, err := condUnder.ToSql()
	require.NoError(t, err)
	require.Equal(t, `%item\_name%`, argsUnder[0])
}

func TestExactFiltersIgnoreClientStorageHints(t *testing.T) {
	for _, tc := range []struct {
		name  string
		kind  filter.ValueKind
		value any
		want  any
	}{
		{"money above JS integer range", filter.MoneyValue, "9007199254740993", int64(9007199254740993)},
		{"money minimum", filter.MoneyValue, "-9223372036854775808", int64(math.MinInt64)},
		{"money maximum", filter.MoneyValue, "9223372036854775807", int64(math.MaxInt64)},
		{"quantity minimum", filter.QuantityValue, json.Number("-922337203685477.5808"), int64(math.MinInt64)},
		{"quantity maximum", filter.QuantityValue, "922337203685477.5807", int64(math.MaxInt64)},
		{"quantity fraction", filter.QuantityValue, json.Number("0.0001"), int64(1)},
		{"ordinary text", filter.NativeValue, "123.45", "123.45"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fields := map[string]filter.FieldSpec{"amount": {Kind: tc.kind}}
			for _, hint := range []string{"", "money", "date", "number"} {
				item := filter.Item{Field: "amount", Operator: filter.Equal, FieldType: hint, Scale: math.MaxInt, Value: tc.value}
				for _, build := range []func() (squirrel.Sqlizer, error){
					func() (squirrel.Sqlizer, error) {
						items, err := BuildConditions([]filter.Item{item}, fields, "parent")
						if err != nil {
							return nil, err
						}
						return squirrel.And(items), nil
					},
					func() (squirrel.Sqlizer, error) {
						return BuildTablePartCondition(item, "parent", filter.TablePartInfo{TableName: "lines", ForeignKey: "document_id", Fields: fields}, "amount")
					},
					func() (squirrel.Sqlizer, error) {
						return BuildReferenceFieldCondition(item, "parent", filter.ReferenceFieldInfo{TableName: "linked", ForeignKey: "reference_id", Fields: fields}, "amount")
					},
				} {
					condition, err := build()
					require.NoError(t, err)
					sql, args, err := squirrel.Select("id").From("parent").Where(condition).PlaceholderFormat(squirrel.Dollar).ToSql()
					require.NoError(t, err)
					require.Equal(t, []any{tc.want}, args)
					require.Contains(t, sql, "= $1")
					require.NotContains(t, sql, "ROUND")
					require.NotContains(t, sql, "minor_multiplier")
					require.NotContains(t, sql, "DATE")
				}
			}
		})
	}
}

func TestExactFilterListsAndRelatedNegativeSemantics(t *testing.T) {
	fields := map[string]filter.FieldSpec{"amount": {Kind: filter.MoneyValue}}
	condition, err := BuildTablePartCondition(filter.Item{Field: "lines.amount", Operator: filter.NotInList, Value: []string{"1", "9007199254740993"}}, "parent", filter.TablePartInfo{TableName: "lines", ForeignKey: "document_id", Fields: fields}, "amount")
	require.NoError(t, err)
	sql, args, err := condition.ToSql()
	require.NoError(t, err)
	require.Equal(t, "NOT EXISTS (SELECT 1 FROM lines WHERE lines.document_id = parent.id AND lines.amount IN (?,?))", sql)
	require.Equal(t, []any{int64(1), int64(9007199254740993)}, args)
}

func TestResolveDeclaredFieldKindsPreserveSQL(t *testing.T) {
	tests := []struct {
		name      string
		giveKind  filter.ValueKind
		giveValue string
		wantSQL   string
		wantValue any
	}{
		{"native", filter.NativeValue, "1.25", "value = ?", "1.25"},
		{"date", filter.DateValue, "2026-09-21", "DATE(value) = ?", "2026-09-21"},
		{"quantity", filter.QuantityValue, "1.25", "value = ?", int64(12500)},
		{"money", filter.MoneyValue, "125", "value = ?", int64(125)},
		{"crypto", filter.CryptoAmountValue, "9007199254740993", "value = ?", "9007199254740993"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved, err := filter.Resolve(
				filter.Item{Field: "value", Operator: filter.Equal, Value: tt.giveValue},
				filter.FieldSpec{Kind: tt.giveKind},
			)
			require.NoError(t, err)
			condition, err := BuildSingleCondition(resolved, "value")
			require.NoError(t, err)
			sql, args, err := condition.ToSql()
			require.NoError(t, err)
			require.Equal(t, tt.wantSQL, sql)
			require.Equal(t, []any{tt.wantValue}, args)
		})
	}
}
