package merchant

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

type listByUserRepo struct {
	MerchantUserRepository
	assocs []MerchantUser
}

func (r listByUserRepo) ListByUser(context.Context, id.ID) ([]MerchantUser, error) {
	return r.assocs, nil
}

func TestPortalAccessScopesIssueMerchantRoles(t *testing.T) {
	owner, viewer := id.New(), id.New()
	provider := NewPortalAccessScopes(listByUserRepo{assocs: []MerchantUser{
		{MerchantID: owner, Role: MerchantRoleOwner},
		{MerchantID: viewer, Role: MerchantRoleViewer},
	}})

	grants, err := provider.AccessGrants(t.Context(), id.New())
	require.NoError(t, err)
	require.Equal(t, AccessScopeKind, provider.AccessScopeKind())
	require.Equal(t, map[string]int{owner.String(): int(PortalRoleOwner), viewer.String(): int(PortalRoleViewer)}, grants)

	empty, err := NewPortalAccessScopes(listByUserRepo{}).AccessGrants(t.Context(), id.New())
	require.NoError(t, err)
	require.Nil(t, empty)
}
