package register_repo

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/json"
	"github.com/alex-bogatiuk/metapus/internal/domain/eventlog"
	er "github.com/alex-bogatiuk/metapus/internal/domain/registers/exchange_rate"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
)

var _externalCoinID = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,99}$`)

func (r *ExchangeRateRepo) Mappings(ctx context.Context, source, base id.ID) ([]er.Mapping, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var exists bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cat_rate_sources WHERE id=$1 AND NOT deletion_mark AND _deleted_at IS NULL)`, source).Scan(&exists); err != nil {
		return nil, fmt.Errorf("get mapping source: %w", err)
	}
	if !exists {
		return nil, apperror.NewNotFound("rate_source", source)
	}
	rows, err := q.Query(ctx, `SELECT jsonb_build_object('currency',`+_currencyJSON+`,'baseCurrency',`+_baseJSON+`,'sourceId',m.rate_source_id,'externalId',m.external_id,'isActive',m.is_active,'version',m.version)
 FROM reg_rate_source_mappings m JOIN cat_currencies c ON c.id=m.currency_id JOIN cat_currencies b ON b.id=m.base_currency_id
 WHERE m.rate_source_id=$1 AND m.base_currency_id=$2 AND m._deleted_at IS NULL ORDER BY COALESCE(c.iso_code,c.code),c.id`, source, base)
	if err != nil {
		return nil, fmt.Errorf("list mappings: %w", err)
	}
	defer rows.Close()
	out := make([]er.Mapping, 0, 16)
	for rows.Next() {
		var raw []byte
		var m er.Mapping
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *ExchangeRateRepo) PutMapping(ctx context.Context, m er.Mapping, expected int) (er.Mapping, error) {
	user := appctx.GetUser(ctx)
	if user == nil || (!user.IsAdmin && !slices.Contains(user.Permissions, "catalog:rate_source:update")) {
		return m, apperror.NewForbidden("Недостаточно прав для настройки источника")
	}
	if expected < 0 || !_externalCoinID.MatchString(m.ExternalID) || m.Currency.ID == m.BaseCurrency.ID {
		return m, apperror.NewValidation("Укажите валюту и корректный CoinGecko ID")
	}
	txm := postgres.MustGetTxManager(ctx)
	err := txm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := txm.GetQuerier(ctx)
		var sourceType string
		if err := q.QueryRow(ctx, `SELECT source_type FROM cat_rate_sources WHERE id=$1 AND NOT deletion_mark AND _deleted_at IS NULL AND NOT is_folder FOR SHARE`, m.SourceID).Scan(&sourceType); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apperror.NewNotFound("rate_source", m.SourceID)
			}
			return err
		}
		if sourceType != "coingecko" {
			return apperror.NewBusinessRule("RATE_SOURCE_UNSUPPORTED", "В первой версии поддерживается CoinGecko")
		}
		var currency, base []byte
		if err := q.QueryRow(ctx, `SELECT `+_currencyJSON+`,`+_baseJSON+` FROM cat_currencies c CROSS JOIN cat_currencies b WHERE c.id=$1 AND b.id=$2 AND b.iso_code='USD' AND NOT c.is_folder AND NOT b.is_folder AND NOT c.deletion_mark AND NOT b.deletion_mark`, m.Currency.ID, m.BaseCurrency.ID).Scan(&currency, &base); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apperror.NewValidation("Выберите активную валюту и USD для пересчёта")
			}
			return err
		}
		if err := json.Unmarshal(currency, &m.Currency); err != nil {
			return err
		}
		if err := json.Unmarshal(base, &m.BaseCurrency); err != nil {
			return err
		}
		created := time.Now().UTC()
		var previous json.RawMessage
		if expected > 0 {
			err := q.QueryRow(ctx, `DELETE FROM reg_rate_source_mappings WHERE currency_id=$1 AND base_currency_id=$2 AND rate_source_id=$3 AND version=$4 RETURNING created_at,to_jsonb(reg_rate_source_mappings)`, m.Currency.ID, m.BaseCurrency.ID, m.SourceID, expected).Scan(&created, &previous)
			if errors.Is(err, pgx.ErrNoRows) {
				return apperror.NewConcurrentModification("rate_source_mapping", m.Currency.ID)
			}
			if err != nil {
				return err
			}
		}
		m.Version = expected + 1
		tag, err := q.Exec(ctx, `INSERT INTO reg_rate_source_mappings(currency_id,base_currency_id,rate_source_id,external_id,is_active,version,created_at) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`, m.Currency.ID, m.BaseCurrency.ID, m.SourceID, m.ExternalID, m.IsActive, m.Version, created)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return apperror.NewConcurrentModification("rate_source_mapping", m.Currency.ID)
		}
		return postgres.NewEventLogRepo().Write(ctx, eventlog.Event{Category: eventlog.CategoryData, Severity: eventlog.SeverityInfo, EventType: "exchange_rate.mapping_changed", Source: "api", EntityType: "rate_source", EntityID: &m.SourceID, Message: "Изменено соответствие валюты CoinGecko", Details: map[string]any{"before": previous, "after": m}})
	})
	return m, err
}
