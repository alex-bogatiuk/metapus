//go:build integration

package crypto_repo

import (
	"context"
	"database/sql"
	"io/fs"
	"sync"
	"testing"
	"time"

	dbassets "metapus/db"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

func history107Provider(t *testing.T, f amlRoutingFixture) *goose.Provider {
	t.Helper()
	db, err := sql.Open("pgx", f.pool.Config().ConnConfig.ConnString())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	root, err := fs.Sub(dbassets.Files, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectPostgres, db, root)
	require.NoError(t, err)
	return provider
}

func completedDecision107(t *testing.T) amlRoutingFixture {
	t.Helper()
	f := newAMLRoutingFixture(t, true)
	_, effect := f.save(t, f.proof(f.input.Request.Address, "decision-history", 1))
	f.complete(t, effect, 1000, true)
	return f
}

func decisionSnapshot107(t *testing.T, f amlRoutingFixture) string {
	t.Helper()
	var data string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT jsonb_agg(to_jsonb(d) ORDER BY id)::text FROM reg_crypto_aml_decisions d`).Scan(&data))
	return data
}

func requeueDecision107(t *testing.T, f amlRoutingFixture) {
	t.Helper()
	_, err := f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_aml_decision_queue(report_id,policy_version) SELECT report_id,policy_version FROM reg_crypto_aml_decisions ON CONFLICT DO NOTHING`)
	require.NoError(t, err)
}

func TestAMLDecisionReplayUsesOriginalClockAndRetainsExpiry(t *testing.T) {
	f := completedDecision107(t)
	before := decisionSnapshot107(t, f)
	requeueDecision107(t, f)
	applied, err := f.repo.ApplyNextAMLDecision(f.ctx, time.Now().Add(3*time.Hour))
	require.NoError(t, err)
	require.True(t, applied, "late queue delivery retains the first assessment")
	require.Equal(t, before, decisionSnapshot107(t, f))
	// Recreated queue membership is consumed once without creating another fact.
	requeueDecision107(t, f)
	results := make([]bool, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Go(func() {
			results[i], errs[i] = f.repo.ApplyNextAMLDecision(f.ctx, time.Now().Add(time.Duration(i)*time.Hour))
		})
	}
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	require.NotEqual(t, results[0], results[1])
	require.Equal(t, before, decisionSnapshot107(t, f))
	for _, change := range []string{
		`'high',reason,expires_at,evaluated_at`,
		`risk_class,'another reason',expires_at,evaluated_at`,
		`risk_class,reason,expires_at+interval '1 second',evaluated_at`,
		`risk_class,reason,expires_at,evaluated_at+interval '1 second'`,
		`risk_class,NULL,expires_at,evaluated_at`,
		`risk_class,reason,expires_at,NULL`,
	} {
		_, err := f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_aml_decisions(subject_id,report_id,generation,policy_version,risk_class,reason,expires_at,evaluated_at)
 SELECT subject_id,report_id,generation,policy_version,`+change+` FROM reg_crypto_aml_decisions ON CONFLICT DO NOTHING`)
		require.Error(t, err)
	}
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_aml_decisions(subject_id,report_id,generation,policy_version,risk_class,reason,expires_at,evaluated_at)
 SELECT subject_id,report_id,generation,policy_version,risk_class,reason,expires_at,evaluated_at FROM reg_crypto_aml_decisions ON CONFLICT DO NOTHING`)
	require.NoError(t, err)
	require.Equal(t, before, decisionSnapshot107(t, f))
	requeueDecision107(t, f)
	tx, err := f.pool.Begin(f.ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(f.ctx) }()
	_, err = tx.Exec(f.ctx, `SELECT token_id FROM sys_crypto_aml_routing_scopes WHERE network_id=$1 FOR UPDATE`, f.input.Request.NetworkID)
	require.NoError(t, err)
	applied, err = f.repo.ApplyNextAMLDecision(f.ctx, time.Now())
	require.NoError(t, err)
	require.False(t, applied, "busy scope is skipped before wallet invalidation")
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_aml_decisions(subject_id,report_id,generation,policy_version,risk_class,reason,expires_at,evaluated_at)
 SELECT subject_id,report_id,generation,policy_version,risk_class,reason,expires_at,evaluated_at FROM reg_crypto_aml_decisions ON CONFLICT DO NOTHING`)
	require.ErrorContains(t, err, "55P03", "direct SQL cannot wait for a scope while holding other business rows")
	require.NoError(t, tx.Rollback(f.ctx))
	applied, err = f.repo.ApplyNextAMLDecision(f.ctx, time.Now())
	require.NoError(t, err)
	require.True(t, applied)
	require.Equal(t, before, decisionSnapshot107(t, f))
}

func TestAMLDecisionChangedSavedAssessmentDoesNotConsumeQueue(t *testing.T) {
	f := completedDecision107(t)
	_, err := f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_aml_risk_policies(version,low_max_bp,medium_max_bp,max_report_age_seconds,decision_ttl_seconds,accepted_signals,critical_signals)
 SELECT 'history107-policy',low_max_bp,medium_max_bp,max_report_age_seconds,decision_ttl_seconds,accepted_signals,critical_signals FROM sys_crypto_aml_risk_policies WHERE version='routing-v1'`)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE sys_crypto_aml_routing_scopes SET policy_version='history107-policy',version=version+1`)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_aml_decisions(subject_id,report_id,generation,policy_version,risk_class,reason,expires_at)
 SELECT subject_id,report_id,generation,'history107-policy',risk_class,reason,expires_at FROM reg_crypto_aml_decisions WHERE policy_version='routing-v1'`)
	require.ErrorContains(t, err, "assessment clock")
	// A separately written first result is checked against the immutable report
	// and its original clock before the application acknowledges queue delivery.
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_aml_decisions(subject_id,report_id,generation,policy_version,risk_class,reason,expires_at,evaluated_at)
 SELECT subject_id,report_id,generation,'history107-policy','high',reason,expires_at,evaluated_at FROM reg_crypto_aml_decisions WHERE policy_version='routing-v1'`)
	require.NoError(t, err)
	before := decisionSnapshot107(t, f)
	_, err = f.repo.ApplyNextAMLDecision(f.ctx, time.Now())
	require.ErrorContains(t, err, "original assessment")
	var count int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM reg_crypto_aml_decision_queue`).Scan(&count))
	require.Equal(t, 1, count)
	require.Equal(t, before, decisionSnapshot107(t, f))
}

func driftInvoice107(t *testing.T, f amlRoutingFixture) id.ID {
	t.Helper()
	invoice := id.New()
	_, err := f.pool.Exec(f.ctx, `INSERT INTO doc_crypto_invoices(id,public_ref,number,date,merchant_id,token_id,expected_amount,expires_at)
 VALUES($1,$2,$3,NOW(),$4,$5,100,NOW()+interval '1 hour')`, invoice, "ci_"+id.Compact(invoice), invoice.String(), f.input.Request.MerchantID, f.input.Request.TokenID)
	require.NoError(t, err)
	return invoice
}

func driftSnapshot107(t *testing.T, f amlRoutingFixture) string {
	t.Helper()
	var data string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT jsonb_build_object('current',(SELECT jsonb_agg(to_jsonb(d) ORDER BY invoice_id) FROM sys_invoice_reconciliation_drift d),
 'incidents',(SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM reg_crypto_security_incidents i WHERE incident_kind='invoice_projection_drift'))::text`).Scan(&data))
	return data
}

func TestInvoiceDriftRetainsFirstBasisAndCreatesNewEpisodeAfterResolution(t *testing.T) {
	f := newAMLRoutingFixture(t, false)
	invoice := driftInvoice107(t, f)
	repo := NewWorkerRuntimeRepo()
	require.NoError(t, repo.RecordInvoiceDrift(f.ctx, invoice, "first failure"))
	var count int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM reg_crypto_security_incidents`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, repo.RecordInvoiceDrift(f.ctx, invoice, "second failure"))
	var incident id.ID
	var basis string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT id,details::text FROM reg_crypto_security_incidents WHERE invoice_id=$1`, invoice).Scan(&incident, &basis))
	require.Contains(t, basis, "first failure")
	require.Contains(t, basis, "second failure")
	_, err := f.pool.Exec(f.ctx, `UPDATE reg_crypto_security_incidents SET status='acknowledged' WHERE id=$1`, incident)
	require.NoError(t, err)
	var acknowledged string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT to_jsonb(i)::text FROM reg_crypto_security_incidents i WHERE id=$1`, incident).Scan(&acknowledged))
	require.NoError(t, repo.RecordInvoiceDrift(f.ctx, invoice, "third failure"))
	var after string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT to_jsonb(i)::text FROM reg_crypto_security_incidents i WHERE id=$1`, incident).Scan(&after))
	require.Equal(t, acknowledged, after, "a later scan preserves basis and acknowledgement")
	var cycles int
	var latest string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT consecutive_cycles,details->>'error' FROM sys_invoice_reconciliation_drift WHERE invoice_id=$1`, invoice).Scan(&cycles, &latest))
	require.Equal(t, 3, cycles)
	require.Equal(t, "third failure", latest)
	require.NoError(t, repo.ResolveInvoiceDrift(f.ctx, invoice))
	closed := driftSnapshot107(t, f)
	require.NoError(t, repo.ResolveInvoiceDrift(f.ctx, invoice))
	require.Equal(t, closed, driftSnapshot107(t, f))
	for _, mutation := range []string{
		`UPDATE reg_crypto_security_incidents SET details='{}' WHERE id=$1`,
		`UPDATE reg_crypto_security_incidents SET status='open' WHERE id=$1`,
		`DELETE FROM reg_crypto_security_incidents WHERE id=$1`,
	} {
		_, err = f.pool.Exec(f.ctx, mutation, incident)
		require.Error(t, err)
	}
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_security_incidents(network_id,invoice_id,incident_kind,tx_hash,details)
 SELECT network_id,invoice_id,incident_kind,tx_hash,details FROM reg_crypto_security_incidents WHERE id=$1 ON CONFLICT DO NOTHING`, incident)
	require.NoError(t, err)
	require.Equal(t, closed, driftSnapshot107(t, f))
	require.NoError(t, repo.RecordInvoiceDrift(f.ctx, invoice, "new first failure"))
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT consecutive_cycles FROM sys_invoice_reconciliation_drift WHERE invoice_id=$1`, invoice).Scan(&cycles))
	require.Equal(t, 1, cycles, "a fresh episode must again pass the two-cycle threshold")
	require.NoError(t, repo.RecordInvoiceDrift(f.ctx, invoice, "new second failure"))
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM reg_crypto_security_incidents WHERE invoice_id=$1`, invoice).Scan(&count))
	require.Equal(t, 2, count)
	var unchanged bool
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT status='resolved' AND details::text=$2 FROM reg_crypto_security_incidents WHERE id=$1`, incident, basis).Scan(&unchanged))
	require.True(t, unchanged)
	before := driftSnapshot107(t, f)
	err = postgres.MustGetTxManager(f.ctx).RunInTransaction(f.ctx, func(ctx context.Context) error {
		if err := repo.RecordInvoiceDrift(ctx, invoice, "rolled back observation"); err != nil {
			return err
		}
		return apperror.NewConflict("synthetic outer rollback")
	})
	require.ErrorContains(t, err, "outer rollback")
	require.Equal(t, before, driftSnapshot107(t, f))
	var movements int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM reg_crypto_balance_movements`).Scan(&movements))
	require.Zero(t, movements)
}

func TestInvoiceDriftSharesNonblockingInvoiceGateAndRejectsForeignLinks(t *testing.T) {
	f := newAMLRoutingFixture(t, false)
	invoice, other := driftInvoice107(t, f), driftInvoice107(t, f)
	repo := NewWorkerRuntimeRepo()
	for _, target := range []id.ID{invoice, other} {
		require.NoError(t, repo.RecordInvoiceDrift(f.ctx, target, "first failure"))
		require.NoError(t, repo.RecordInvoiceDrift(f.ctx, target, "second failure"))
	}
	var incident, foreign id.ID
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT incident_id FROM sys_invoice_reconciliation_drift WHERE invoice_id=$1`, invoice).Scan(&incident))
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT incident_id FROM sys_invoice_reconciliation_drift WHERE invoice_id=$1`, other).Scan(&foreign))
	before := driftSnapshot107(t, f)
	for i, mutation := range []string{
		`UPDATE sys_invoice_reconciliation_drift SET incident_id=$2 WHERE invoice_id=$1`,
		`UPDATE sys_invoice_reconciliation_drift SET initial_details='{}' WHERE invoice_id=$1`,
		`UPDATE sys_invoice_reconciliation_drift SET first_seen_at=first_seen_at-interval '1 day' WHERE invoice_id=$1`,
	} {
		args := []any{invoice}
		if i == 0 {
			args = append(args, foreign)
		}
		_, err := f.pool.Exec(f.ctx, mutation, args...)
		require.ErrorContains(t, err, "first observation is immutable")
	}
	_, err := f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_security_incidents(network_id,invoice_id,incident_kind,tx_hash,details)
 SELECT network_id,$2,'invoice_projection_drift',tx_hash,details FROM reg_crypto_security_incidents WHERE id=$1 ON CONFLICT DO NOTHING`, incident, other)
	require.ErrorContains(t, err, "first observation")
	require.Equal(t, before, driftSnapshot107(t, f))

	tx, err := f.pool.Begin(f.ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(f.ctx) }()
	_, err = tx.Exec(f.ctx, `SELECT id FROM doc_crypto_invoices WHERE id=$1 FOR UPDATE`, invoice)
	require.NoError(t, err)
	require.ErrorContains(t, repo.RecordInvoiceDrift(f.ctx, invoice, "busy record"), "busy")
	require.ErrorContains(t, repo.ResolveInvoiceDrift(f.ctx, invoice), "busy")
	_, err = f.pool.Exec(f.ctx, `UPDATE sys_invoice_reconciliation_drift SET consecutive_cycles=consecutive_cycles+1 WHERE invoice_id=$1`, invoice)
	require.ErrorContains(t, err, "55P03")
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_security_incidents SET status='resolved' WHERE id=$1`, incident)
	require.ErrorContains(t, err, "55P03")
	// Rejected raw writers release the child rows without waiting for the
	// invoice owner. Its own operation can still advance in the same order.
	_, err = tx.Exec(f.ctx, `SELECT invoice_id FROM sys_invoice_reconciliation_drift WHERE invoice_id=$1 FOR UPDATE NOWAIT`, invoice)
	require.NoError(t, err)
	_, err = tx.Exec(f.ctx, `SELECT id FROM reg_crypto_security_incidents WHERE id=$1 FOR UPDATE NOWAIT`, incident)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(f.ctx))
	require.Equal(t, before, driftSnapshot107(t, f))
	require.NoError(t, repo.ResolveInvoiceDrift(f.ctx, invoice))
	var open bool
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT status='open' FROM reg_crypto_security_incidents WHERE id=$1`, foreign).Scan(&open))
	require.True(t, open, "resolving one invoice preserves the other invoice's incident")
}
