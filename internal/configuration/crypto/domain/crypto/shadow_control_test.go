package crypto

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
)

type shadowControlRepoStub struct {
	state  ShadowControlState
	writes int
}

func (r *shadowControlRepoStub) GetShadowControl(context.Context, id.ID) (ShadowControlState, error) {
	return r.state, nil
}
func (r *shadowControlRepoStub) SetShadowPaused(_ context.Context, _ id.ID, version int, paused bool) (ShadowControlState, error) {
	if r.state.Paused != paused {
		r.state.Paused = paused
		r.state.Version++
		r.writes++
	}
	return r.state, nil
}

func TestShadowControlObservedStateAndFinancialBoundary(t *testing.T) {
	now := time.Now().UTC()
	base := ShadowControlState{Family: ChainFamilyTRON, Mode: "shadow", Version: 1, WorkerObservedAt: &now}
	tests := []struct {
		name   string
		give   ShadowControlState
		want   string
		change bool
	}{
		{name: "starting", give: base, want: "starting", change: true},
		{name: "running", give: func() ShadowControlState { s := base; s.LeaseActive = true; return s }(), want: "running", change: true},
		{name: "pausing", give: func() ShadowControlState { s := base; s.Paused = true; s.LeaseActive = true; return s }(), want: "pausing", change: true},
		{name: "paused", give: func() ShadowControlState { s := base; s.Paused = true; return s }(), want: "paused", change: true},
		{name: "worker unavailable", give: func() ShadowControlState { s := base; s.WorkerObservedAt = nil; return s }(), want: "worker_unavailable", change: true},
		{name: "activated shadow is fenced", give: func() ShadowControlState { s := base; s.ActivatedAt = &now; return s }(), want: "financial"},
		{name: "hybrid", give: func() ShadowControlState { s := base; s.Mode = "hybrid"; s.ActivatedAt = &now; return s }(), want: "financial"},
		{name: "operator paused financial", give: func() ShadowControlState { s := base; s.Mode = "paused"; s.ActivatedAt = &now; return s }(), want: "financial"},
		{name: "unconfigured", give: func() ShadowControlState { s := base; s.Version = 0; return s }(), want: "unavailable"},
		{name: "EVM", give: func() ShadowControlState { s := base; s.Family = ChainFamilyEVM; return s }(), want: "unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.give.Observed(now)
			require.Equal(t, tt.want, got.Status)
			require.Equal(t, tt.change, got.CanChange)
		})
	}
}

func TestShadowControlAuthorizationVersionAndAudit(t *testing.T) {
	network := id.New()
	ctx := tenant.WithTxManager(t.Context(), walletPoolServiceTxManager{})
	admin := appctx.WithUser(ctx, &appctx.UserContext{UserID: id.New().String(), IsAdmin: true, Email: "admin@example.test"})
	repo := &shadowControlRepoStub{state: ShadowControlState{NetworkID: network, Family: ChainFamilyTRON, Mode: "shadow", Version: 1}}
	writer := &walletPoolEventWriterStub{}
	svc := NewShadowControlAdminService(repo, writer)
	_, err := svc.SetPaused(ctx, network, 1, true, "maintenance")
	require.Error(t, err)
	require.Zero(t, repo.writes)
	security := appctx.WithUser(ctx, &appctx.UserContext{Roles: []string{"security_admin"}})
	_, err = svc.SetPaused(security, network, 1, true, "maintenance")
	require.Error(t, err)
	_, err = svc.SetPaused(admin, network, 2, true, "maintenance")
	require.Error(t, err)
	require.Zero(t, repo.writes)
	_, err = svc.SetPaused(admin, network, 1, true, "  ")
	require.Error(t, err)
	got, err := svc.SetPaused(admin, network, 1, true, "  Плановая проверка  ")
	require.NoError(t, err)
	require.True(t, got.Paused)
	require.Equal(t, 2, got.Version)
	require.Len(t, writer.events, 1)
	require.Equal(t, "Плановая проверка", writer.events[0].Details["reason"])
	_, err = svc.SetPaused(admin, network, 2, true, "already paused")
	require.NoError(t, err)
	require.Len(t, writer.events, 1)
	now := time.Now()
	repo.state.ActivatedAt = &now
	_, err = svc.SetPaused(admin, network, 2, false, "resume")
	require.Error(t, err)
	require.Equal(t, 1, repo.writes)
	writer.err = errors.New("audit unavailable")
	repo.state.ActivatedAt = nil
	_, err = svc.SetPaused(admin, network, 2, false, "resume")
	require.Error(t, err, "audit failure must fail the transactional operation")
}
