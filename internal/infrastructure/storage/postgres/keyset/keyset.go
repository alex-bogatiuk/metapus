// Package keyset provides cursor-based (keyset) pagination helpers
// for building efficient SQL queries using tuple comparison.
//
// Instead of OFFSET/LIMIT, this uses WHERE (sort_col, id) > ($val, $id)
// with a composite B-Tree index for O(log N) performance at any depth.
package keyset

import (
	"fmt"
	"github.com/shopspring/decimal"
	"math"
	"metapus/internal/core/id"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/Masterminds/squirrel"

	"metapus/internal/core/apperror"
	"metapus/internal/domain/cursor"
)

// SortSpec describes the parsed ORDER BY specification.
type SortSpec struct {
	FieldType string // PostgreSQL scalar type, registered by the storage adapter
	Nullable  bool
	Nulls     string // FIRST or LAST; empty follows PostgreSQL defaults
	Field     string // e.g. "date", "name"
	Direction string // "ASC" or "DESC"
	IDField   string // optional table-qualified id, e.g. "e.id"; defaults to "id"
}

// idField returns the qualified ID column name, defaulting to "id".
func (s SortSpec) idField() string {
	if s.IDField != "" {
		return s.IDField
	}
	return "id"
}

// ParseOrderBy parses an orderBy string like "-date" or "name" into a SortSpec.
// Returns the field and direction. Validates the field against allowed columns.
func ParseOrderBy(orderBy string, defaultField string, defaultDir string, allowedCols map[string]struct{}) (SortSpec, error) {
	if strings.TrimSpace(orderBy) == "" {
		return SortSpec{Field: defaultField, Direction: defaultDir, Nulls: defaultNulls(defaultDir)}, nil
	}

	direction := "ASC"
	field := orderBy
	if strings.HasPrefix(orderBy, "-") {
		direction = "DESC"
		field = strings.TrimPrefix(orderBy, "-")
	} else if after, ok := strings.CutPrefix(orderBy, "+"); ok {
		field = after
	}

	field = strings.TrimSpace(field)
	if field == "" {
		return SortSpec{}, apperror.NewValidation("invalid orderBy").WithDetail("orderBy", orderBy)
	}

	if _, ok := allowedCols[field]; !ok {
		return SortSpec{}, apperror.NewValidation("invalid orderBy").WithDetail("orderBy", orderBy).WithDetail("field", field)
	}

	return SortSpec{Field: field, Direction: direction, Nulls: defaultNulls(direction)}, nil
}

func defaultNulls(direction string) string {
	if direction == "DESC" {
		return "FIRST"
	}
	return "LAST"
}
func (s SortSpec) nullOrder() string {
	if s.Nulls != "" {
		return s.Nulls
	}
	return defaultNulls(s.Direction)
}

func (s SortSpec) OrderByClause() string {
	return s.Field + " " + s.Direction + " NULLS " + s.nullOrder() + ", " + s.idField() + " " + s.Direction
}
func (s SortSpec) InvertedOrderByClause() string {
	direction, nulls := "ASC", "FIRST"
	if s.Direction == "ASC" {
		direction = "DESC"
	}
	if s.nullOrder() == "FIRST" {
		nulls = "LAST"
	}
	return s.Field + " " + direction + " NULLS " + nulls + ", " + s.idField() + " " + direction
}

func TupleCondition(spec SortSpec, values []any, forward bool) squirrel.Sqlizer {
	return boundaryCondition(spec, values, forward, false)
}
func TupleConditionInclusive(spec SortSpec, values []any, forward bool) squirrel.Sqlizer {
	return boundaryCondition(spec, values, forward, true)
}

type invalidCondition struct{ err error }

func (c invalidCondition) ToSql() (string, []any, error) { return "", nil, c.err }

func boundaryCondition(s SortSpec, values []any, forward, inclusive bool) squirrel.Sqlizer {
	if len(values) != 2 {
		return invalidCondition{apperror.NewValidation("malformed cursor boundary")}
	}
	op := ">"
	if (s.Direction == "DESC") == forward {
		op = "<"
	}
	equality := ""
	if inclusive {
		equality = "="
	}
	value := scalarText(values[0])
	uid := scalarText(values[1])
	if uid == nil {
		return invalidCondition{apperror.NewValidation("cursor ID is missing")}
	}
	// NULL has a separate rank, never an invented COALESCE sentinel.
	nullIsAfter := (s.nullOrder() == "LAST") == forward
	if value == nil {
		if !s.Nullable {
			return invalidCondition{apperror.NewValidation("NULL boundary for a required field")}
		}
		sameRank := squirrel.Expr(s.Field+" IS NULL AND "+s.idField()+" "+op+equality+" ?::text::uuid", *uid)
		if nullIsAfter {
			return sameRank
		}
		return squirrel.Or{squirrel.Expr(s.Field + " IS NOT NULL"), sameRank}
	}
	cast := "?"
	if s.FieldType != "" {
		if !SupportedType(s.FieldType) {
			return invalidCondition{apperror.NewValidation("unsupported cursor field type")}
		}
		cast = "?::text::" + s.FieldType
	}
	comparison := squirrel.Expr(fmt.Sprintf("(%s, %s) %s%s (%s, ?::text::uuid)", s.Field, s.idField(), op, equality, cast), *value, *uid)
	if s.Nullable && nullIsAfter {
		return squirrel.Or{comparison, squirrel.Expr(s.Field + " IS NULL")}
	}
	return comparison
}

func SupportedType(kind string) bool {
	switch kind {
	case "text", "varchar", "bpchar", "int2", "int4", "int8", "numeric", "float4", "float8", "bool", "uuid", "date", "timestamp", "timestamptz":
		return true
	}
	return false
}

func scalarText(value any) *string {
	if value == nil {
		return nil
	}
	rv := reflect.ValueOf(value)
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	var text string
	if stamp, ok := rv.Interface().(time.Time); ok {
		text = stamp.Format(time.RFC3339Nano)
	} else {
		text = fmt.Sprint(rv.Interface())
	}
	return &text
}

func nullRank(spec SortSpec, value *string) int {
	if (value == nil) == (spec.nullOrder() == "FIRST") {
		return 0
	}
	return 1
}

func BuildCursorFromRow(spec SortSpec, sortValue any, idValue any) (string, error) {
	value, uid := scalarText(sortValue), scalarText(idValue)
	if uid == nil {
		return "", apperror.NewValidation("cursor ID is missing")
	}
	if _, err := id.Parse(*uid); err != nil {
		return "", err
	}
	if value == nil && !spec.Nullable {
		return "", apperror.NewValidation("NULL boundary for a required field")
	}
	return cursor.EncodeKeyset(cursor.KeysetPayload{Version: 2, Field: spec.Field, Direction: spec.Direction, FieldType: spec.FieldType, Nullable: spec.Nullable, Nulls: spec.nullOrder(), NullRank: nullRank(spec, value), Value: value, ID: *uid})
}

func DecodeCursor(token string, spec SortSpec) ([]any, error) {
	p, err := cursor.DecodeKeyset(token)
	if err != nil {
		return nil, apperror.NewValidation("invalid cursor").WithCause(err)
	}
	if p.Field != spec.Field || p.Direction != spec.Direction || p.FieldType != spec.FieldType || p.Nullable != spec.Nullable || p.Nulls != spec.nullOrder() || p.NullRank != nullRank(spec, p.Value) {
		return nil, apperror.NewValidation("cursor sort mismatch")
	}
	if _, err := id.Parse(p.ID); err != nil {
		return nil, apperror.NewValidation("invalid cursor ID")
	}
	if p.Value == nil {
		if !spec.Nullable {
			return nil, apperror.NewValidation("NULL boundary for a required field")
		}
		return []any{nil, p.ID}, nil
	}
	if err := validateValue(spec.FieldType, *p.Value); err != nil {
		return nil, apperror.NewValidation("invalid cursor value").WithCause(err)
	}
	return []any{*p.Value, p.ID}, nil
}

func validateValue(kind, value string) error {
	switch kind {
	case "int2", "int4", "int8":
		bits := 64
		if kind == "int2" {
			bits = 16
		}
		if kind == "int4" {
			bits = 32
		}
		_, err := strconv.ParseInt(value, 10, bits)
		return err
	case "bool":
		if value != "true" && value != "false" {
			return fmt.Errorf("invalid boolean")
		}
	case "uuid":
		_, err := id.Parse(value)
		return err
	case "numeric", "float4", "float8":
		// Validate without converting arbitrary-precision NUMERIC through float64.
		if kind == "numeric" {
			n, err := decimal.NewFromString(value)
			if err != nil {
				return err
			}
			if n.Exponent() < -16383 || n.Exponent() > 131071 {
				return fmt.Errorf("numeric exponent is out of range")
			}
		} else {
			if value == "NaN" || value == "Infinity" || value == "-Infinity" {
				return nil
			}
			bits := 64
			if kind == "float4" {
				bits = 32
			}
			n, err := strconv.ParseFloat(value, bits)
			if err != nil {
				return err
			}
			if math.IsNaN(n) || math.IsInf(n, 0) {
				return fmt.Errorf("invalid floating boundary")
			}
		}
	case "date", "timestamp", "timestamptz":
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02", "2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05.999999999Z07", "2006-01-02 15:04:05.999999999Z07:00", "2006-01-02 15:04:05.999999999Z07:00:00"} {
			if _, err := time.Parse(layout, value); err == nil {
				return nil
			}
		}
		return fmt.Errorf("invalid timestamp boundary")
	}
	return nil
}
