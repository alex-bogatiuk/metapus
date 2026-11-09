package readaccess_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/application/readaccess"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/security"
	"metapus/internal/domain/security_profile"
)

type registry struct{}

func (registry) Definition(key string) (readaccess.Definition, bool) {
	return readaccess.Definition{Key: "entry", Permission: "entry:read", Fields: []readaccess.Field{{Name: "code", Column: "code"}, {Name: "name", Column: "name"}}}, key == "entry"
}

type repository struct {
	read func(readaccess.Query) ([]readaccess.Record, error)
}

func (r repository) Read(_ context.Context, _ readaccess.Definition, q readaccess.Query) ([]readaccess.Record, error) {
	return r.read(q)
}

type entry struct {
	Code string `json:"code" db:"code"`
	Name string `json:"name" db:"name"`
}

func readerContext() context.Context {
	return appctx.WithUser(context.Background(), &appctx.UserContext{UserID: id.New().String(), Permissions: []string{"entry:read"}})
}
func deny(expression string) security.PolicyRule {
	return &security_profile.PolicyRule{ID: id.New(), EntityName: "entry", Expression: expression, Actions: []string{"read"}, Effect: "deny", Enabled: true}
}

func TestSearchContinuesPastDeniedCandidateBatch(t *testing.T) {
	engine, err := security.NewPolicyEngine()
	require.NoError(t, err)
	defer engine.Stop()
	calls := 0
	last := id.New()
	repo := repository{read: func(q readaccess.Query) ([]readaccess.Record, error) {
		calls++
		require.Equal(t, []string{"code"}, q.SearchColumns)
		if calls == 1 {
			result := make([]readaccess.Record, 100)
			for i := range result {
				result[i] = readaccess.Record{ID: last, Entity: &entry{Code: "blocked"}, Fields: map[string]any{"code": "blocked"}}
			}
			return result, nil
		}
		require.Equal(t, last, q.After)
		return []readaccess.Record{{ID: id.New(), Entity: &entry{Code: "visible", Name: "private"}, Fields: map[string]any{"code": "visible", "name": "private"}}}, nil
	}}
	ctx := security.WithFieldPolicies(readerContext(), map[string]*security.FieldPolicy{"entry:read": {AllowedFields: []string{"code"}}})
	ctx = security.WithPolicyRules(ctx, []security.PolicyRule{deny("doc.code == 'blocked'")})
	rows, err := readaccess.New(registry{}, repo, engine).Search(ctx, "entry", "needle", []string{"code", "name"}, 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, 2, calls)
	require.Equal(t, map[string]any{"code": "visible"}, rows[0].Fields)
}

func TestUnavailablePolicyProjectionDoesNotExecuteSQL(t *testing.T) {
	engine, err := security.NewPolicyEngine()
	require.NoError(t, err)
	defer engine.Stop()
	repo := repository{read: func(readaccess.Query) ([]readaccess.Record, error) { t.Fatal("SQL must not execute"); return nil, nil }}
	for _, expr := range []string{"doc.lines.size() == 0", "doc[user.id] == 'x'", "doc == {}", "doc['absent'] == null", "doc.name =="} {
		t.Run(expr, func(t *testing.T) {
			ctx := security.WithPolicyRules(readerContext(), []security.PolicyRule{deny(expr)})
			rows, err := readaccess.New(registry{}, repo, engine).ReadIDs(ctx, "entry", []id.ID{id.New()})
			require.NoError(t, err)
			require.Empty(t, rows)
		})
	}
}

func TestReadDeduplicatesAndPropagatesRepositoryErrors(t *testing.T) {
	failure := errors.New("storage unavailable")
	uid := id.New()
	repo := repository{read: func(q readaccess.Query) ([]readaccess.Record, error) {
		require.Equal(t, []id.ID{uid}, q.IDs)
		return nil, failure
	}}
	_, err := readaccess.New(registry{}, repo, nil).ReadIDs(readerContext(), "entry", []id.ID{uid, id.Nil(), uid})
	require.ErrorIs(t, err, failure)
	require.Contains(t, fmt.Sprint(err), "authorized read entry")
}
