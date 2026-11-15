//go:build integration

package crypto_worker

import (
	"context"
	"errors"
	"sync"
	"testing"

	"metapus/internal/application/command"
	"metapus/internal/application/cryptoapp"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	pbo "metapus/internal/domain/documents/crypto_provider_balance_operation"
	"metapus/internal/domain/eventlog"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/registers/crypto_provider_balance"
	"metapus/internal/domain/registers/cryptoposting"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/infrastructure/storage/postgres/document_repo"
	"metapus/internal/infrastructure/storage/postgres/register_repo"

	"github.com/stretchr/testify/require"
)

func (f providerOpeningFixture) admin(t *testing.T, byRole bool) context.Context {
	t.Helper()
	q := f.txm.GetQuerier(f.ctx)
	_, err := q.Exec(f.ctx, `INSERT INTO auth_users(id,email,password_hash,is_admin) VALUES($1::uuid,$1::uuid::text||'@opening.test','unused-fixture-hash',$2)`, f.maker, !byRole)
	require.NoError(t, err)
	if byRole {
		_, err = q.Exec(f.ctx, `INSERT INTO auth_user_roles(user_id,role_id) SELECT $1,id FROM auth_roles WHERE code='admin'`, f.maker)
		require.NoError(t, err)
	}
	user := &appctx.UserContext{UserID: f.maker.String(), SessionID: f.maker.String(), TenantID: f.account.String(), IsAdmin: !byRole}
	if byRole {
		user.Roles = []string{"admin"}
	}
	return appctx.WithUser(f.ctx, user)
}

func (f providerOpeningFixture) auditCount(t *testing.T, docID id.ID) int {
	t.Helper()
	var count int
	require.NoError(t, f.txm.GetQuerier(f.ctx).QueryRow(f.ctx, `SELECT count(*) FROM sys_event_log WHERE entity_id=$1 AND event_type='crypto.provider_opening.decision'`, docID).Scan(&count))
	return count
}

func TestProviderOpeningAdminSelfDecisionsAndImmutableAudit(t *testing.T) {
	for _, byRole := range []bool{false, true} {
		for _, approve := range []bool{false, true} {
			withProviderOpening(t, func(f providerOpeningFixture) {
				ctx := f.admin(t, byRole)
				doc := f.prepare(t)
				decide := f.service.Reject
				status, count := "rejected", 0
				if approve {
					decide = f.service.Approve
					status, count = "posted", 1
				}
				const n = 20
				results := make(chan command.Execution, n)
				errs := make(chan error, n)
				var wg sync.WaitGroup
				for range n {
					wg.Go(func() { res, err := decide(ctx, decisionFor(doc)); results <- res; errs <- err })
				}
				wg.Wait()
				close(results)
				close(errs)
				for err := range errs {
					require.NoError(t, err)
				}
				originals := 0
				for result := range results {
					if !result.Replayed {
						originals++
					}
					var payload cryptoapp.ProviderBalanceResult
					require.NoError(t, json.Unmarshal(result.Result.Payload, &payload))
					require.Equal(t, pbo.DecisionAdminSelf, payload.DecisionMode)
					require.Equal(t, 1, payload.DecisionPolicyVersion)
				}
				require.Equal(t, 1, originals)
				f.assertState(t, doc.ID, status, count)
				require.Equal(t, 1, f.auditCount(t, doc.ID))
				var details map[string]any
				var raw []byte
				require.NoError(t, f.txm.GetQuerier(f.ctx).QueryRow(f.ctx, `SELECT details FROM sys_event_log WHERE entity_id=$1 AND event_type='crypto.provider_opening.decision'`, doc.ID).Scan(&raw))
				require.NoError(t, json.Unmarshal(raw, &details))
				require.Equal(t, "admin_self", details["decisionMode"])
				basis := "system_admin_flag"
				if byRole {
					basis = "system_admin_role"
				}
				require.Equal(t, basis, details["authorityBasis"])
				require.Equal(t, f.maker.String(), details["makerId"])
				require.Len(t, details["requestFingerprint"], 64)
				require.NotEmpty(t, details["stepUpCheckedAt"])
				for _, sql := range []string{
					`UPDATE sys_event_log SET details='{}' WHERE entity_id=$1 AND event_type='crypto.provider_opening.decision'`,
					`DELETE FROM sys_event_log WHERE entity_id=$1 AND event_type='crypto.provider_opening.decision'`,
					`UPDATE doc_crypto_provider_balance_operations SET decision_mode='independent' WHERE id=$1`,
					`UPDATE doc_crypto_provider_balance_operations SET decision_policy_version=2 WHERE id=$1`,
				} {
					_, err := f.txm.GetQuerier(f.ctx).Exec(f.ctx, sql, doc.ID)
					require.Error(t, err)
				}
				// Revoking today's authority does not invalidate the historical decision.
				_, err := f.txm.GetQuerier(f.ctx).Exec(f.ctx, `UPDATE auth_users SET is_admin=FALSE,auth_version=auth_version+1 WHERE id=$1`, f.maker)
				require.NoError(t, err)
				_, err = f.txm.GetQuerier(f.ctx).Exec(f.ctx, `DELETE FROM auth_user_roles WHERE user_id=$1`, f.maker)
				require.NoError(t, err)
				stored, err := document_repo.NewCryptoProviderBalanceOperationRepo().GetByID(f.ctx, doc.ID)
				require.NoError(t, err)
				require.Equal(t, pbo.DecisionAdminSelf, stored.DecisionMode)
				f.assertState(t, doc.ID, status, count)
			})
		}
	}
}

func TestProviderOpeningAdminAuthorityRevocationWins(t *testing.T) {
	for _, byRole := range []bool{false, true} {
		withProviderOpening(t, func(f providerOpeningFixture) {
			ctx := f.admin(t, byRole)
			doc := f.prepare(t)
			locked, release := make(chan struct{}), make(chan struct{})
			revokeErr := make(chan error, 1)
			go func() {
				revokeErr <- f.txm.RunInTransaction(f.ctx, func(txCtx context.Context) error {
					q := f.txm.GetQuerier(txCtx)
					if byRole {
						if _, err := q.Exec(txCtx, `DELETE FROM auth_user_roles WHERE user_id=$1`, f.maker); err != nil {
							return err
						}
					}
					if _, err := q.Exec(txCtx, `UPDATE auth_users SET is_admin=FALSE,auth_version=auth_version+1 WHERE id=$1`, f.maker); err != nil {
						return err
					}
					close(locked)
					<-release
					return nil
				})
			}()
			<-locked
			decisionErr := make(chan error, 1)
			go func() { _, err := f.service.Approve(ctx, decisionFor(doc)); decisionErr <- err }()
			close(release)
			require.NoError(t, <-revokeErr)
			require.Error(t, <-decisionErr)
			f.assertState(t, doc.ID, "pending_approval", 0)
			require.Zero(t, f.auditCount(t, doc.ID))
			// Even raw SQL cannot turn a stale claim into admin_self.
			_, err := f.txm.GetQuerier(f.ctx).Exec(f.ctx, `UPDATE doc_crypto_provider_balance_operations
                SET status='rejected',version=version+1,rejected_by=created_by,updated_by=created_by,rejected_at=now(),rejection_reason='spoof',decision_mode='admin_self',decision_policy_version=1 WHERE id=$1`, doc.ID)
			require.ErrorContains(t, err, "current system administrator authority")
		})
	}
}

type providerFaultAudit struct {
	entered chan struct{}
	release chan struct{}
	fail    bool
}

func (w providerFaultAudit) Write(ctx context.Context, e eventlog.Event) error {
	if w.entered != nil {
		close(w.entered)
		<-w.release
	}
	if w.fail {
		return errors.New("injected mandatory audit failure")
	}
	return postgres.NewEventLogRepo().Write(ctx, e)
}
func (f providerOpeningFixture) serviceWithAudit(audit providerFaultAudit) *cryptoapp.ProviderBalanceWorkflowService {
	engine := posting.NewEngine(nil)
	engine.AddVisitor(&cryptoposting.CryptoProviderBalanceVisitor{})
	engine.AddRecorder(cryptoposting.NewCryptoProviderBalanceRecorder(crypto_provider_balance.NewService(register_repo.NewCryptoProviderBalanceRepo())))
	return cryptoapp.NewProviderBalanceWorkflowService(document_repo.NewCryptoProviderBalanceOperationRepo(), crypto_repo.NewProviderBalanceWorkflowRepo(), fixedNetworkOperationNumerator{}, engine, command.NewExecutor(f.txm, postgres.NewCommandRepository()), f.stepUp, crypto_repo.NewProviderOpeningAuthorityRepo(), audit)
}

func TestProviderOpeningAdminDecisionWinsBeforeRevocation(t *testing.T) {
	for _, byRole := range []bool{false, true} {
		withProviderOpening(t, func(f providerOpeningFixture) {
			ctx := f.admin(t, byRole)
			doc := f.prepare(t)
			entered, release := make(chan struct{}), make(chan struct{})
			service := f.serviceWithAudit(providerFaultAudit{entered: entered, release: release})
			decisionErr := make(chan error, 1)
			go func() { _, err := service.Approve(ctx, decisionFor(doc)); decisionErr <- err }()
			<-entered // The decision holds the membership/user authority locks.
			revokeErr := make(chan error, 1)
			go func() {
				revokeErr <- f.txm.RunInTransaction(f.ctx, func(txCtx context.Context) error {
					q := f.txm.GetQuerier(txCtx)
					if _, err := q.Exec(txCtx, `SET LOCAL lock_timeout='50ms'`); err != nil {
						return err
					}
					if byRole {
						_, err := q.Exec(txCtx, `DELETE FROM auth_user_roles WHERE user_id=$1`, f.maker)
						return err
					}
					_, err := q.Exec(txCtx, `UPDATE auth_users SET is_admin=FALSE WHERE id=$1`, f.maker)
					return err
				})
			}()
			err := <-revokeErr
			close(release)
			require.ErrorContains(t, err, "lock timeout")
			require.NoError(t, <-decisionErr)
			f.assertState(t, doc.ID, "posted", 1)
		})
	}
}

func TestProviderOpeningMandatoryAuditFailureRollsBack(t *testing.T) {
	withProviderOpening(t, func(f providerOpeningFixture) {
		ctx := f.admin(t, false)
		doc := f.prepare(t)
		_, err := f.txm.GetQuerier(f.ctx).Exec(f.ctx, `UPDATE doc_crypto_provider_balance_operations SET
			status='rejected',version=version+1,rejected_by=created_by,updated_by=created_by,rejected_at=now(),rejection_reason='no audit',decision_mode='admin_self',decision_policy_version=1 WHERE id=$1`, doc.ID)
		require.ErrorContains(t, err, "exact audit must commit atomically")
		_, err = f.serviceWithAudit(providerFaultAudit{fail: true}).Approve(ctx, decisionFor(doc))
		require.ErrorContains(t, err, "mandatory audit failure")
		f.assertState(t, doc.ID, "pending_approval", 0)
		require.Zero(t, f.auditCount(t, doc.ID))
		_, err = f.service.Approve(ctx, decisionFor(doc))
		require.NoError(t, err)
		f.assertState(t, doc.ID, "posted", 1)
		require.Equal(t, 1, f.auditCount(t, doc.ID))
	})
}

func TestProviderOpeningAdminApproveRejectRace(t *testing.T) {
	withProviderOpening(t, func(f providerOpeningFixture) {
		ctx := f.admin(t, true)
		doc := f.prepare(t)
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		wg.Go(func() { _, err := f.service.Approve(ctx, decisionFor(doc)); errs <- err })
		wg.Go(func() {
			req := decisionFor(doc)
			req.IdempotencyKey = id.New().String()
			_, err := f.service.Reject(ctx, req)
			errs <- err
		})
		wg.Wait()
		close(errs)
		successes := 0
		for err := range errs {
			if err == nil {
				successes++
			}
		}
		require.Equal(t, 1, successes)
		require.Equal(t, 1, f.auditCount(t, doc.ID))
	})
}

func TestProviderOpeningInvalidAdminPrincipal(t *testing.T) {
	for _, byRole := range []bool{false, true} {
		for _, invalidation := range []string{"is_active=FALSE", "deletion_mark=TRUE"} {
			withProviderOpening(t, func(f providerOpeningFixture) {
				ctx := f.admin(t, byRole)
				doc := f.prepare(t)
				_, err := f.txm.GetQuerier(f.ctx).Exec(f.ctx, `UPDATE auth_users SET `+invalidation+` WHERE id=$1`, f.maker)
				require.NoError(t, err)
				_, err = f.service.Approve(ctx, decisionFor(doc))
				require.Error(t, err, "a stale administrative context must not authorize a disabled or deleted principal")
				_, err = f.service.Reject(ctx, decisionFor(doc))
				require.Error(t, err)
				f.assertState(t, doc.ID, "pending_approval", 0)
				require.Zero(t, f.auditCount(t, doc.ID))
			})
		}
	}
}

type providerExpiringAuthority struct {
	cryptoapp.ProviderOpeningAuthorityRepository
	stepUp *providerTestStepUp
}

func (r providerExpiringAuthority) LockSystemAdmin(ctx context.Context, actor id.ID) (cryptoapp.ProviderOpeningAdminAuthority, error) {
	authority, err := r.ProviderOpeningAuthorityRepository.LockSystemAdmin(ctx, actor)
	// Model a grant that was fresh before the database locks, but expired while
	// acquiring them. The subsequent application check must reject the command.
	r.stepUp.denied.Store(true)
	return authority, err
}

func TestProviderOpeningStepUpExpiresAtAuthorityLock(t *testing.T) {
	for _, approve := range []bool{false, true} {
		withProviderOpening(t, func(f providerOpeningFixture) {
			ctx := f.admin(t, true)
			doc := f.prepare(t)
			engine := posting.NewEngine(nil)
			engine.AddVisitor(&cryptoposting.CryptoProviderBalanceVisitor{})
			engine.AddRecorder(cryptoposting.NewCryptoProviderBalanceRecorder(crypto_provider_balance.NewService(register_repo.NewCryptoProviderBalanceRepo())))
			service := cryptoapp.NewProviderBalanceWorkflowService(document_repo.NewCryptoProviderBalanceOperationRepo(), crypto_repo.NewProviderBalanceWorkflowRepo(), fixedNetworkOperationNumerator{}, engine,
				command.NewExecutor(f.txm, postgres.NewCommandRepository()), f.stepUp,
				providerExpiringAuthority{crypto_repo.NewProviderOpeningAuthorityRepo(), f.stepUp}, postgres.NewEventLogRepo())
			decide := service.Reject
			if approve {
				decide = service.Approve
			}
			_, err := decide(ctx, decisionFor(doc))
			require.ErrorContains(t, err, "step-up")
			f.assertState(t, doc.ID, "pending_approval", 0)
			require.Zero(t, f.auditCount(t, doc.ID))
			f.stepUp.denied.Store(false)
			freshDecision := f.service.Reject
			if approve {
				freshDecision = f.service.Approve
			}
			_, err = freshDecision(ctx, decisionFor(doc))
			require.NoError(t, err, "expired step-up must roll back the command key as well")
		})
	}
}

func TestProviderOpeningAdminDifferentKeysSingleWinner(t *testing.T) {
	withProviderOpening(t, func(f providerOpeningFixture) {
		ctx := f.admin(t, false)
		doc := f.prepare(t)
		const n = 20
		errs := make(chan error, n)
		var wg sync.WaitGroup
		for range n {
			wg.Go(func() {
				req := decisionFor(doc)
				req.IdempotencyKey = id.New().String()
				_, err := f.service.Approve(ctx, req)
				errs <- err
			})
		}
		wg.Wait()
		close(errs)
		winners := 0
		for err := range errs {
			if err == nil {
				winners++
			}
		}
		require.Equal(t, 1, winners)
		f.assertState(t, doc.ID, "posted", 1)
		require.Equal(t, 1, f.auditCount(t, doc.ID))
	})
}
