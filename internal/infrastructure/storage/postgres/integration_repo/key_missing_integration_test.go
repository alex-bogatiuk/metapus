//go:build integration

package integration_repo

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
)

func TestSelectionReportsMissingKeyName(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	ctx, _ := fixtureDB(t)
	ar, pr := catalog_repo.NewIntegrationAccountRepo(), catalog_repo.NewIntegrationPoolRepo()
	a := integrations.NewAccount("INFURA-SELECT", "Infura")
	a.Provider = integrations.Infura
	a.PendingCredentials = &integrations.Credentials{Keys: []integrations.NamedKey{{Name: "sepolia", Value: "0123456789abcdef0123456789abcdef"}}}
	require.NoError(t, a.Validate(ctx))
	require.NoError(t, ar.Prepare(ctx, a))
	require.NoError(t, ar.Create(ctx, a))
	pool := integrations.NewPool("INFURA-SELECT", "Infura")
	pool.Provider, pool.AccountIDs = integrations.Infura, []id.ID{a.ID}
	require.NoError(t, pr.Create(ctx, pool))
	_, err := NewRuntime().Select(ctx, pool.ID, "default", integrations.Infura)
	var unavailable *integrations.PoolUnavailableError
	require.True(t, errors.As(err, &unavailable), "%v", err)
	require.Equal(t, integrations.KeyMissing, unavailable.Reason)
	selected, err := NewRuntime().Select(ctx, pool.ID, "sepolia", integrations.Infura)
	require.NoError(t, err)
	require.Equal(t, a.ID, selected.AccountID)
}
