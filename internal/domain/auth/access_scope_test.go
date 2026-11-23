package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
)

type stubScopeProvider struct {
	kind   string
	grants map[string]int
	err    error
}

func (p stubScopeProvider) AccessScopeKind() string { return p.kind }

func (p stubScopeProvider) AccessGrants(context.Context, id.ID) (map[string]int, error) {
	return p.grants, p.err
}

func TestLoadAccessScopesCollectsProvidersBestEffort(t *testing.T) {
	s := &Service{}
	for _, option := range []ServiceOption{
		WithAccessScopeProvider(stubScopeProvider{kind: "merchant", grants: map[string]int{"m-1": 1}}),
		WithAccessScopeProvider(stubScopeProvider{kind: "broken", err: errors.New("db down")}),
		WithAccessScopeProvider(stubScopeProvider{kind: "empty"}),
		WithAccessScopeProvider(nil),
	} {
		option(s)
	}

	require.Equal(t, appctx.AccessScopes{"merchant": {"m-1": 1}}, s.loadAccessScopes(t.Context(), id.New()))
	require.Nil(t, (&Service{}).loadAccessScopes(t.Context(), id.New()))
}
