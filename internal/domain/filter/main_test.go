package filter_test

import (
"fmt"
"testing"
"github.com/Masterminds/squirrel"
)

func TestSquirrelDate(t *testing.T) {
sql, args, _ := squirrel.Eq{"DATE(foo)": "2026-01-01"}.ToSql()
fmt.Println(sql, args)
sql, args, _ = squirrel.Lt{"DATE(foo)": "2026-01-01"}.ToSql()
fmt.Println(sql, args)
}
