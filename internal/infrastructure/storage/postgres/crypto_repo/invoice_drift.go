package crypto_repo

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
)

// Invoice -> current drift -> incident is the order for both commands. The
// invoice gate never waits, including when a caller already holds other rows.
func lockInvoiceDrift(ctx context.Context, invoice id.ID) (id.ID, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var network id.ID
	err := q.QueryRow(ctx, `SELECT fn_crypto_lock_invoice_drift($1)`, invoice).Scan(&network)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "55P03":
			return network, apperror.NewConflict("invoice reconciliation is busy; retry the operation")
		case "23503":
			return network, apperror.NewNotFound("invoice", invoice)
		case "23514":
			return network, apperror.NewValidation("invoice drift requires read committed isolation")
		}
	}
	return network, err
}

// invoiceDriftKey identifies the incident of one drift episode of an invoice.
func invoiceDriftKey(invoice, episode id.ID) string {
	return invoice.String() + ":" + episode.String()
}

func findInvoiceDriftIncident(ctx context.Context, invoice, network id.ID, key string) (*id.ID, string, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var incident id.ID
	var state string
	var owned bool
	err := q.QueryRow(ctx, `SELECT id,status,COALESCE(invoice_id=$3,FALSE) AND payment_id IS NULL FROM reg_crypto_security_incidents
 WHERE network_id=$1 AND incident_kind='invoice_projection_drift' AND tx_hash=$2 AND event_kind='native_value' AND event_index=0 FOR UPDATE`, network, key, invoice).Scan(&incident, &state, &owned)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	if !owned {
		return nil, "", apperror.NewConflict("invoice drift incident belongs to another invoice")
	}
	return &incident, state, nil
}

func (r *WorkerRuntimeRepo) RecordInvoiceDrift(ctx context.Context, invoice id.ID, reason string) error {
	if id.IsNil(invoice) || strings.TrimSpace(reason) == "" {
		return apperror.NewValidation("invoice drift requires invoice and reason")
	}
	tm := postgres.MustGetTxManager(ctx)
	return tm.RunInTransaction(ctx, func(ctx context.Context) error {
		network, err := lockInvoiceDrift(ctx, invoice)
		if err != nil {
			return err
		}
		q := tm.GetQuerier(ctx)
		var cycles int
		var episode id.ID
		var linked *id.ID
		err = q.QueryRow(ctx, `INSERT INTO sys_invoice_reconciliation_drift(invoice_id,details,initial_details,episode_id,first_seen_at,last_seen_at)
 VALUES($1,jsonb_build_object('error',$2::text),jsonb_build_object('error',$2::text),$3,clock_timestamp(),clock_timestamp())
 ON CONFLICT(invoice_id) DO UPDATE SET
 consecutive_cycles=CASE WHEN sys_invoice_reconciliation_drift.resolved_at IS NULL THEN sys_invoice_reconciliation_drift.consecutive_cycles+1 ELSE 1 END,
 details=EXCLUDED.details,last_seen_at=clock_timestamp(),resolved_at=NULL,
 first_seen_at=CASE WHEN sys_invoice_reconciliation_drift.resolved_at IS NULL THEN sys_invoice_reconciliation_drift.first_seen_at ELSE EXCLUDED.first_seen_at END,
 initial_details=CASE WHEN sys_invoice_reconciliation_drift.resolved_at IS NULL THEN sys_invoice_reconciliation_drift.initial_details ELSE EXCLUDED.initial_details END,
 episode_id=CASE WHEN sys_invoice_reconciliation_drift.resolved_at IS NULL THEN sys_invoice_reconciliation_drift.episode_id ELSE EXCLUDED.episode_id END,
 incident_id=CASE WHEN sys_invoice_reconciliation_drift.resolved_at IS NULL THEN sys_invoice_reconciliation_drift.incident_id ELSE NULL END
 RETURNING consecutive_cycles,episode_id,incident_id`, invoice, reason, id.New()).Scan(&cycles, &episode, &linked)
		if err != nil || cycles < 2 {
			return err
		}
		key := invoiceDriftKey(invoice, episode)
		incident, state, err := findInvoiceDriftIncident(ctx, invoice, network, key)
		if err != nil {
			return err
		}
		if linked != nil && (incident == nil || *linked != *incident) {
			return apperror.NewConflict("invoice drift incident link changed")
		}
		if state == "resolved" {
			return apperror.NewConflict("resolved invoice drift requires a new episode")
		}
		if incident == nil {
			created := id.New()
			_, err := q.Exec(ctx, `INSERT INTO reg_crypto_security_incidents(id,network_id,invoice_id,incident_kind,tx_hash,details)
 VALUES($1,$2,$3,'invoice_projection_drift',$4,fn_crypto_invoice_drift_details($3))`, created, network, invoice, key)
			if err != nil {
				return err
			}
			incident = &created
		}
		// Later observations update the current projection, never this incident's
		// first details or its acknowledged state.
		if linked == nil {
			_, err = q.Exec(ctx, `UPDATE sys_invoice_reconciliation_drift SET incident_id=$2 WHERE invoice_id=$1`, invoice, *incident)
		}
		return err
	})
}

func (r *WorkerRuntimeRepo) ResolveInvoiceDrift(ctx context.Context, invoice id.ID) error {
	if id.IsNil(invoice) {
		return apperror.NewValidation("invoice drift requires invoice")
	}
	tm := postgres.MustGetTxManager(ctx)
	return tm.RunInTransaction(ctx, func(ctx context.Context) error {
		network, err := lockInvoiceDrift(ctx, invoice)
		if err != nil {
			return err
		}
		q := tm.GetQuerier(ctx)
		var episode id.ID
		var linked *id.ID
		err = q.QueryRow(ctx, `SELECT episode_id,incident_id FROM sys_invoice_reconciliation_drift WHERE invoice_id=$1 FOR UPDATE`, invoice).Scan(&episode, &linked)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		incident, state, err := findInvoiceDriftIncident(ctx, invoice, network, invoiceDriftKey(invoice, episode))
		if err != nil {
			return err
		}
		if linked != nil && (incident == nil || *linked != *incident) {
			return apperror.NewConflict("invoice drift incident link changed")
		}
		if incident != nil && state != "resolved" {
			if _, err := q.Exec(ctx, `UPDATE reg_crypto_security_incidents SET status='resolved',resolved_at=clock_timestamp() WHERE id=$1 AND status IN ('open','acknowledged')`, *incident); err != nil {
				return err
			}
		}
		_, err = q.Exec(ctx, `UPDATE sys_invoice_reconciliation_drift SET resolved_at=clock_timestamp() WHERE invoice_id=$1 AND resolved_at IS NULL`, invoice)
		return err
	})
}
