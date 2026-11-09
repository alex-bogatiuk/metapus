package register_repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	er "metapus/internal/domain/registers/exchange_rate"
	"metapus/internal/infrastructure/storage/postgres"
)

func (r *ExchangeRateRepo) Claim(ctx context.Context, cfg er.Configuration, token id.ID) (bool, error) {
	txm := postgres.MustGetTxManager(ctx)
	claimed := false
	err := txm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := txm.GetQuerier(ctx)
		if _, err := q.Exec(ctx, `INSERT INTO sys_rate_feed_state(rate_source_id,base_currency_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, sourceID(cfg), cfg.BaseCurrency.ID); err != nil {
			return err
		}
		tag, err := q.Exec(ctx, `UPDATE sys_rate_feed_state SET lease_token=$3,lease_until=clock_timestamp()+interval '120 seconds',last_attempt_at=clock_timestamp(),status='running'
 WHERE rate_source_id=$1 AND base_currency_id=$2 AND next_run_at<=clock_timestamp() AND (lease_until IS NULL OR lease_until<=clock_timestamp())`, sourceID(cfg), cfg.BaseCurrency.ID, token)
		claimed = tag.RowsAffected() == 1
		return err
	})
	return claimed, err
}
func (r *ExchangeRateRepo) Renew(ctx context.Context, cfg er.Configuration, token id.ID) (bool, error) {
	tag, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `UPDATE sys_rate_feed_state SET lease_until=clock_timestamp()+interval '120 seconds' WHERE rate_source_id=$1 AND base_currency_id=$2 AND lease_token=$3 AND lease_until>clock_timestamp()`, sourceID(cfg), cfg.BaseCurrency.ID, token)
	return tag.RowsAffected() == 1, err
}
func (r *ExchangeRateRepo) FeedState(ctx context.Context, source, base id.ID) (er.FeedState, error) {
	s := er.FeedState{SourceID: source, BaseCurrencyID: base, Status: "idle", Problems: []er.Problem{}}
	var problems []byte
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT status,next_run_at,last_attempt_at,last_success_at,requested,received,inserted,repeated,rejected,problems FROM sys_rate_feed_state WHERE rate_source_id=$1 AND base_currency_id=$2`, source, base).Scan(&s.Status, &s.NextRunAt, &s.LastAttemptAt, &s.LastSuccessAt, &s.Requested, &s.Received, &s.Inserted, &s.Repeated, &s.Rejected, &problems)
	if errors.Is(err, pgx.ErrNoRows) {
		var exists bool
		if err = postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cat_rate_sources WHERE id=$1 AND NOT deletion_mark AND _deleted_at IS NULL)`, source).Scan(&exists); err != nil {
			return s, err
		}
		if !exists {
			return s, apperror.NewNotFound("rate_source", source)
		}
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("get feed state: %w", err)
	}
	if err = json.Unmarshal(problems, &s.Problems); err != nil {
		return s, err
	}
	return s, nil
}

func (r *ExchangeRateRepo) Finish(ctx context.Context, cfg er.Configuration, token id.ID, result er.FetchResult) (er.FeedState, error) {
	var state er.FeedState
	txm := postgres.MustGetTxManager(ctx)
	err := txm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := txm.GetQuerier(ctx)
		// Acquiring the row for this token fences both imports and lease renewal.
		tag, err := q.Exec(ctx, `UPDATE sys_rate_feed_state SET _txid=txid_current() WHERE rate_source_id=$1 AND base_currency_id=$2 AND lease_token=$3 AND lease_until>clock_timestamp()`, sourceID(cfg), cfg.BaseCurrency.ID, token)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			var completed bool
			if err = q.QueryRow(ctx, `SELECT COALESCE(last_completed_token=$3,false) FROM sys_rate_feed_state WHERE rate_source_id=$1 AND base_currency_id=$2`, sourceID(cfg), cfg.BaseCurrency.ID, token).Scan(&completed); err != nil {
				return err
			}
			if completed {
				state, err = r.FeedState(ctx, sourceID(cfg), cfg.BaseCurrency.ID)
				return err
			}
			return apperror.NewBusinessRule("RATE_LEASE_LOST", "Попытка загрузки больше не владеет правом записи")
		}
		var unchanged bool
		if err = q.QueryRow(ctx, `SELECT rs.version=$2 AND s.version=$3 AND rs.is_active AND NOT rs.deletion_mark AND rs._deleted_at IS NULL AND rs.source_type='coingecko' AND s.exchange_rates->>'sourceId'=rs.id::text FROM cat_rate_sources rs CROSS JOIN sys_settings s WHERE rs.id=$1 FOR SHARE OF rs,s`, sourceID(cfg), cfg.SourceVersion, cfg.SettingsVersion).Scan(&unchanged); err != nil {
			return err
		}
		state = er.FeedState{SourceID: sourceID(cfg), BaseCurrencyID: cfg.BaseCurrency.ID, Requested: result.Requested, Problems: append([]er.Problem{}, result.Problems...)}
		if !unchanged {
			state.Problems = append(state.Problems, er.Problem{Code: "configuration_changed", Message: "Настройки изменились во время получения курсов"})
		}
		for _, o := range result.Observations {
			if !unchanged {
				continue
			}
			var currentVersion int
			var active bool
			err = q.QueryRow(ctx, `SELECT version,is_active FROM reg_rate_source_mappings WHERE currency_id=$1 AND base_currency_id=$2 AND rate_source_id=$3 FOR SHARE`, o.Mapping.Currency.ID, cfg.BaseCurrency.ID, sourceID(cfg)).Scan(&currentVersion, &active)
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && (!active || currentVersion != o.Mapping.Version)) {
				cid := o.Mapping.Currency.ID
				state.Problems = append(state.Problems, er.Problem{CurrencyID: &cid, Code: "mapping_changed", Message: "Соответствие изменилось во время получения"})
				continue
			}
			if err != nil {
				return err
			}
			if err = er.ValidatePrice(o.Rate, 1); err != nil {
				return err
			}
			if o.ProviderUpdatedAt.IsZero() || o.ProviderUpdatedAt.After(o.ReceivedAt) {
				return apperror.NewValidation("Некорректное время котировки")
			}
			var previous decimal.Decimal
			var version, mappingVersion, multiplier int
			err = q.QueryRow(ctx, `SELECT rate,version,mapping_version,multiplier FROM reg_exchange_rates WHERE currency_id=$1 AND base_currency_id=$2 AND rate_source_id=$3 AND provider_updated_at=$4 ORDER BY version DESC LIMIT 1`, o.Mapping.Currency.ID, cfg.BaseCurrency.ID, sourceID(cfg), o.ProviderUpdatedAt).Scan(&previous, &version, &mappingVersion, &multiplier)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			state.Received++
			if err == nil && previous.Equal(o.Rate) && mappingVersion == o.Mapping.Version && multiplier == 1 {
				state.Repeated++
				continue
			}
			_, err = q.Exec(ctx, `INSERT INTO reg_exchange_rates(id,currency_id,base_currency_id,rate_source_id,rate,multiplier,provider_updated_at,received_at,version,mapping_version,feed_attempt_id) VALUES($1,$2,$3,$4,$5,1,$6,$7,$8,$9,$10)`, id.New(), o.Mapping.Currency.ID, cfg.BaseCurrency.ID, sourceID(cfg), o.Rate, o.ProviderUpdatedAt, o.ReceivedAt, version+1, o.Mapping.Version, token)
			if err != nil {
				return fmt.Errorf("append quote: %w", err)
			}
			state.Inserted++
		}
		state.Rejected = state.Requested - state.Received
		state.Status = "succeeded"
		if state.Rejected > 0 || len(state.Problems) > 0 {
			state.Status = "partial"
		}
		if state.Received == 0 && state.Requested > 0 || !unchanged || state.Requested == 0 && len(state.Problems) > 0 {
			state.Status = "failed"
		}
		next := time.Now().UTC().Add(er.FetchInterval)
		if result.RetryAt != nil && result.RetryAt.After(next) {
			next = *result.RetryAt
		}
		problems, err := json.Marshal(state.Problems)
		if err != nil {
			return err
		}
		tag, err = q.Exec(ctx, `UPDATE sys_rate_feed_state SET lease_token=NULL,lease_until=NULL,last_completed_token=$3,next_run_at=$4,status=$5,requested=$6,received=$7,inserted=$8,repeated=$9,rejected=$10,problems=$11,last_success_at=CASE WHEN $7>0 THEN clock_timestamp() ELSE last_success_at END WHERE rate_source_id=$1 AND base_currency_id=$2 AND lease_token=$3 AND lease_until>clock_timestamp()`, sourceID(cfg), cfg.BaseCurrency.ID, token, next, state.Status, state.Requested, state.Received, state.Inserted, state.Repeated, state.Rejected, problems)
		if err == nil && tag.RowsAffected() != 1 {
			return apperror.NewBusinessRule("RATE_LEASE_LOST", "Срок владения попыткой истёк до завершения записи")
		}
		return err
	})
	if err != nil {
		return state, err
	}
	return r.FeedState(ctx, sourceID(cfg), cfg.BaseCurrency.ID)
}
func (r *ExchangeRateRepo) Fail(ctx context.Context, cfg er.Configuration, token id.ID, message string, requested int) error {
	problems, err := json.Marshal([]er.Problem{{Code: "storage_error", Message: message}})
	if err != nil {
		return err
	}
	_, err = postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `UPDATE sys_rate_feed_state SET lease_token=NULL,lease_until=NULL,next_run_at=clock_timestamp()+interval '300 seconds',status='failed',requested=$5,received=0,inserted=0,repeated=0,rejected=$5,problems=$4 WHERE rate_source_id=$1 AND base_currency_id=$2 AND lease_token=$3 AND lease_until>clock_timestamp()`, sourceID(cfg), cfg.BaseCurrency.ID, token, problems, max(0, requested))
	return err
}
