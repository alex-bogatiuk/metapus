package integration_repo

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/domain/integrations"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/catalog_repo"
)

func (r *Runtime) Status(ctx context.Context, uid id.ID) (integrations.PoolStatus, error) {
	out := integrations.PoolStatus{PoolID: uid.String(), Accounts: []integrations.AccountStatus{}}
	_, err := catalog_repo.NewIntegrationPoolRepo().GetByID(ctx, uid)
	if err != nil {
		return out, err
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var account *id.ID
	err = q.QueryRow(ctx, `SELECT account_id,selection_version,reason FROM sys_integration_pool_state WHERE pool_id=$1`, uid).Scan(&account, &out.SelectionVersion, &out.Reason)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	if account != nil {
		s := account.String()
		out.AccountID = &s
	}
	rows, err := q.Query(ctx, `SELECT a.id,a.name,a.is_active AND NOT a.deletion_mark AND a._deleted_at IS NULL,COALESCE(octet_length(a.credentials_enc)>0,false),a.credentials_version,a.reset_day,a.reset_timezone,a.credentials_expires_at,a.manual_period_started_at,a.manual_period_version,COALESCE(s.manual_period_version,0),s.exhausted_until,s.throttled_until,COALESCE(s.last_error,''),COALESCE(s.invalid_credentials_version,-1),COALESCE(s.invalid_slots,'{}'::text[]),a.provider FROM sys_integration_pool_accounts m JOIN cat_integration_accounts a ON a.id=m.account_id LEFT JOIN sys_integration_account_state s ON s.account_id=a.id WHERE m.pool_id=$1 ORDER BY m.position`, uid)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var state integrations.AccountStatus
		var a integrations.Account
		var invalidVersion int
		var stateManualVersion int64
		if err = rows.Scan(&state.ID, &state.Name, &state.IsActive, &state.HasCredentials, &state.CredentialsVersion, &a.ResetDay, &a.ResetTimezone, &a.CredentialsExpiresAt, &a.ManualPeriodStartedAt, &a.ManualPeriodVersion, &stateManualVersion, &state.ExhaustedUntil, &state.ThrottledUntil, &state.LastError, &invalidVersion, &state.InvalidSlots, &a.Provider); err != nil {
			return out, err
		}
		definition, _ := r.catalog.Definition(a.Provider)
		state.QuotaPolicy = definition.QuotaPolicy
		state.CredentialsExpiresAt, state.ManualPeriodVersion = a.CredentialsExpiresAt, a.ManualPeriodVersion
		if definition.ManualPeriod() && stateManualVersion != a.ManualPeriodVersion {
			state.ExhaustedUntil, state.ThrottledUntil = nil, nil
			state.InvalidSlots = []string{}
			state.LastError = ""
			invalidVersion = -1
		}
		if definition.QuotaPolicy != integrations.QuotaPackage && definition.QuotaPolicy != integrations.QuotaManual {
			_, end, periodErr := quotaPeriod(definition, &a, r.Now())
			if periodErr != nil {
				return out, periodErr
			}
			state.NextResetAt = &end
		}
		if invalidVersion != state.CredentialsVersion {
			state.InvalidSlots = []string{}
			if state.LastError == string(integrations.Unauthorized) {
				state.LastError = ""
			}
		}
		if state.LastError == string(integrations.Exhausted) && (state.ExhaustedUntil == nil || !r.Now().Before(*state.ExhaustedUntil)) {
			state.LastError = ""
		}
		if state.LastError == string(integrations.Throttled) && (state.ThrottledUntil == nil || !r.Now().Before(*state.ThrottledUntil)) {
			state.LastError = ""
		}
		switch {
		case !state.IsActive:
			state.Availability = "disabled"
		case !state.HasCredentials:
			state.Availability = "unconfigured"
		case a.Expired(definition, r.Now()):
			state.Availability = string(integrations.Expired)
			state.LastError = state.Availability
		case state.ExhaustedUntil != nil && (definition.ManualPeriod() || r.Now().Before(*state.ExhaustedUntil)):
			state.Availability = string(integrations.Exhausted)
		case len(state.InvalidSlots) > 0:
			state.Availability = string(integrations.Unauthorized)
		case state.ThrottledUntil != nil && r.Now().Before(*state.ThrottledUntil):
			state.Availability = string(integrations.Throttled)
		default:
			state.Availability = "available"
		}
		if out.AccountID != nil && *out.AccountID == state.ID && state.Availability == string(integrations.Expired) {
			out.AccountID = nil
			out.Reason = string(integrations.Expired)
		}
		out.Accounts = append(out.Accounts, state)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	return out, nil
}
func (r *Runtime) Command(ctx context.Context, uid id.ID, action string, expected int64) error {
	tm := postgres.MustGetTxManager(ctx)
	return tm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := tm.GetQuerier(ctx)
		var selected *id.ID
		var version int64
		if _, err := q.Exec(ctx, `INSERT INTO sys_integration_pool_state(pool_id) VALUES($1) ON CONFLICT DO NOTHING`, uid); err != nil {
			return err
		}
		if err := q.QueryRow(ctx, `SELECT account_id,selection_version FROM sys_integration_pool_state WHERE pool_id=$1 FOR UPDATE`, uid).Scan(&selected, &version); err != nil {
			return err
		}
		if version != expected {
			return apperror.NewConcurrentModification("integration_pool", uid.String())
		}
		pool, err := catalog_repo.NewIntegrationPoolRepo().GetByID(ctx, uid)
		if err != nil {
			return err
		}
		if action == "exhaust" {
			if selected == nil {
				return apperror.NewValidation("Сначала выберите аккаунт")
			}
			a, err := catalog_repo.NewIntegrationAccountRepo().GetByID(ctx, *selected)
			if err != nil {
				return err
			}
			definition, err := r.catalog.Require(a.Provider)
			if err != nil {
				return err
			}
			if a.Expired(definition, r.Now()) {
				return apperror.NewValidation("Срок ключа истёк. Продлите аккаунт новым ключом и датой")
			}
			start, end, err := quotaPeriod(definition, a, r.Now())
			if err != nil {
				return err
			}
			if err := r.Report(ctx, integrations.Selection{PoolID: uid, AccountID: a.ID, CredentialsVersion: a.CredentialsVersion, ManualPeriodVersion: a.ManualPeriodVersion, CredentialsExpiresAt: a.CredentialsExpiresAt, SelectionVersion: version, PeriodStart: start, PeriodEnd: end}, integrations.Exhausted, time.Time{}); err != nil {
				return err
			}
		} else {
			if action == "resume" {
				var resumable int
				if err := q.QueryRow(ctx, `SELECT count(*) FROM cat_integration_accounts a LEFT JOIN sys_integration_account_state s ON s.account_id=a.id AND s.manual_period_version=a.manual_period_version WHERE a.id=ANY($1) AND (a.manual_period_version=0 OR (a.credentials_expires_at>$2 AND s.exhausted_until IS NULL))`, pool.AccountIDs, r.Now()).Scan(&resumable); err != nil {
					return err
				}
				if resumable == 0 {
					return apperror.NewValidation("Для восстановления ручного периода сохраните новый ключ и дату")
				}

				if _, err := q.Exec(ctx, `SELECT account_id FROM sys_integration_account_state WHERE account_id=ANY($1) ORDER BY account_id FOR UPDATE`, pool.AccountIDs); err != nil {
					return err
				}
				if _, err := q.Exec(ctx, `UPDATE sys_integration_account_state SET exhausted_until=CASE WHEN manual_period_version>0 THEN exhausted_until ELSE NULL END,throttled_until=NULL,probe_lease_until=NULL,version=version+1,updated_at=NOW() WHERE account_id=ANY($1)`, pool.AccountIDs); err != nil {
					return err
				}
			}
			if selected != nil {
				for i, aid := range pool.AccountIDs {
					if aid == *selected {
						if _, err := q.Exec(ctx, `UPDATE sys_integration_pool_state SET position=$2 WHERE pool_id=$1`, uid, i); err != nil {
							return err
						}
						break
					}
				}
			}

			if _, err := q.Exec(ctx, `UPDATE sys_integration_pool_state SET account_id=NULL,selection_version=selection_version+1,reason=$2,updated_at=NOW() WHERE pool_id=$1`, uid, "manual_"+action); err != nil {
				return err
			}
		}
		return nil
	})
}
