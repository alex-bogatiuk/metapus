package readaccess

import (
	"context"
	"errors"
	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/security"
	"github.com/alex-bogatiuk/metapus/internal/domain/auth"
	"github.com/alex-bogatiuk/metapus/internal/domain/security_profile"
	"github.com/stretchr/testify/require"
	"testing"
)

type principalFixture struct {
	user        *auth.User
	fail        string
	permissions []string
	profile     *security_profile.SecurityProfile
}

func (f *principalFixture) GetByID(context.Context, id.ID) (*auth.User, error) {
	if f.fail == "user" {
		return nil, errors.New("database unavailable")
	}
	return f.user, nil
}
func (f *principalFixture) LoadRoles(context.Context, id.ID) ([]auth.Role, error) {
	if f.fail == "roles" {
		return nil, errors.New("database unavailable")
	}
	return []auth.Role{{Code: "reader"}}, nil
}
func (f *principalFixture) LoadPermissions(context.Context, id.ID) ([]string, error) {
	if f.fail == "permissions" {
		return nil, errors.New("database unavailable")
	}
	return f.permissions, nil
}
func (f *principalFixture) GetUserProfile(context.Context, id.ID) (*security_profile.SecurityProfile, error) {
	if f.fail == "profile" {
		return nil, errors.New("database unavailable")
	}
	return f.profile, nil
}

func TestScheduledPrincipalReloadsAuthorityAndDiscardsInheritedPolicies(t *testing.T) {
	uid := id.New()
	fixture := &principalFixture{user: &auth.User{BaseEntity: entity.BaseEntity{ID: uid}, IsActive: true}, permissions: []string{"report:document-journal:read"}}
	loader := NewPrincipalLoader(fixture, fixture)
	ctx := appctx.WithUser(context.Background(), &appctx.UserContext{UserID: id.New().String(), IsAdmin: true})
	ctx = security.WithDataScope(ctx, &security.DataScope{IsAdmin: true})
	ctx = security.WithFieldPolicies(ctx, map[string]*security.FieldPolicy{"wallet:read": {AllowedFields: []string{"*"}}})
	ctx = security.WithPolicyRules(ctx, []security.PolicyRule{&security_profile.PolicyRule{ID: id.New(), EntityName: "wallet", Actions: []string{"read"}, Expression: "true", Effect: "allow", Enabled: true}})
	first, err := loader.ContextFor(ctx, uid)
	require.NoError(t, err)
	require.Equal(t, uid.String(), appctx.GetUser(first).UserID)
	require.False(t, appctx.GetUser(first).IsAdmin)
	require.False(t, security.GetDataScope(first).IsAdmin)
	require.Nil(t, security.GetFieldPolicy(first, "token", "read"))
	require.Empty(t, security.GetPolicyRules(first))
	require.Equal(t, fixture.permissions, appctx.GetUser(first).Permissions)
	fixture.permissions = nil
	second, err := loader.ContextFor(ctx, uid)
	require.NoError(t, err)
	require.Empty(t, appctx.GetUser(second).Permissions, "each invocation sees permission revocation")
	for _, failure := range []string{"user", "roles", "permissions", "profile"} {
		fixture.fail = failure
		result, err := loader.ContextFor(ctx, uid)
		require.Error(t, err)
		require.Nil(t, result, "failed lookup must not inherit the event actor")
	}
	fixture.fail = ""
	fixture.user.IsActive = false
	_, err = loader.ContextFor(ctx, uid)
	require.Error(t, err)
	_, err = loader.ContextFor(ctx, id.Nil())
	require.Error(t, err)
}
