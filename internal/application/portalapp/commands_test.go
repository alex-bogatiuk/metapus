package portalapp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"metapus/internal/core/tx"
	"metapus/internal/domain/catalogs/merchant"
)

type commandTx struct{ committed bool }

func (m *commandTx) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	inner, completion := tx.WithCommitScope(ctx)
	defer completion.Discard()
	if err := fn(inner); err != nil {
		return err
	}
	m.committed = true
	completion.Commit(ctx)
	return nil
}

type settingsStore struct{ enabled bool }

func (s *settingsStore) GetMerchantSettings(context.Context, id.ID) (*PortalSettingsResponse, error) {
	return &PortalSettingsResponse{SinglePaymentPerInvoice: s.enabled}, nil
}
func (s *settingsStore) UpdateMerchantSettings(_ context.Context, _ id.ID, r UpdatePortalSettingsRequest) error {
	if r.SinglePaymentPerInvoice != nil {
		s.enabled = *r.SinglePaymentPerInvoice
	}
	return nil
}

type activationStub struct {
	err   error
	calls int
}

func (s *activationStub) EnableSinglePaymentForActiveInvoices(context.Context, id.ID) error {
	s.calls++
	return s.err
}

func TestSettingsActivationFailurePreventsCommit(t *testing.T) {
	failure := errors.New("invoice activation failed")
	store, activator, manager := &settingsStore{}, &activationStub{err: failure}, &commandTx{}
	enabled := true
	_, err := NewSettingsService(store, activator, manager).Update(t.Context(), id.New(), UpdatePortalSettingsRequest{SinglePaymentPerInvoice: &enabled})
	require.ErrorIs(t, err, failure)
	require.False(t, manager.committed)
	require.Equal(t, 1, activator.calls)
}

type merchantUserStore struct {
	merchant.MerchantUserRepository
}

func (merchantUserStore) Add(context.Context, id.ID, id.ID, merchant.MerchantRole) error { return nil }

type invalidatorStub struct {
	manager     *commandTx
	err         error
	invalidated bool
}

func (s *invalidatorStub) BumpUserAuthVersion(context.Context, id.ID, string) error { return s.err }
func (s *invalidatorStub) InvalidateUserAuthCache(context.Context, id.ID) {
	if !s.manager.committed {
		panic("cache invalidated before commit")
	}
	s.invalidated = true
}

func TestMerchantAccessInvalidationWaitsForOuterCommit(t *testing.T) {
	manager := &commandTx{}
	invalidator := &invalidatorStub{manager: manager}
	service := NewMerchantUserService(merchantUserStore{}, invalidator, manager)
	ctx, outer := tx.WithCommitScope(t.Context())
	// Nested managers reuse the enclosing transaction; no callbacks run yet.
	service.txm = nestedCommandTx{}
	require.NoError(t, service.Add(ctx, id.New(), id.New(), merchant.MerchantRoleOwner))
	require.False(t, invalidator.invalidated)
	outer.Discard()
	require.False(t, invalidator.invalidated)
	service.txm = manager
	require.NoError(t, service.Add(t.Context(), id.New(), id.New(), merchant.MerchantRoleOwner))
	require.True(t, invalidator.invalidated)
	manager.committed, invalidator.invalidated = false, false
	invalidator.err = errors.New("auth epoch storage failed")
	require.Error(t, service.Add(t.Context(), id.New(), id.New(), merchant.MerchantRoleOwner))
	require.False(t, manager.committed)
	require.False(t, invalidator.invalidated)
}

type nestedCommandTx struct{}

func (nestedCommandTx) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}
