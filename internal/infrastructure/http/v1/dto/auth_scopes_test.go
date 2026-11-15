package dto

import (
	"testing"

	"github.com/stretchr/testify/require"

	appctx "metapus/internal/core/context"
	"metapus/internal/domain/auth"
)

func TestUserResponseListsAccessScopeIDsWithoutLevels(t *testing.T) {
	user := &auth.User{AccessScopes: appctx.AccessScopes{
		"merchant": {"m-2": 3, "m-1": 1},
		"empty":    {},
	}}
	require.Equal(t, map[string][]string{"merchant": {"m-1", "m-2"}}, FromUser(user).AccessScopes)
	require.Nil(t, FromUser(&auth.User{}).AccessScopes)
}
