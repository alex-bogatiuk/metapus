package tenantdb

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	tenantcore "github.com/alex-bogatiuk/metapus/internal/core/tenant"
	"github.com/alex-bogatiuk/metapus/pkg/logger"
)

type prewarmRegistry struct {
	tenantcore.Registry
	tenants      []*tenantcore.Tenant
	lookupErrors map[string]error
	listError    error
}

func (r prewarmRegistry) ListActive(context.Context) ([]*tenantcore.Tenant, error) {
	return r.tenants, r.listError
}

func (r prewarmRegistry) GetByID(_ context.Context, tenantID string) (*tenantcore.Tenant, error) {
	return nil, r.lookupErrors[tenantID]
}

func TestPrewarmPoolsPreservesErrors(t *testing.T) {
	firstError := errors.New("first tenant lookup failed")
	secondError := errors.New("second tenant lookup failed")
	listError := errors.New("registry unavailable")
	tests := []struct {
		name string
		give prewarmRegistry
		want []error
	}{
		{
			name: "all tenant failures",
			give: prewarmRegistry{
				tenants: []*tenantcore.Tenant{{ID: "first"}, {ID: "second"}},
				lookupErrors: map[string]error{
					"first":  firstError,
					"second": secondError,
				},
			},
			want: []error{firstError, secondError},
		},
		{name: "no active tenants"},
		{
			name: "registry failure",
			give: prewarmRegistry{listError: listError},
			want: []error{listError},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log, err := logger.New(logger.Config{Level: "error"})
			require.NoError(t, err)
			manager := NewManager(ManagerConfig{}, tt.give, log)
			t.Cleanup(manager.Close)

			err = manager.PrewarmPools(t.Context())
			if len(tt.want) == 0 {
				require.NoError(t, err)
				return
			}
			for _, want := range tt.want {
				require.ErrorIs(t, err, want)
			}
			for tenantID := range tt.give.lookupErrors {
				require.ErrorContains(t, err, "prewarm "+tenantID+":")
			}
		})
	}
}
