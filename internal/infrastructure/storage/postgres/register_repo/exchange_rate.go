package register_repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/json"
	er "github.com/alex-bogatiuk/metapus/internal/domain/registers/exchange_rate"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
)

type ExchangeRateRepo struct{}

func NewExchangeRateRepo() *ExchangeRateRepo { return &ExchangeRateRepo{} }

func RateSnapshot(ctx context.Context, fn func(context.Context) error) error {
	opts := postgres.DefaultTxOptions()
	opts.IsolationLevel, opts.AccessMode = pgx.RepeatableRead, pgx.ReadOnly
	return postgres.MustGetTxManager(ctx).RunInTransactionWithOptions(ctx, opts, fn)
}

const _currencyJSON = `jsonb_build_object('id',c.id,'code',COALESCE(c.iso_code,c.code),'name',c.name,'decimalPlaces',c.decimal_places)`
const _baseJSON = `jsonb_build_object('id',b.id,'code',COALESCE(b.iso_code,b.code),'name',b.name,'decimalPlaces',b.decimal_places)`
const _sourceJSON = `jsonb_build_object('id',rs.id,'code',rs.code,'name',rs.name,'sourceType',rs.source_type)`
const _quoteJSON = `jsonb_build_object('id',r.id,'currency',` + _currencyJSON + `,'baseCurrency',` + _baseJSON + `,'source',` + _sourceJSON + `,
 'rate',r.rate::text,'multiplier',r.multiplier,'providerUpdatedAt',r.provider_updated_at,'receivedAt',r.received_at,
 'recordedAt',r.recorded_at,'version',r.version,'mappingVersion',r.mapping_version,'feedAttemptId',r.feed_attempt_id)`
const _quoteJoins = ` JOIN cat_currencies c ON c.id=r.currency_id JOIN cat_currencies b ON b.id=r.base_currency_id JOIN cat_rate_sources rs ON rs.id=r.rate_source_id `

func (r *ExchangeRateRepo) LoadConfiguration(ctx context.Context) (er.Configuration, error) {
	var cfg er.Configuration
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var base []byte
	if err := q.QueryRow(ctx, `SELECT `+_currencyJSON+` FROM cat_currencies c WHERE c.iso_code='USD' AND NOT c.deletion_mark AND c._deleted_at IS NULL AND NOT c.is_folder ORDER BY c.id LIMIT 1`).Scan(&base); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return cfg, apperror.NewBusinessRule("RATE_BASE_CURRENCY_MISSING", "В справочнике отсутствует USD")
		}
		return cfg, fmt.Errorf("load reporting currency: %w", err)
	}
	if err := json.Unmarshal(base, &cfg.BaseCurrency); err != nil {
		return cfg, fmt.Errorf("decode reporting currency: %w", err)
	}
	var source []byte
	err := q.QueryRow(ctx, `SELECT s.version, CASE WHEN rs.id IS NULL THEN NULL ELSE `+_sourceJSON+` END,
 COALESCE(rs.is_active AND NOT rs.deletion_mark AND rs._deleted_at IS NULL AND NOT rs.is_folder AND rs.source_type='coingecko',false),COALESCE(rs.version,0),COALESCE(rs.rate_limit_rpm,1),COALESCE(rs.api_key,''),rs.integration_pool_id
 FROM sys_settings s LEFT JOIN cat_rate_sources rs ON rs.id::text=s.exchange_rates->>'sourceId' WHERE s.singleton`).Scan(&cfg.SettingsVersion, &source, &cfg.SourceActive, &cfg.SourceVersion, &cfg.RateLimitRPM, &cfg.APIKey, &cfg.IntegrationPoolID)
	if err != nil {
		return cfg, fmt.Errorf("load rate configuration: %w", err)
	}
	if source != nil {
		if err := json.Unmarshal(source, &cfg.Source); err != nil {
			return cfg, fmt.Errorf("decode source: %w", err)
		}
	}
	return cfg, nil
}

const _candidateSelect = `SELECT ` + _currencyJSON + `,
 CASE WHEN m.currency_id IS NULL THEN NULL ELSE jsonb_build_object('currency',` + _currencyJSON + `,'baseCurrency',` + _baseJSON + `,'sourceId',m.rate_source_id,'externalId',m.external_id,'isActive',m.is_active,'version',m.version) END,
 CASE WHEN r.id IS NULL THEN NULL ELSE ` + _quoteJSON + ` END
 FROM cat_currencies c CROSS JOIN cat_currencies b
 LEFT JOIN cat_rate_sources rs ON rs.id=$2
 LEFT JOIN reg_rate_source_mappings m ON m.currency_id=c.id AND m.base_currency_id=b.id AND m.rate_source_id=rs.id AND m._deleted_at IS NULL
 LEFT JOIN LATERAL (SELECT * FROM reg_exchange_rates e WHERE e.currency_id=c.id AND e.base_currency_id=b.id AND e.rate_source_id=rs.id AND e.provider_updated_at<=$3 ORDER BY e.provider_updated_at DESC,e.version DESC LIMIT 1) r ON true
 WHERE b.id=$1 `

func scanCandidates(rows pgx.Rows) ([]er.Candidate, error) {
	defer rows.Close()
	out := make([]er.Candidate, 0, 50)
	for rows.Next() {
		var c er.Candidate
		var currency, mapping, quote []byte
		if err := rows.Scan(&currency, &mapping, &quote); err != nil {
			return nil, fmt.Errorf("scan rate candidate: %w", err)
		}
		if err := json.Unmarshal(currency, &c.Currency); err != nil {
			return nil, err
		}
		if mapping != nil {
			if err := json.Unmarshal(mapping, &c.Mapping); err != nil {
				return nil, err
			}
		}
		if quote != nil {
			if err := json.Unmarshal(quote, &c.Quote); err != nil {
				return nil, err
			}
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func sourceID(cfg er.Configuration) id.ID {
	if cfg.Source == nil {
		return id.Nil()
	}
	return cfg.Source.ID
}
func (r *ExchangeRateRepo) Candidates(ctx context.Context, cfg er.Configuration, ids []id.ID, at time.Time) ([]er.Candidate, error) {
	rows, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, _candidateSelect+` AND c.id=ANY($4) AND NOT c.deletion_mark AND c._deleted_at IS NULL AND NOT c.is_folder`, cfg.BaseCurrency.ID, sourceID(cfg), at, ids)
	if err != nil {
		return nil, fmt.Errorf("query rate candidates: %w", err)
	}
	return scanCandidates(rows)
}

// Selection predicates match the pure resolver; filtering happens before paging.
const _statusSQL = `CASE WHEN c.id=b.id THEN 'identity'
 WHEN rs.id IS NULL THEN 'missing' WHEN NOT $4 THEN 'disabled'
 WHEN m.currency_id IS NULL THEN 'missing' WHEN NOT m.is_active THEN 'disabled'
 WHEN r.id IS NULL OR r.mapping_version<>m.version THEN 'missing'
 WHEN r.provider_updated_at<=($3::timestamptz-interval '900 seconds') THEN 'stale' ELSE 'fresh' END`

func (r *ExchangeRateRepo) Current(ctx context.Context, cfg er.Configuration, f er.CurrentFilter, at time.Time) ([]er.Candidate, error) {
	rows, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, _candidateSelect+`
 AND NOT c.deletion_mark AND c._deleted_at IS NULL AND NOT c.is_folder
 AND ($5::uuid IS NULL OR c.id=$5) AND ($6='' OR (`+_statusSQL+`)=$6)
 AND ($7='' OR (COALESCE(c.iso_code,c.code),c.id)>($7,$8::uuid))
 ORDER BY COALESCE(c.iso_code,c.code),c.id LIMIT $9`, cfg.BaseCurrency.ID, sourceID(cfg), at, cfg.SourceActive, f.CurrencyID, string(f.Status), f.AfterCode, f.AfterID, f.Limit)
	if err != nil {
		return nil, fmt.Errorf("query current rates: %w", err)
	}
	return scanCandidates(rows)
}
func (r *ExchangeRateRepo) GetQuote(ctx context.Context, quoteID id.ID) (*er.Quote, error) {
	var raw []byte
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT `+_quoteJSON+` FROM reg_exchange_rates r `+_quoteJoins+` WHERE r.id=$1`, quoteID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperror.NewNotFound("exchange_rate", quoteID)
	}
	if err != nil {
		return nil, fmt.Errorf("get quote: %w", err)
	}
	var q er.Quote
	if err = json.Unmarshal(raw, &q); err != nil {
		return nil, err
	}
	return &q, nil
}
func (r *ExchangeRateRepo) History(ctx context.Context, f er.HistoryFilter) ([]er.Quote, error) {
	rows, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, `SELECT `+_quoteJSON+` FROM reg_exchange_rates r `+_quoteJoins+`
 WHERE r.currency_id=$1 AND r.base_currency_id=$2 AND ($3::uuid IS NULL OR r.rate_source_id=$3)
 AND r.provider_updated_at >=$4 AND r.provider_updated_at<$5
 AND ($6::timestamptz IS NULL OR (r.provider_updated_at,r.version,r.id)<($6,$7,$8::uuid))
 ORDER BY r.provider_updated_at DESC,r.version DESC,r.id DESC LIMIT $9`, f.CurrencyID, f.BaseCurrencyID, f.SourceID, f.From, f.To, f.AfterTime, f.AfterVersion, f.AfterID, f.Limit)
	if err != nil {
		return nil, fmt.Errorf("query rate history: %w", err)
	}
	defer rows.Close()
	out := make([]er.Quote, 0, f.Limit)
	for rows.Next() {
		var raw []byte
		var q er.Quote
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &q); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}
