//go:build integration

package crypto_repo

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/posting"
	"metapus/internal/infrastructure/storage/postgres"
)

func decisionService111(ctx context.Context) *cryptoapp.ExternalOutflowService {
	tm := postgres.MustGetTxManager(ctx)
	return cryptoapp.NewExternalOutflowService(tm, NewExternalOutflowWorkflowRepo(), cryptoapp.NewOperationalHoldService(tm, NewOperationalHoldRepo()), posting.NewEngine(postgres.NewDocLocker()))
}

func decisionSnapshot111(t *testing.T, ctx context.Context, pool *pgxpool.Pool, outflow id.ID) string {
	t.Helper()
	var data string
	require.NoError(t, pool.QueryRow(ctx, `SELECT jsonb_build_object(
 'outflow',(SELECT to_jsonb(o) FROM reg_crypto_external_outflows o WHERE id=$1),
 'adjustments',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM doc_crypto_custody_adjustments a WHERE external_outflow_id=$1),
 'actions',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM reg_crypto_external_outflow_actions a WHERE external_outflow_id=$1),
 'networkHolds',(SELECT jsonb_agg(to_jsonb(h) ORDER BY id) FROM reg_crypto_network_holds h WHERE owner_type='external_outflow' AND owner_id=$1::uuid::text),
 'walletHolds',(SELECT jsonb_agg(to_jsonb(h) ORDER BY id) FROM reg_crypto_wallet_allocation_holds h WHERE owner_type='external_outflow' AND owner_id=$1::uuid::text),
 'movements',(SELECT jsonb_agg(to_jsonb(m) ORDER BY line_id) FROM reg_crypto_balance_movements m WHERE recorder_id IN (SELECT id FROM doc_crypto_custody_adjustments WHERE external_outflow_id=$1)),
 'requests',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM reg_crypto_reconciliation_requests r WHERE source_id IN (SELECT id FROM doc_crypto_custody_adjustments WHERE external_outflow_id=$1)))::text`, outflow).Scan(&data))
	return data
}

func TestOutflowDecisionApprovalReplayPreservesBasisAndMoney(t *testing.T) {
	f := newCustodyLossFixture(t, "")
	command := cryptoapp.ClassifyExternalOutflowCommand{OutflowID: f.doc.ExternalOutflowID, Classification: crypto.OutflowUnauthorizedLoss, Reason: "proven unexplained outflow"}
	before := decisionSnapshot111(t, f.ctx, f.pool, command.OutflowID)
	_, err := f.service.Classify(f.ctx, command, f.doc.MakerID)
	require.NoError(t, err)
	require.Equal(t, before, decisionSnapshot111(t, f.ctx, f.pool, command.OutflowID))
	changed := command
	changed.Evidence = map[string]any{"different": "evidence"}
	_, err = f.service.Classify(f.ctx, changed, f.doc.MakerID)
	require.Error(t, err)
	require.NoError(t, f.approve())
	posted := decisionSnapshot111(t, f.ctx, f.pool, command.OutflowID)
	require.NoError(t, f.approve())
	require.Equal(t, posted, decisionSnapshot111(t, f.ctx, f.pool, command.OutflowID))
	require.Equal(t, []int{1, 1, 1, 1}, f.counts(t))
	_, err = f.service.ApproveAdjustment(f.ctx, f.doc.ID, id.New(), "confirmed custody accounting loss")
	require.Error(t, err)
	_, err = f.service.ApproveAdjustment(f.ctx, f.doc.ID, f.checker, "different reason")
	require.Error(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_external_outflows SET finality_status='reorged' WHERE id=$1`, command.OutflowID)
	require.NoError(t, err)
	afterObservation := decisionSnapshot111(t, f.ctx, f.pool, command.OutflowID)
	require.NoError(t, f.approve(), "a saved financial decision is returned without a new posting or reauthorization")
	require.Equal(t, afterObservation, decisionSnapshot111(t, f.ctx, f.pool, command.OutflowID))
	for _, mutation := range []string{
		`UPDATE doc_crypto_custody_adjustments SET decision_reason='replaced' WHERE id=$1`,
		`UPDATE doc_crypto_custody_adjustments SET decided_at=clock_timestamp() WHERE id=$1`,
		`UPDATE doc_crypto_custody_adjustments SET decision_basis='{}' WHERE id=$1`,
	} {
		_, err = f.pool.Exec(f.ctx, mutation, f.doc.ID)
		require.Error(t, err)
	}
	var matchedAudit int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM reg_crypto_external_outflow_actions a JOIN doc_crypto_custody_adjustments d ON d.external_outflow_id=a.external_outflow_id
 WHERE d.id=$1 AND a.id=(d.decision_basis->>'id')::uuid AND a.decision_basis=d.decision_basis`, f.doc.ID).Scan(&matchedAudit))
	require.Equal(t, 1, matchedAudit)
}

func TestOutflowDecisionRejectAndReclassificationKeepSeparateHistory(t *testing.T) {
	f := newCustodyLossFixture(t, "")
	first := cryptoapp.ClassifyExternalOutflowCommand{OutflowID: f.doc.ExternalOutflowID, Classification: crypto.OutflowUnauthorizedLoss, Reason: "proven unexplained outflow"}
	_, err := f.service.RejectAdjustment(f.ctx, f.doc.ID, f.checker, "proof disputed")
	require.NoError(t, err)
	before := decisionSnapshot111(t, f.ctx, f.pool, first.OutflowID)
	_, err = f.service.RejectAdjustment(f.ctx, f.doc.ID, f.checker, "proof disputed")
	require.NoError(t, err)
	_, err = f.service.Classify(f.ctx, first, f.doc.MakerID)
	require.NoError(t, err)
	require.Equal(t, before, decisionSnapshot111(t, f.ctx, f.pool, first.OutflowID))
	_, err = f.service.ApproveAdjustment(f.ctx, f.doc.ID, f.checker, "proof disputed")
	require.Error(t, err)
	_, err = f.service.RejectAdjustment(f.ctx, f.doc.ID, f.checker, "changed")
	require.Error(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_external_outflows SET finality_status='reorged' WHERE id=$1`, first.OutflowID)
	require.NoError(t, err)
	reviewer := id.New()
	second := cryptoapp.ClassifyExternalOutflowCommand{OutflowID: first.OutflowID, Classification: crypto.OutflowFalsePositiveOrReorg, Reason: "removed chain observation", Evidence: map[string]any{"proof": "removed", "reviewed": true}}
	_, err = f.service.Classify(f.ctx, second, reviewer)
	require.NoError(t, err)
	after := decisionSnapshot111(t, f.ctx, f.pool, first.OutflowID)
	second.Evidence = map[string]any{"reviewed": true, "proof": "removed"}
	_, err = f.service.Classify(f.ctx, second, reviewer)
	require.NoError(t, err, "JSON field order does not create another decision")
	require.Equal(t, after, decisionSnapshot111(t, f.ctx, f.pool, first.OutflowID))
	_, err = f.service.Classify(f.ctx, first, f.doc.MakerID)
	require.Error(t, err, "a superseded classification cannot become a new command")
	second.Reason = "changed"
	_, err = f.service.Classify(f.ctx, second, reviewer)
	require.Error(t, err, "the rejected adjustment does not authorize arbitrary future edits")
	var decisions int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM reg_crypto_external_outflow_actions WHERE external_outflow_id=$1 AND action IN ('classified','reclassified') AND decision_basis IS NOT NULL`, first.OutflowID).Scan(&decisions))
	require.Equal(t, 2, decisions)
	require.Equal(t, []int{0, 0, 0, 0}, f.counts(t))
}

func TestOutflowDecisionResolutionRetainsExactReconciliationAndHolds(t *testing.T) {
	f := newOutflowFixture110(t, "tron")
	f.event.Canonicality = crypto.CanonicalityOrphaned
	f.record(t, f.event)
	key, _, _ := f.saved(t)
	ctx, pool := f.base.ctx, f.base.pool
	service := decisionService111(ctx)
	maker, resolver := id.New(), id.New()
	command := cryptoapp.ClassifyExternalOutflowCommand{OutflowID: key, Classification: crypto.OutflowFalsePositiveOrReorg, Reason: "removed observation"}
	_, err := service.Classify(ctx, command, maker)
	require.NoError(t, err)
	run, check := id.New(), id.New()
	_, err = pool.Exec(ctx, `INSERT INTO reg_crypto_reconciliation_runs(id,network_id,status,worker_id,started_at,completed_at,wallets_checked,items_created)
 VALUES($1,$2,'completed','decision-test',clock_timestamp(),clock_timestamp()+interval '1 second',1,0)`, run, f.network)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO reg_crypto_reconciliation_checks(id,run_id,network_id,wallet_id,token_id,ledger_amount,chain_amount,delta_amount) VALUES($1,$2,$3,$4,$5,0,0,0)`, check, run, f.network, f.wallet, f.asset)
	require.NoError(t, err)
	beforeResolution := decisionSnapshot111(t, ctx, pool, key)
	err = postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		if _, err := service.Resolve(ctx, key, resolver, "exact reconciliation"); err != nil {
			return err
		}
		_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `UPDATE reg_crypto_reconciliation_runs SET status='failed' WHERE id=$1`, run)
		return err
	})
	require.ErrorContains(t, err, "reconciliation basis changed")
	require.Equal(t, beforeResolution, decisionSnapshot111(t, ctx, pool, key), "a failed commit restores the decision, audit, incident and original hold cohort")
	_, err = service.Resolve(ctx, key, resolver, "exact reconciliation")
	require.NoError(t, err)
	before := decisionSnapshot111(t, ctx, pool, key)
	_, err = service.Resolve(ctx, key, resolver, "exact reconciliation")
	require.NoError(t, err)
	_, err = service.Classify(ctx, command, maker)
	require.NoError(t, err)
	require.Equal(t, before, decisionSnapshot111(t, ctx, pool, key))
	var savedCheck, savedRun string
	var holds int
	require.NoError(t, pool.QueryRow(ctx, `SELECT resolution_basis->'source'->'reconciliation'->>'checkId',resolution_basis->'source'->'reconciliation'->>'runId',jsonb_array_length(resolution_basis->'source'->'holds')
 FROM reg_crypto_external_outflows WHERE id=$1`, key).Scan(&savedCheck, &savedRun, &holds))
	require.Equal(t, check.String(), savedCheck)
	require.Equal(t, run.String(), savedRun)
	require.Equal(t, 2, holds)
	for _, mutation := range []string{
		`UPDATE reg_crypto_external_outflows SET resolution='rewritten' WHERE id=$1`,
		`UPDATE reg_crypto_external_outflows SET resolved_at=clock_timestamp() WHERE id=$1`,
		`UPDATE reg_crypto_external_outflows SET resolution_basis='{}' WHERE id=$1`,
	} {
		_, err = pool.Exec(ctx, mutation, key)
		require.Error(t, err)
	}
	_, err = service.Resolve(ctx, key, id.New(), "exact reconciliation")
	require.Error(t, err)
	_, err = service.Resolve(ctx, key, resolver, "changed")
	require.Error(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO reg_crypto_external_outflow_actions(id,external_outflow_id,actor_type,actor_id,action,old_state,new_state,reason,evidence,decision_basis)
 SELECT $2,external_outflow_id,actor_type,actor_id,action,old_state,new_state,reason,evidence,decision_basis FROM reg_crypto_external_outflow_actions WHERE external_outflow_id=$1 AND action='resolved'`, key, id.New())
	require.ErrorContains(t, err, "saved decision")
	require.Equal(t, before, decisionSnapshot111(t, ctx, pool, key))
}

func TestOutflowDecisionMigrationLeavesLegacyUnknownAndRejectsRollbackAfterDecision(t *testing.T) {
	f := newOutflowFixture110(t, "tron")
	f.event.Canonicality = crypto.CanonicalityOrphaned
	f.record(t, f.event)
	key, _, _ := f.saved(t)
	ctx, pool := f.base.ctx, f.base.pool
	provider := history107Provider(t, f.base)
	_, err := provider.DownTo(ctx, 103)
	require.NoError(t, err)
	legacy := id.New()
	_, err = pool.Exec(ctx, `INSERT INTO reg_crypto_external_outflows(network_id,token_id,source_wallet_id,tx_hash,event_kind,event_index,from_address,to_address,source_address_key,destination_address_key,contract_address,contract_address_key,amount,block_number,block_hash,finality_status)
 SELECT network_id,token_id,source_wallet_id,$2::uuid::text,event_kind,event_index,from_address,to_address,source_address_key,destination_address_key,contract_address,contract_address_key,amount,block_number,block_hash,'reorged' FROM reg_crypto_external_outflows WHERE id=$1`, key, legacy)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE reg_crypto_external_outflows SET status='classified',classification='false_positive_or_reorg',classification_reason='legacy review',classified_by=$2,classified_at=clock_timestamp() WHERE tx_hash=$1`, legacy.String(), id.New())
	require.NoError(t, err)
	_, err = provider.Up(ctx)
	require.NoError(t, err)
	var unknown bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT classification_basis IS NULL FROM reg_crypto_external_outflows WHERE tx_hash=$1`, legacy.String()).Scan(&unknown))
	require.True(t, unknown)
	_, err = provider.DownTo(ctx, 103)
	require.NoError(t, err)
	_, err = provider.Up(ctx)
	require.NoError(t, err)
	_, err = decisionService111(ctx).Classify(ctx, cryptoapp.ClassifyExternalOutflowCommand{OutflowID: key, Classification: crypto.OutflowFalsePositiveOrReorg, Reason: "new review"}, id.New())
	require.NoError(t, err)
	_, err = provider.DownTo(ctx, 103)
	require.ErrorContains(t, err, "forward recovery")
}
