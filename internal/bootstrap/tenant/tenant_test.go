package tenant

import (
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/none"
	"metapus/internal/core/tenant"
)

func TestParseMigrateArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		args      []string
		wantID    string
		wantAll   bool
		wantError string
	}{
		{name: "all", args: []string{"--all"}, wantAll: true},
		{name: "one tenant", args: []string{"--id", "tenant-id"}, wantID: "tenant-id"},
		{name: "missing selector", wantError: "specify --id <tenant-uuid> or --all"},
		{name: "missing id value", args: []string{"--id"}, wantError: "--id requires a tenant UUID"},
		{name: "mutually exclusive", args: []string{"--all", "--id", "tenant-id"}, wantError: "--id and --all are mutually exclusive"},
		{name: "unknown option", args: []string{"--force"}, wantError: `unknown migrate option "--force"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotID, gotAll, err := parseMigrateArgs(tt.args)
			if tt.wantError != "" {
				require.EqualError(t, err, tt.wantError)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantID, gotID)
			require.Equal(t, tt.wantAll, gotAll)
		})
	}
}

func TestCanMigrateTenant(t *testing.T) {
	t.Parallel()

	for _, status := range []tenant.Status{tenant.StatusActive, tenant.StatusSuspended} {
		require.True(t, canMigrateTenant(&tenant.Tenant{Status: status}), status)
	}
	for _, status := range []tenant.Status{
		tenant.StatusProvisioning,
		tenant.StatusProvisionFailed,
		tenant.StatusDeleted,
	} {
		require.False(t, canMigrateTenant(&tenant.Tenant{Status: status}), status)
	}
}

func TestBelongsTo(t *testing.T) {
	t.Parallel()

	cfg := none.New()
	require.True(t, belongsTo(&tenant.Tenant{Configuration: ""}, cfg), "a tenant no binary recorded the configuration of")
	require.True(t, belongsTo(&tenant.Tenant{Configuration: cfg.Name()}, cfg))
	require.False(t, belongsTo(&tenant.Tenant{Configuration: "crypto"}, cfg))
}
