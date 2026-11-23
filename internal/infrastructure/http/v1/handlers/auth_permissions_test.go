package handlers

import (
	"testing"

	"github.com/stretchr/testify/require"

	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/domain/auth"
)

func TestEffectivePermissionsAddSystemCodesForSystemAdmins(t *testing.T) {
	granted := []string{"catalog:currency:read", auth.PermissionEventLogRead}
	for _, tc := range []struct {
		name string
		user appctx.UserContext
		want []string
	}{
		{name: "granted only", user: appctx.UserContext{Permissions: granted}, want: granted},
		{name: "nothing granted", user: appctx.UserContext{}, want: []string{}},
		{name: "security admin is not a system admin", user: appctx.UserContext{Roles: []string{"security_admin"}}, want: []string{}},
		{
			name: "admin role",
			user: appctx.UserContext{Roles: []string{"admin"}, Permissions: granted},
			want: []string{"catalog:currency:read", auth.PermissionEventLogRead, auth.PermissionSystemAdmin, auth.PermissionWorkerJobRead},
		},
		{
			name: "admin flag",
			user: appctx.UserContext{IsAdmin: true},
			want: auth.SystemPermissionCodes(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, effectivePermissions(&tc.user))
		})
	}
}

func TestEffectivePermissionsDoNotWriteIntoTheUsersSlice(t *testing.T) {
	spare := make([]string, 1, 8)
	spare[0] = "catalog:currency:read"
	user := appctx.UserContext{Roles: []string{"admin"}, Permissions: spare}
	_ = effectivePermissions(&user)
	require.Equal(t, []string{"catalog:currency:read", ""}, spare[:2])
}
