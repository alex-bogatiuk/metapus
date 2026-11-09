package security_profile

import (
	"context"
	"testing"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/tenant"

	"github.com/stretchr/testify/require"
)

type profileLoaderRepo struct {
	*mockRepo
	load func(context.Context, id.ID) (*SecurityProfile, error)
}

func (r *profileLoaderRepo) GetByUserID(ctx context.Context, userID id.ID) (*SecurityProfile, error) {
	return r.load(ctx, userID)
}

func TestCachedProfileProviderIsolatesTenantsAndInvalidatesUser(t *testing.T) {
	userID, otherUserID := id.New(), id.New()
	profiles := map[profileKey]*SecurityProfile{
		{tenantID: "a", userID: userID}:      {Code: "a"},
		{tenantID: "b", userID: userID}:      {Code: "b"},
		{tenantID: "a", userID: otherUserID}: {Code: "other"},
	}
	calls := make(map[profileKey]int)
	repo := &profileLoaderRepo{mockRepo: newMockRepo(), load: func(ctx context.Context, userID id.ID) (*SecurityProfile, error) {
		key := profileKey{tenantID: tenant.GetTenantID(ctx), userID: userID}
		calls[key]++
		return profiles[key], nil
	}}
	provider := NewCachedProfileProvider(repo, time.Minute, 32)
	get := func(key profileKey) *SecurityProfile {
		ctx := tenant.WithTenant(context.Background(), &tenant.Tenant{ID: key.tenantID})
		profile, err := provider.GetUserProfile(ctx, key.userID)
		require.NoError(t, err)
		return profile
	}
	for key, profile := range profiles {
		require.Same(t, profile, get(key))
		require.Same(t, profile, get(key))
		require.Equal(t, 1, calls[key])
	}
	provider.Invalidate(userID)
	for key, profile := range profiles {
		if key.userID == userID {
			profiles[key] = &SecurityProfile{Code: profile.Code + "-updated"}
		}
		require.Same(t, profiles[key], get(key))
		wantCalls := 1
		if key.userID == userID {
			wantCalls = 2
		}
		require.Equal(t, wantCalls, calls[key])
	}
	provider.InvalidateAll()
	for key := range profiles {
		before := calls[key]
		require.Same(t, profiles[key], get(key))
		require.Equal(t, before+1, calls[key])
	}
}
