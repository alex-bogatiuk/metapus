// Package integration_repo coordinates account selection in a tenant database.
package integration_repo

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"metapus/internal/core/apperror"
	corecrypto "metapus/internal/core/crypto"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
)

// Runtime selects pool accounts under the provider rules of its catalog.
type Runtime struct {
	Now     func() time.Time
	catalog *integrations.Catalog
}

func NewRuntime(catalog *integrations.Catalog) *Runtime {
	if catalog == nil {
		panic("integration_repo.NewRuntime: catalog is required")
	}
	return &Runtime{Now: func() time.Time { return time.Now().UTC() }, catalog: catalog}
}

type candidate struct {
	account                     integrations.Account
	position                    int
	exhausted, throttled, probe *time.Time
	stateManualVersion          int64
	invalidVersion              int
	invalidSlots                []string
}

func (r *Runtime) Select(ctx context.Context, poolID id.ID, slot string, provider integrations.Provider) (integrations.Selection, error) {
	definition, err := r.catalog.Require(provider)
	if err != nil {
		return integrations.Selection{}, err
	}
	if definition.PoolSelectionDenied != "" {
		return integrations.Selection{}, apperror.NewValidation(definition.PoolSelectionDenied)
	}
	if slot == "" {
		slot = "default"
	}
	var out integrations.Selection
	tm := postgres.MustGetTxManager(ctx)
	err = tm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := tm.GetQuerier(ctx)
		var actual integrations.Provider
		var poolVersion int
		if err := q.QueryRow(ctx, `SELECT provider,version FROM cat_integration_pools WHERE id=$1 AND is_active AND NOT deletion_mark AND _deleted_at IS NULL`, poolID).Scan(&actual, &poolVersion); err != nil {
			if err == pgx.ErrNoRows {
				return apperror.NewValidation("Пул интеграции отсутствует или отключён")
			}
			return fmt.Errorf("load integration pool: %w", err)
		}
		if actual != provider {
			return apperror.NewValidation("Провайдер пула не соответствует подключению")
		}
		if _, err := q.Exec(ctx, `INSERT INTO sys_integration_pool_state(pool_id) VALUES($1) ON CONFLICT DO NOTHING`, poolID); err != nil {
			return fmt.Errorf("initialize pool selection: %w", err)
		}
		// Short pool-local serialization. No network operation is performed here.
		var current *id.ID
		var pos int
		var selectionVersion int64
		if err := q.QueryRow(ctx, `SELECT account_id,position,selection_version FROM sys_integration_pool_state WHERE pool_id=$1 FOR UPDATE`, poolID).Scan(&current, &pos, &selectionVersion); err != nil {
			return fmt.Errorf("lock pool selection: %w", err)
		}
		rows, err := q.Query(ctx, `SELECT a.id,a.name,a.provider,a.version,a.credentials_enc,a.credentials_version,a.reset_day,a.reset_timezone,a.credentials_expires_at,a.manual_period_started_at,a.manual_period_version,COALESCE(s.manual_period_version,0),m.position,s.exhausted_until,s.throttled_until,s.probe_lease_until,COALESCE(s.invalid_credentials_version,-1),COALESCE(s.invalid_slots,'{}'::text[]) FROM sys_integration_pool_accounts m JOIN cat_integration_accounts a ON a.id=m.account_id LEFT JOIN sys_integration_account_state s ON s.account_id=a.id WHERE m.pool_id=$1 AND a.provider=$2 AND a.is_active AND NOT a.deletion_mark AND a._deleted_at IS NULL ORDER BY m.position`, poolID, provider)
		if err != nil {
			return fmt.Errorf("load available integration accounts: %w", err)
		}
		candidates := make([]candidate, 0, 8)
		for rows.Next() {
			var c candidate
			if err := rows.Scan(&c.account.ID, &c.account.Name, &c.account.Provider, &c.account.Version, &c.account.CredentialsEnc, &c.account.CredentialsVersion, &c.account.ResetDay, &c.account.ResetTimezone, &c.account.CredentialsExpiresAt, &c.account.ManualPeriodStartedAt, &c.account.ManualPeriodVersion, &c.stateManualVersion, &c.position, &c.exhausted, &c.throttled, &c.probe, &c.invalidVersion, &c.invalidSlots); err != nil {
				rows.Close()
				return fmt.Errorf("scan integration account: %w", err)
			}
			candidates = append(candidates, c)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		// Keep the current account while healthy; otherwise continue after its position.
		start := 0
		for i, c := range candidates {
			if c.position > pos {
				start = i
				break
			}
		}
		if current != nil {
			for i, c := range candidates {
				if c.account.ID == *current {
					start = i
					break
				}
			}
		}
		now := r.Now()
		var key []byte
		var unavailable integrations.PoolUnavailableError
		exhaustedCount, expiredCount, missingKeyCount := 0, 0, 0
		noteRetry := func(at time.Time) {
			if at.After(now) && (unavailable.RetryAt.IsZero() || at.Before(unavailable.RetryAt)) {
				unavailable.RetryAt = at
			}
		}
		for n := 0; n < len(candidates); n++ {
			c := candidates[(start+n)%len(candidates)]
			a := c.account
			if definition.ManualPeriod() {
				if a.Expired(definition, now) {
					expiredCount++
					continue
				}
				if c.stateManualVersion != a.ManualPeriodVersion {
					c.exhausted, c.throttled, c.probe = nil, nil, nil
					c.invalidVersion = -1
					c.invalidSlots = nil
				}
			}
			if c.exhausted != nil && (definition.ManualPeriod() || now.Before(*c.exhausted)) {
				exhaustedCount++
				if !definition.ManualPeriod() {
					noteRetry(*c.exhausted)
				}
				continue
			}
			if c.throttled != nil && now.Before(*c.throttled) {
				unavailable.Reason = integrations.Throttled
				noteRetry(*c.throttled)
				continue
			}
			if len(a.CredentialsEnc) == 0 {
				continue
			}
			invalid := false
			if c.invalidVersion == a.CredentialsVersion {
				for _, s := range c.invalidSlots {
					if s == slot {
						invalid = true
					}
				}
			}
			if invalid {
				continue
			}
			begin, end, err := quotaPeriod(definition, &a, now)
			if err != nil {
				return err
			}

			if key == nil {
				key, err = catalog_repo.IntegrationEncryptionKey()
				if err != nil {
					return err
				}
			}
			raw, err := corecrypto.Decrypt(a.CredentialsEnc, key)
			if err != nil {
				return apperror.NewInternal(fmt.Errorf("decrypt integration account credentials"))
			}
			var credentials integrations.Credentials
			if err := json.Unmarshal(raw, &credentials); err != nil {
				return apperror.NewInternal(fmt.Errorf("decode integration account credentials"))
			}
			secret, ok := credentials.Key(slot)
			if !ok {
				missingKeyCount++
				continue
			}
			if c.exhausted != nil && !now.Before(*c.exhausted) {
				if c.probe != nil && now.Before(*c.probe) {
					noteRetry(*c.probe)
					continue
				}
				tag, err := q.Exec(ctx, `UPDATE sys_integration_account_state SET probe_lease_until=$2 WHERE account_id=$1 AND (probe_lease_until IS NULL OR probe_lease_until<=$3)`, a.ID, now.Add(30*time.Second), now)
				if err != nil {
					return err
				}
				if tag.RowsAffected() == 0 {
					continue
				}
			}
			if current == nil || *current != a.ID || pos != c.position {
				selectionVersion++
				if _, err := q.Exec(ctx, `UPDATE sys_integration_pool_state SET account_id=$2,position=$3,selection_version=$4,reason='account_selected',updated_at=$5 WHERE pool_id=$1`, poolID, a.ID, c.position, selectionVersion, now); err != nil {
					return err
				}
			}
			out = integrations.Selection{PoolID: poolID, AccountID: a.ID, AccountName: a.Name, Provider: provider, PoolVersion: poolVersion, AccountVersion: a.Version, CredentialsVersion: a.CredentialsVersion, SelectionVersion: selectionVersion, PeriodStart: begin, PeriodEnd: end, Slot: slot, Key: secret, Recovery: c.exhausted != nil && !definition.ManualPeriod(), CredentialsExpiresAt: a.CredentialsExpiresAt, ManualPeriodVersion: a.ManualPeriodVersion}
			return nil
		}
		if len(candidates) > 0 && expiredCount == len(candidates) {
			unavailable.Reason = integrations.Expired
		}
		if len(candidates) > 0 && exhaustedCount == len(candidates) {
			unavailable.Reason = integrations.Exhausted
		}
		if unavailable.Reason == "" && missingKeyCount > 0 {
			unavailable.Reason = integrations.KeyMissing
		}
		return &unavailable
	})
	return out, err
}

func (r *Runtime) Report(ctx context.Context, s integrations.Selection, f integrations.Failure, retry time.Time) error {
	if f != "" && f != integrations.Exhausted && f != integrations.Throttled && f != integrations.Unauthorized {
		return apperror.NewValidation("Неизвестное состояние аккаунта")
	}
	now := r.Now()
	if !now.Before(s.PeriodEnd) {
		return nil
	}
	if retry.IsZero() {
		retry = now.Add(5 * time.Second)
	}
	tm := postgres.MustGetTxManager(ctx)
	return tm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := tm.GetQuerier(ctx)
		// Use the same pool -> account-state lock order as Select and manual commands.
		var locked int
		if err := q.QueryRow(ctx, `SELECT 1 FROM sys_integration_pool_state WHERE pool_id=$1 FOR UPDATE`, s.PoolID).Scan(&locked); err != nil {
			return fmt.Errorf("lock observed pool: %w", err)
		}
		var credentialVersion int
		var manualVersion int64
		var expires, start *time.Time
		if err := q.QueryRow(ctx, `SELECT credentials_version,manual_period_version,credentials_expires_at,manual_period_started_at FROM cat_integration_accounts WHERE id=$1 FOR SHARE`, s.AccountID).Scan(&credentialVersion, &manualVersion, &expires, &start); err != nil {
			return err
		}
		if credentialVersion != s.CredentialsVersion || manualVersion != s.ManualPeriodVersion {
			return nil
		}
		if manualVersion > 0 && (expires == nil || start == nil || !now.Before(*expires) || !s.PeriodEnd.Equal(*expires) || !s.PeriodStart.Equal(*start)) {
			return nil
		}
		var exhausted, throttled *time.Time
		invalid := -1
		slots := []string{}
		if f == integrations.Exhausted {
			until := s.PeriodEnd
			definition, err := r.catalog.Require(s.Provider)
			if err != nil {
				return err
			}
			if delay := definition.ExhaustionRecheck; delay > 0 && now.Add(delay).Before(until) {
				until = now.Add(delay)
			}
			exhausted = &until
		}
		if f == integrations.Throttled {
			throttled = &retry
		}
		if f == integrations.Unauthorized {
			invalid = s.CredentialsVersion
			slots = []string{s.Slot}
		}
		tag, err := q.Exec(ctx, `INSERT INTO sys_integration_account_state(account_id,period_start,period_end,exhausted_until,throttled_until,invalid_credentials_version,invalid_slots,last_error,manual_period_version)
 SELECT id,$2,$3,$4,$5,$6,$7,$8,$10 FROM cat_integration_accounts WHERE id=$1 AND credentials_version=$9 AND manual_period_version=$10
 ON CONFLICT(account_id) DO UPDATE SET
 period_start=EXCLUDED.period_start,period_end=EXCLUDED.period_end,
 exhausted_until=CASE WHEN sys_integration_account_state.manual_period_version<>EXCLUDED.manual_period_version THEN EXCLUDED.exhausted_until WHEN $8='' AND EXCLUDED.manual_period_version=0 THEN NULL WHEN EXCLUDED.exhausted_until IS NOT NULL THEN EXCLUDED.exhausted_until ELSE sys_integration_account_state.exhausted_until END,
 throttled_until=EXCLUDED.throttled_until,probe_lease_until=NULL,
 invalid_credentials_version=CASE WHEN sys_integration_account_state.manual_period_version<>EXCLUDED.manual_period_version THEN EXCLUDED.invalid_credentials_version WHEN $6>=0 THEN $6 ELSE sys_integration_account_state.invalid_credentials_version END,
 invalid_slots=CASE WHEN sys_integration_account_state.manual_period_version<>EXCLUDED.manual_period_version THEN EXCLUDED.invalid_slots WHEN $6>=0 THEN CASE WHEN sys_integration_account_state.invalid_credentials_version=$6 THEN ARRAY(SELECT DISTINCT unnest(sys_integration_account_state.invalid_slots||$7::text[])) ELSE $7 END ELSE sys_integration_account_state.invalid_slots END,
 last_error=$8,manual_period_version=EXCLUDED.manual_period_version,version=sys_integration_account_state.version+1,updated_at=NOW()
 WHERE sys_integration_account_state.manual_period_version<EXCLUDED.manual_period_version OR (sys_integration_account_state.manual_period_version=EXCLUDED.manual_period_version AND sys_integration_account_state.period_start<=EXCLUDED.period_start)`, s.AccountID, s.PeriodStart, s.PeriodEnd, exhausted, throttled, invalid, slots, string(f), s.CredentialsVersion, s.ManualPeriodVersion)
		if err != nil {
			return fmt.Errorf("record integration account result: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return nil
		}
		if f != "" {
			_, err = q.Exec(ctx, `UPDATE sys_integration_pool_state SET account_id=NULL,selection_version=selection_version+1,reason=$4,updated_at=NOW() WHERE pool_id=$1 AND account_id=$2 AND selection_version=$3`, s.PoolID, s.AccountID, s.SelectionVersion, string(f))
			if err != nil {
				return fmt.Errorf("advance integration pool: %w", err)
			}
		}
		return nil
	})
}
