package readpolicy

import (
	"context"
	"metapus/internal/core/security"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/apperror"
)

func TestCompileReadSQLRejectsUnsupportedPolicies(t *testing.T) {
	engine, err := security.NewPolicyEngine()
	require.NoError(t, err)
	defer engine.Stop()
	fields := map[string]SQLPolicyField{"amount": {SQL: "m.amount", Kind: SQLInteger, Nullable: true}, "name": {SQL: "m.name", Kind: SQLString}, "other": {SQL: "m.other", Kind: SQLInteger}}
	for _, expression := range []string{
		"doc.name.startsWith('x')", "doc.amount + 1 > 0", "doc.amount == 1.0", "doc.amount == doc.other",
		"doc[user.id] == 1", "has(doc.name)", "now > timestamp('2000-01-01T00:00:00Z')", "doc.amount in [user.id]",
		"doc.name == 1", "(doc.amount > 0) == true", "doc.amount in doc.other", "doc.unknown == null",
	} {
		t.Run(expression, func(t *testing.T) {
			rule := makeRule(expression, expression, "deny", 0)
			rule.actions = []string{"read"}
			sql, err := CompileReadSQL(context.Background(), engine, []security.PolicyRule{rule}, fields)
			require.Nil(t, sql)
			require.Error(t, err)
			app, ok := apperror.AsAppError(err)
			require.True(t, ok)
			require.Equal(t, "REPORT_POLICY_UNSUPPORTED", app.Code)
			require.Equal(t, 403, app.HTTPStatus)
		})
	}
}

func TestCompileReadSQLParametersAndFirstMatchingRule(t *testing.T) {
	engine, err := security.NewPolicyEngine()
	require.NoError(t, err)
	defer engine.Stop()
	allow := makeRule("allow", "doc.amount > 100", "allow", 20)
	allow.actions = []string{"read"}
	deny := makeRule("deny", "doc.name == 'private'", "deny", 10)
	deny.actions = []string{"read"}
	predicate, err := CompileReadSQL(context.Background(), engine, []security.PolicyRule{deny, allow}, map[string]SQLPolicyField{
		"amount": {SQL: "m.amount", Kind: SQLInteger}, "name": {SQL: "m.name", Kind: SQLString},
	})
	require.NoError(t, err)
	sql, args, err := predicate.ToSql()
	require.NoError(t, err)
	require.Equal(t, []any{int64(100), "private"}, args)
	require.Contains(t, sql, "WHEN TRUE THEN TRUE WHEN FALSE THEN (CASE")
	require.Contains(t, sql, "?::bigint")
	require.NotContains(t, sql, "private")
	require.NotContains(t, sql, "$1")
}

type stubRule struct {
	id, expression, effect string
	priority               int
	enabled                bool
	actions                []string
}

func (r *stubRule) GetID() string         { return r.id }
func (r *stubRule) GetExpression() string { return r.expression }
func (r *stubRule) GetEffect() string     { return r.effect }
func (r *stubRule) GetPriority() int      { return r.priority }
func (r *stubRule) GetEnabled() bool      { return r.enabled }
func (r *stubRule) MatchesAction(action string) bool {
	for _, v := range r.actions {
		if v == action {
			return true
		}
	}
	return false
}
func (r *stubRule) MatchesEntity(string) bool { return true }
func makeRule(id, expression, effect string, priority int) *stubRule {
	return &stubRule{id: id, expression: expression, effect: effect, priority: priority, enabled: true}
}
