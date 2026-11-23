package viewpolicy

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	corectx "github.com/alex-bogatiuk/metapus/internal/core/context"
)

func TestMutationRightsAreIndependentOfVisibility(t *testing.T) {
	owner, stranger := uuid.New(), uuid.New()
	for _, test := range []struct {
		name, visibility string
		user             *corectx.UserContext
		allow            bool
	}{
		{"personal owner", "personal", &corectx.UserContext{UserID: owner.String()}, true},
		{"personal stranger", "personal", &corectx.UserContext{UserID: stranger.String()}, false},
		{"personal admin stranger", "personal", &corectx.UserContext{UserID: stranger.String(), IsAdmin: true}, false},
		{"shared owner", "shared", &corectx.UserContext{UserID: owner.String()}, true},
		{"shared reader", "shared", &corectx.UserContext{UserID: stranger.String()}, false},
		{"shared admin", "shared", &corectx.UserContext{UserID: stranger.String(), Roles: []string{"admin"}}, true},
		{"shared security admin", "shared", &corectx.UserContext{UserID: stranger.String(), Roles: []string{"security_admin"}}, false},
		{"system owner", "system", &corectx.UserContext{UserID: owner.String()}, false},
		{"system admin", "system", &corectx.UserContext{UserID: stranger.String(), IsAdmin: true}, true},
		{"anonymous", "shared", nil, false},
		{"invalid user", "shared", &corectx.UserContext{UserID: "invalid", IsAdmin: true}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := corectx.WithUser(context.Background(), test.user)
			_, err := RequireEdit(ctx, test.visibility, &owner)
			if test.allow {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestSystemScopeRequiresBusinessAdministrator(t *testing.T) {
	for _, user := range []*corectx.UserContext{
		{UserID: uuid.NewString()},
		{UserID: uuid.NewString(), Roles: []string{"security_admin"}},
	} {
		ctx := corectx.WithUser(context.Background(), user)
		_, err := RequireCreate(ctx, "system")
		require.Error(t, err)
		_, err = RequireCreate(ctx, "shared")
		require.NoError(t, err)
	}
}
