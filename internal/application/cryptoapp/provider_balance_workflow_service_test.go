package cryptoapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/application/command"
	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	pbo "metapus/internal/domain/documents/crypto_provider_balance_operation"
)

func TestProviderOpeningRejectsMissingMaker(t *testing.T) {
	doc := &pbo.CryptoProviderBalanceOperation{Document: entity.NewDocument(), Status: pbo.StatusPendingApproval}
	require.Error(t, doc.CheckDecision(pbo.DecisionAuthorization{ActorID: id.New(), Mode: pbo.DecisionAdminSelf, PolicyVersion: 1}, doc.Version, "checked"), "a zero/missing maker must never pass maker-checker")
}

type testOpeningAuthority struct {
	basis  string
	err    error
	locked int
}

func (a *testOpeningAuthority) ReadSystemAdmin(context.Context, id.ID) (ProviderOpeningAdminAuthority, error) {
	return ProviderOpeningAdminAuthority{Basis: a.basis, AuthVersion: 1}, a.err
}
func (a *testOpeningAuthority) LockSystemAdmin(ctx context.Context, actor id.ID) (ProviderOpeningAdminAuthority, error) {
	a.locked++
	return a.ReadSystemAdmin(ctx, actor)
}

func TestProviderOpeningCurrentAuthorityAndReadProjection(t *testing.T) {
	for _, role := range []string{"admin_flag", "admin", "security_admin", "operator", "creator"} {
		for _, action := range []string{"approve", "reject"} {
			for _, self := range []bool{true, false} {
				actor := id.New()
				doc := &pbo.CryptoProviderBalanceOperation{Document: entity.NewDocument(), Status: pbo.StatusPendingApproval}
				doc.CreatedBy = id.New()
				if self {
					doc.CreatedBy = actor
				}
				user := &appctx.UserContext{UserID: actor.String(), IsAdmin: role == "admin_flag", Roles: []string{role}}
				user.Permissions = []string{"document:crypto_provider_balance_operation:create"}
				if role != "creator" {
					user.Permissions = append(user.Permissions, "document:crypto_provider_balance_operation:"+action)
				}
				ctx := appctx.WithUser(context.Background(), user)
				admin := role == "admin_flag" || role == "admin"
				authority := &testOpeningAuthority{}
				if admin {
					authority.basis = "system_admin_role"
				}
				s := &ProviderBalanceWorkflowService{authority: authority}
				_, _, err := s.authorizeDecision(ctx, doc, actor, action)
				allowed := admin || (!self && role != "creator")
				require.Equal(t, allowed, err == nil, "role=%s self=%t action=%s", role, self, action)
				states, err := s.ActionAvailability(ctx, doc)
				require.NoError(t, err)
				require.Equal(t, allowed, states[action].Allowed)
				if admin && self {
					require.Equal(t, 1, authority.locked)
					authority.basis = ""
					_, _, err = s.authorizeDecision(ctx, doc, actor, action)
					require.Error(t, err, "stale admin claims must not authorize self decision")
					authority.err = errors.New("database unavailable")
					_, _, err = s.authorizeDecision(ctx, doc, actor, action)
					require.Error(t, err)
				}
			}
		}
	}
}

type providerBusyCommandRepo struct{ command.Repository }

func (providerBusyCommandRepo) Acquire(context.Context, command.Metadata) (command.AcquireResult, error) {
	return command.AcquireResult{}, command.ErrInProgress
}

func TestProviderOpeningLockTimeoutReturnsRetryAfter(t *testing.T) {
	user := &appctx.UserContext{UserID: id.New().String(), SessionID: id.New().String(), TenantID: id.New().String(), IsAdmin: true}
	s := &ProviderBalanceWorkflowService{
		now: time.Now, commands: command.NewExecutor(&immediateTxManager{}, providerBusyCommandRepo{}),
		stepUp: providerStepUpCheck(func(context.Context, id.ID, id.ID, string, string, time.Time) (bool, error) { return true, nil }),
	}
	_, err := s.PrepareOpening(appctx.WithUser(context.Background(), user), PrepareProviderOpeningCommand{
		IncidentID: id.New(), ObservationID: id.New(), Reason: "verified", IdempotencyKey: "busy",
	})
	var appErr *apperror.AppError
	require.True(t, errors.As(err, &appErr))
	require.Equal(t, "IDEMPOTENCY_IN_PROGRESS", appErr.Code)
	require.Equal(t, 3, appErr.Details["retryAfterSeconds"])
}

type providerStepUpCheck func(context.Context, id.ID, id.ID, string, string, time.Time) (bool, error)

func (f providerStepUpCheck) HasFreshStepUp(ctx context.Context, session, actor id.ID, tenant, action string, now time.Time) (bool, error) {
	return f(ctx, session, actor, tenant, action, now)
}

func TestProviderOpeningSecurityBoundary(t *testing.T) {
	req := PrepareProviderOpeningCommand{IncidentID: id.New(), ObservationID: id.New(), Reason: "verified opening", IdempotencyKey: "test-key"}
	user := &appctx.UserContext{UserID: id.New().String(), SessionID: id.New().String(), TenantID: id.New().String(), IsAdmin: true}
	ctx := appctx.WithUser(context.Background(), user)
	s := &ProviderBalanceWorkflowService{now: time.Now}
	// No commands/repositories configured: rejected requests cannot reach effects.
	_, err := s.PrepareOpening(context.Background(), req)
	require.Error(t, err)
	noKey := req
	noKey.IdempotencyKey = ""
	_, err = s.PrepareOpening(ctx, noKey)
	require.Error(t, err)
	s.stepUp = providerStepUpCheck(func(_ context.Context, session, actor id.ID, tenant, action string, _ time.Time) (bool, error) {
		fingerprint, e := command.Fingerprint(req)
		require.NoError(t, e)
		require.Equal(t, "crypto.provider_balance.prepare_opening:"+fingerprint, action)
		require.Equal(t, user.SessionID, session.String())
		require.Equal(t, user.UserID, actor.String())
		require.Equal(t, user.TenantID, tenant)
		return false, nil
	})
	_, err = s.PrepareOpening(ctx, req)
	require.ErrorContains(t, err, "step-up")
	denied := *user
	denied.IsAdmin = false
	denied.Roles = []string{"security_admin"}
	_, err = s.PrepareOpening(appctx.WithUser(context.Background(), &denied), req)
	require.Error(t, err)
}

func TestProviderOpeningFingerprintBindsEvidenceAndDecision(t *testing.T) {
	req := PrepareProviderOpeningCommand{IncidentID: id.New(), ObservationID: id.New(), Reason: "reason", IdempotencyKey: "first"}
	fingerprint, err := command.Fingerprint(req)
	require.NoError(t, err)
	req.IdempotencyKey = "retry"
	same, err := command.Fingerprint(req)
	require.NoError(t, err)
	require.Equal(t, fingerprint, same)
	req.ObservationID = id.New()
	changed, err := command.Fingerprint(req)
	require.NoError(t, err)
	require.NotEqual(t, fingerprint, changed)
	decision := ProviderBalanceDecisionCommand{OperationID: id.New(), Version: 1, Reason: "checked"}
	fingerprint, err = command.Fingerprint(decision)
	require.NoError(t, err)
	decision.Version++
	changed, err = command.Fingerprint(decision)
	require.NoError(t, err)
	require.NotEqual(t, fingerprint, changed)
}
