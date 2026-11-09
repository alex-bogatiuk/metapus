//go:build integration

package crypto_worker

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/application/command"
	"metapus/internal/application/cryptoapp"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
	pbo "metapus/internal/domain/documents/crypto_provider_balance_operation"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/registers/crypto_provider_balance"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/infrastructure/storage/postgres/document_repo"
	"metapus/internal/infrastructure/storage/postgres/register_repo"
)

type providerTestStepUp struct{ denied atomic.Bool }

func (s *providerTestStepUp) HasFreshStepUp(context.Context, id.ID, id.ID, string, string, time.Time) (bool, error) {
	return !s.denied.Load(), nil
}

type providerOpeningFixture struct {
	ctx                                                   context.Context
	txm                                                   *postgres.TxManager
	account, token, incident, observation, maker, checker id.ID
	service                                               *cryptoapp.ProviderBalanceWorkflowService
	stepUp                                                *providerTestStepUp
}

func withProviderOpening(t *testing.T, fn func(providerOpeningFixture)) {
	t.Helper()
	runSweepCoordinationFixture(t, true, func(ctx context.Context, txm *postgres.TxManager, base sweepCoordinationFixture) {
		f := providerOpeningFixture{ctx: ctx, txm: txm, account: id.New(), token: base.native, maker: id.New(), checker: id.New(), stepUp: &providerTestStepUp{}}
		err := txm.RunInTransaction(ctx, func(ctx context.Context) error {
			_, err := txm.GetQuerier(ctx).Exec(ctx, `INSERT INTO cat_crypto_provider_accounts(id,code,name,provider,network_id,balance_token_id) VALUES($1::uuid,$1::uuid::text,'Provider workflow test','itrx',$2,$3)`, f.account, base.network, f.token)
			if err != nil {
				return err
			}
			return NewOutboundRepo().RecordProviderBalanceObservation(ctx, f.account, f.token, "test-opening", types.NewCryptoAmountFromInt64(1000))
		})
		require.NoError(t, err)
		require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, `SELECT id FROM reg_crypto_provider_balance_incidents WHERE provider_account_id=$1`, f.account).Scan(&f.incident))
		require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, `SELECT id FROM reg_crypto_provider_balance_observations WHERE provider_account_id=$1`, f.account).Scan(&f.observation))
		f.service = f.newService(postgres.NewCommandRepository())
		fn(f)
	})
}

func (f providerOpeningFixture) newService(commands command.Repository) *cryptoapp.ProviderBalanceWorkflowService {
	engine := posting.NewEngine(nil)
	engine.AddVisitor(&posting.CryptoProviderBalanceVisitor{})
	engine.AddRecorder(posting.NewCryptoProviderBalanceRecorder(crypto_provider_balance.NewService(register_repo.NewCryptoProviderBalanceRepo())))
	return cryptoapp.NewProviderBalanceWorkflowService(document_repo.NewCryptoProviderBalanceOperationRepo(), crypto_repo.NewProviderBalanceWorkflowRepo(),
		fixedNetworkOperationNumerator{}, engine, command.NewExecutor(f.txm, commands), f.stepUp,
		crypto_repo.NewProviderOpeningAuthorityRepo(), postgres.NewEventLogRepo())
}
func (f providerOpeningFixture) actor(actor id.ID) context.Context {
	return appctx.WithUser(f.ctx, &appctx.UserContext{UserID: actor.String(), SessionID: actor.String(), TenantID: f.account.String(), Permissions: []string{
		"document:crypto_provider_balance_operation:create", "document:crypto_provider_balance_operation:approve", "document:crypto_provider_balance_operation:reject",
	}})
}
func (f providerOpeningFixture) request() cryptoapp.PrepareProviderOpeningCommand {
	return cryptoapp.PrepareProviderOpeningCommand{IncidentID: f.incident, ObservationID: f.observation, Reason: "Independent provider snapshot verified", IdempotencyKey: f.incident.String()}
}
func (f providerOpeningFixture) prepare(t *testing.T) cryptoapp.ProviderBalanceResult {
	t.Helper()
	result, err := f.service.PrepareOpening(f.actor(f.maker), f.request())
	require.NoError(t, err)
	var doc cryptoapp.ProviderBalanceResult
	require.NoError(t, json.Unmarshal(result.Result.Payload, &doc))
	return doc
}
func decisionFor(doc cryptoapp.ProviderBalanceResult) cryptoapp.ProviderBalanceDecisionCommand {
	return cryptoapp.ProviderBalanceDecisionCommand{OperationID: doc.ID, Version: doc.Version, Reason: "Evidence independently checked", IdempotencyKey: doc.ID.String()}
}
func (f providerOpeningFixture) assertState(t *testing.T, docID id.ID, status string, movements int) {
	t.Helper()
	var state string
	var count int
	q := f.txm.GetQuerier(f.ctx)
	require.NoError(t, q.QueryRow(f.ctx, `SELECT status FROM doc_crypto_provider_balance_operations WHERE id=$1`, docID).Scan(&state))
	require.Equal(t, status, state)
	require.NoError(t, q.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_provider_balance_movements WHERE recorder_id=$1`, docID).Scan(&count))
	require.Equal(t, movements, count)
	for _, table := range []string{"reg_crypto_balance_movements", "reg_crypto_merchant_balance_movements"} {
		require.NoError(t, q.QueryRow(f.ctx, "SELECT COUNT(*) FROM "+table+" WHERE recorder_id=$1", docID).Scan(&count))
		require.Zero(t, count, "opening must not affect wallet or merchant balances")
	}
}

func TestProviderOpeningConcurrentReplayAndIndependentApproval(t *testing.T) {
	withProviderOpening(t, func(f providerOpeningFixture) {
		const requests = 20
		results := make(chan command.Execution, requests)
		errs := make(chan error, requests)
		var wg sync.WaitGroup
		for range requests {
			wg.Go(func() {
				res, err := f.service.PrepareOpening(f.actor(f.maker), f.request())
				results <- res
				errs <- err
			})
		}
		wg.Wait()
		close(results)
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
		var resource string
		first := 0
		var doc cryptoapp.ProviderBalanceResult
		for res := range results {
			if !res.Replayed {
				first++
			}
			if resource == "" {
				resource = res.Result.ResourceID
			}
			require.Equal(t, resource, res.Result.ResourceID)
			require.NoError(t, json.Unmarshal(res.Result.Payload, &doc))
		}
		require.Equal(t, 1, first)
		stored, err := document_repo.NewCryptoProviderBalanceOperationRepo().GetByID(f.ctx, doc.ID)
		require.NoError(t, err)
		require.Equal(t, f.maker, stored.CreatedBy)
		_, err = f.service.Approve(f.actor(f.maker), decisionFor(doc))
		require.Error(t, err, "maker cannot approve")
		f.assertState(t, doc.ID, "pending_approval", 0)
		results = make(chan command.Execution, requests)
		errs = make(chan error, requests)
		for range requests {
			wg.Go(func() {
				res, err := f.service.Approve(f.actor(f.checker), decisionFor(doc))
				results <- res
				errs <- err
			})
		}
		wg.Wait()
		close(results)
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
		first = 0
		for res := range results {
			if !res.Replayed {
				first++
			}
			require.Equal(t, "posted", res.Result.Outcome)
		}
		require.Equal(t, 1, first)
		f.assertState(t, doc.ID, "posted", 1)
		changed := f.request()
		changed.Reason = "changed reason"
		_, err = f.service.PrepareOpening(f.actor(f.maker), changed)
		require.Error(t, err)
		f.stepUp.denied.Store(true)
		_, err = f.service.Approve(f.actor(f.checker), decisionFor(doc))
		require.Error(t, err, "replay must not bypass fresh step-up")
		var status string
		require.NoError(t, f.txm.GetQuerier(f.ctx).QueryRow(f.ctx, `SELECT status FROM reg_crypto_provider_balance_incidents WHERE id=$1`, f.incident).Scan(&status))
		require.Equal(t, "open", status, "old provider evidence cannot resolve a newly posted opening")
		require.NoError(t, f.txm.RunInTransaction(f.ctx, func(ctx context.Context) error {
			return NewOutboundRepo().RecordProviderBalanceObservation(ctx, f.account, f.token, "fresh-after-opening", types.NewCryptoAmountFromInt64(1000))
		}))
		require.NoError(t, f.txm.GetQuerier(f.ctx).QueryRow(f.ctx, `SELECT status FROM reg_crypto_provider_balance_incidents WHERE id=$1`, f.incident).Scan(&status))
		require.Equal(t, "resolved", status)
	})
}

func TestProviderOpeningStaleEvidenceAndDecisionRace(t *testing.T) {
	withProviderOpening(t, func(f providerOpeningFixture) {
		doc := f.prepare(t)
		require.NoError(t, f.txm.RunInTransaction(f.ctx, func(ctx context.Context) error {
			return NewOutboundRepo().RecordProviderBalanceObservation(ctx, f.account, f.token, "new-evidence", types.NewCryptoAmountFromInt64(1000))
		}))
		_, err := f.service.Approve(f.actor(f.checker), decisionFor(doc))
		require.Error(t, err, "even a same-value newer observation invalidates the old authorization")
		f.assertState(t, doc.ID, "pending_approval", 0)
		_, err = f.service.Reject(f.actor(f.checker), decisionFor(doc))
		require.NoError(t, err)
		f.assertState(t, doc.ID, "rejected", 0)
	})
	withProviderOpening(t, func(f providerOpeningFixture) {
		doc := f.prepare(t)
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		wg.Go(func() { _, err := f.service.Approve(f.actor(f.checker), decisionFor(doc)); errs <- err })
		wg.Go(func() { _, err := f.service.Reject(f.actor(id.New()), decisionFor(doc)); errs <- err })
		wg.Wait()
		close(errs)
		success := 0
		for err := range errs {
			if err == nil {
				success++
			}
		}
		require.Equal(t, 1, success, "exactly one terminal decision wins")
		stored, err := document_repo.NewCryptoProviderBalanceOperationRepo().GetByID(f.ctx, doc.ID)
		require.NoError(t, err)
		movements := 0
		if stored.Status == "posted" {
			movements = 1
		}
		f.assertState(t, doc.ID, stored.Status, movements)
	})
}

type failProviderCompletion struct{ command.Repository }

func (f failProviderCompletion) Complete(context.Context, command.Metadata, command.Result) error {
	return errors.New("injected completion failure")
}

func TestProviderOpeningRollbackAndDatabaseBypassGuards(t *testing.T) {
	withProviderOpening(t, func(f providerOpeningFixture) {
		doc := f.prepare(t)
		broken := f.newService(failProviderCompletion{postgres.NewCommandRepository()})
		_, err := broken.Approve(f.actor(f.checker), decisionFor(doc))
		require.ErrorContains(t, err, "injected completion failure")
		f.assertState(t, doc.ID, "pending_approval", 0)
		q := f.txm.GetQuerier(f.ctx)
		// Correct checker metadata without the economic effect must fail at commit.
		_, err = q.Exec(f.ctx, `UPDATE doc_crypto_provider_balance_operations SET status='posted',posted=TRUE,posted_version=1,version=version+1,
      approved_by=$2,updated_by=$2,approved_at=NOW(),approval_reason='checked',decision_mode='independent',decision_policy_version=1 WHERE id=$1`, doc.ID, f.checker)
		require.ErrorContains(t, err, "must commit atomically")
		// An extra/wrong amount cannot be hidden behind a posted flag either.
		err = f.txm.RunInTransaction(f.ctx, func(ctx context.Context) error {
			q := f.txm.GetQuerier(ctx)
			_, err := q.Exec(ctx, `INSERT INTO reg_crypto_provider_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,provider_account_id,token_id,operation_type,external_ref,amount)
       SELECT gen_random_uuid_v7(),id,'CryptoProviderBalanceOperation',1,date,record_type,provider_account_id,token_id,operation_type,external_ref,amount+1
       FROM doc_crypto_provider_balance_operations WHERE id=$1`, doc.ID)
			if err != nil {
				return err
			}
			_, err = q.Exec(ctx, `UPDATE doc_crypto_provider_balance_operations SET status='posted',posted=TRUE,posted_version=1,version=version+1,
       approved_by=$2,updated_by=$2,approved_at=NOW(),approval_reason='checked',decision_mode='independent',decision_policy_version=1 WHERE id=$1`, doc.ID, f.checker)
			return err
		})
		require.ErrorContains(t, err, "must commit atomically")
		for _, sql := range []string{
			`UPDATE doc_crypto_provider_balance_operations SET created_by='00000000-0000-0000-0000-000000000000' WHERE id=$1`,
			`UPDATE doc_crypto_provider_balance_operations SET created_by=NULL WHERE id=$1`,
			`UPDATE doc_crypto_provider_balance_operations SET amount=amount+1 WHERE id=$1`,
			`UPDATE doc_crypto_provider_balance_operations SET deletion_mark=TRUE WHERE id=$1`,
			`DELETE FROM doc_crypto_provider_balance_operations WHERE id=$1`,
			`UPDATE doc_crypto_provider_balance_operations SET status='posted',posted=TRUE,posted_version=1,version=version+1,approved_by=created_by,approved_at=NOW(),approval_reason='self' WHERE id=$1`,
			`UPDATE doc_crypto_provider_balance_operations SET status='posted',posted=TRUE,posted_version=1,version=version+1,approved_by=gen_random_uuid_v7(),approved_at=NOW(),approval_reason='no movement' WHERE id=$1`,
		} {
			_, err = q.Exec(f.ctx, sql, doc.ID)
			require.Error(t, err, sql)
		}
		for _, sql := range []string{
			`UPDATE reg_crypto_provider_balance_observations SET delta_amount=0 WHERE id=$1`,
			`DELETE FROM reg_crypto_provider_balance_observations WHERE id=$1`,
		} {
			_, err = q.Exec(f.ctx, sql, f.observation)
			require.Error(t, err)
		}
		_, err = f.service.Approve(f.actor(f.checker), decisionFor(doc))
		require.NoError(t, err, "rollback must release the same idempotency key")
		f.assertState(t, doc.ID, "posted", 1)
		for _, sql := range []string{
			`UPDATE doc_crypto_provider_balance_operations SET status='pending_approval',posted=FALSE WHERE id=$1`,
			`UPDATE doc_crypto_provider_balance_operations SET approved_by=created_by WHERE id=$1`,
			`DELETE FROM reg_crypto_provider_balance_movements WHERE recorder_id=$1`,
			`UPDATE reg_crypto_provider_balance_movements SET amount=1 WHERE recorder_id=$1`,
		} {
			_, err = q.Exec(f.ctx, sql, doc.ID)
			require.Error(t, err)
		}
		stored, err := document_repo.NewCryptoProviderBalanceOperationRepo().GetByID(f.ctx, doc.ID)
		require.NoError(t, err)
		require.Error(t, stored.CanPost(f.ctx), "posted financial facts cannot be reposted")
		require.Equal(t, pbo.StatusPosted, stored.Status)
	})
}

func TestProviderOpeningSerializesWithProviderConsumption(t *testing.T) {
	withProviderOpening(t, func(f providerOpeningFixture) {
		doc := f.prepare(t)
		var approvalErr, chargeErr error
		var wg sync.WaitGroup
		wg.Go(func() { _, approvalErr = f.service.Approve(f.actor(f.checker), decisionFor(doc)) })
		wg.Go(func() {
			chargeErr = f.txm.RunInTransaction(f.ctx, func(ctx context.Context) error {
				q := f.txm.GetQuerier(ctx)
				_, err := q.Exec(ctx, `INSERT INTO reg_crypto_provider_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,provider_account_id,token_id,operation_type,external_ref,amount)
        VALUES(gen_random_uuid_v7(),$1,'CryptoResourceRental',1,NOW(),'expense',$2,$3,'rental_charge','test-consumption',1)`, id.New(), f.account, f.token)
				if err != nil {
					return err
				}
				return NewOutboundRepo().RecordProviderBalanceObservation(ctx, f.account, f.token, "provider-after-consumption", types.NewCryptoAmountFromInt64(999))
			})
		})
		wg.Wait()
		require.NoError(t, chargeErr)
		var ledger string
		require.NoError(t, f.txm.GetQuerier(f.ctx).QueryRow(f.ctx, `SELECT SUM(CASE WHEN record_type='receipt' THEN amount ELSE -amount END)::text FROM reg_crypto_provider_balance_movements WHERE provider_account_id=$1`, f.account).Scan(&ledger))
		if approvalErr == nil {
			require.Equal(t, "999", ledger)
			f.assertState(t, doc.ID, "posted", 1)
		} else {
			require.Equal(t, "-1", ledger)
			f.assertState(t, doc.ID, "pending_approval", 0)
		}
	})
}

func TestProviderOpeningDistinctCommandsCannotDuplicateAcquisition(t *testing.T) {
	withProviderOpening(t, func(f providerOpeningFixture) {
		const n = 20
		var wg sync.WaitGroup
		var successes atomic.Int32
		for range n {
			wg.Go(func() {
				req := f.request()
				req.IdempotencyKey = id.New().String()
				_, err := f.service.PrepareOpening(f.actor(id.New()), req)
				if err == nil {
					successes.Add(1)
				}
			})
		}
		wg.Wait()
		require.EqualValues(t, 1, successes.Load())
		var count int
		require.NoError(t, f.txm.GetQuerier(f.ctx).QueryRow(f.ctx, `SELECT COUNT(*) FROM doc_crypto_provider_balance_operations WHERE provider_account_id=$1`, f.account).Scan(&count))
		require.Equal(t, 1, count)
	})
}
