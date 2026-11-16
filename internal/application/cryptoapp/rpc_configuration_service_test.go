package cryptoapp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"metapus/internal/domain/crypto/rpcconfig"
	"metapus/internal/domain/eventlog"
)

type configurationStore struct {
	view        rpcconfig.RPCConfiguration
	transaction bool
	locked      bool
	writes      int
	lockError   error
}

func (r *configurationStore) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	before, writes := r.view, r.writes
	r.transaction = true
	defer func() { r.transaction = false; r.locked = false }()
	if err := fn(ctx); err != nil {
		r.view, r.writes = before, writes
		return err
	}
	return nil
}
func (r *configurationStore) Get(context.Context, id.ID) (rpcconfig.RPCConfiguration, error) {
	return r.view, nil
}
func (r *configurationStore) Lock(context.Context) error {
	if !r.transaction {
		return errors.New("lock outside transaction")
	}
	r.locked = true
	return r.lockError
}
func (r *configurationStore) Save(_ context.Context, _ id.ID, _ string, changes []rpcconfig.RPCConfigurationChange, settings *rpcconfig.RPCNetworkSettings) (rpcconfig.RPCConfiguration, error) {
	if !r.transaction || !r.locked {
		return rpcconfig.RPCConfiguration{}, errors.New("write without configuration lock")
	}
	if settings != nil {
		r.view.Settings = *settings
	}
	r.writes++
	r.view.Endpoints = append([]rpcconfig.RPCConfigurationEndpoint(nil), r.view.Endpoints...)
	for _, change := range changes {
		for i := range r.view.Endpoints {
			e := &r.view.Endpoints[i]
			if e.ID == change.ID {
				e.IsActive, e.Priority = change.IsActive, change.Priority
				e.Version++
			}
		}
	}
	r.view.Revision = strings.Repeat("b", 64)
	return r.view, nil
}

type configurationAudit struct {
	events []eventlog.Event
	err    error
}

func (w *configurationAudit) Write(_ context.Context, e eventlog.Event) error {
	w.events = append(w.events, e)
	return w.err
}
func (w *configurationAudit) WriteBatch(context.Context, []eventlog.Event) error { return w.err }

func TestRPCConfigurationUseCaseOwnsValidationTransactionAndAudit(t *testing.T) {
	for _, kind := range []string{"success", "invalid coverage", "noop", "audit failure", "lock failure"} {
		t.Run(kind, func(t *testing.T) {
			r := &configurationStore{view: rpcconfig.RPCConfiguration{NetworkID: id.New(), Source: "database", Revision: strings.Repeat("a", 64),
				Endpoints:    []rpcconfig.RPCConfigurationEndpoint{{ID: id.New(), Version: 1, Role: "rpc", TrustGroup: "one", IsActive: true, Priority: 10}, {ID: id.New(), Version: 1, Role: "rpc", TrustGroup: "two", IsActive: true, Priority: 20}},
				Requirements: []rpcconfig.RPCRoleRequirement{{Role: "rpc", RequiredProviders: 1, ConfiguredProviders: 2}}}}
			before := r.view
			w := &configurationAudit{}
			u := rpcconfig.RPCConfigurationUpdate{ExpectedRevision: r.view.Revision, Endpoints: []rpcconfig.RPCConfigurationChange{{ID: r.view.Endpoints[1].ID, Version: 1, IsActive: false, Priority: 20}}}
			switch kind {
			case "invalid coverage":
				r.view.Requirements[0].RequiredProviders = 2
			case "noop":
				u.Endpoints = nil
			case "audit failure":
				w.err = errors.New("audit unavailable")
			case "lock failure":
				r.lockError = errors.New("lock unavailable")
			}
			out, err := NewRPCConfigurationService(r, r, w).Update(t.Context(), r.view.NetworkID, u)
			switch kind {
			case "success":
				require.NoError(t, err)
				require.Equal(t, 1, r.writes)
				require.Len(t, w.events, 1)
				require.False(t, out.Endpoints[1].IsActive)
				require.Equal(t, 2, out.Endpoints[1].Version)
			case "noop":
				require.NoError(t, err)
				require.Equal(t, before.Revision, out.Revision)
				require.Zero(t, r.writes)
				require.Empty(t, w.events)
			default:
				require.Error(t, err)
				require.Zero(t, r.writes)
				require.Equal(t, before.Revision, r.view.Revision)
				require.True(t, r.view.Endpoints[1].IsActive)
			}
		})
	}
}
