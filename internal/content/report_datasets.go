package content

import (
	"context"
	"metapus/internal/infrastructure/readpolicy"
	"time"

	"github.com/Masterminds/squirrel"

	reportcontract "metapus/internal/domain/reports/contract"
	"metapus/internal/domain/reports/schema"
)

// ---------------------------------------------------------------------------
// Document Journal Dataset
// ---------------------------------------------------------------------------

// DocumentJournalDataset defines the "Журнал документов" report.
// Sources: crypto_invoice, crypto_payment, crypto_withdrawal, treasury_withdrawal.
var DocumentJournalDataset = schema.Dataset{
	Key:            "document-journal",
	Name:           "Журнал документов",
	Description:    "Все документы системы в хронологическом порядке",
	Permission:     "report:document-journal:read",
	Sources:        journalSources(),
	ImplicitAccess: journalAccess("deletion_mark"),
	Fields: []schema.Field{
		{Name: "id", Access: journalAccess("id"), Label: "ID", Kind: schema.FieldAttribute, Type: schema.TypeString, Hidden: true},
		{Name: "document_type", Access: journalAccess(), Label: "Тип документа", Kind: schema.FieldDimension, Type: schema.TypeString, Sortable: true},
		{Name: "number", Access: journalAccess("number"), Label: "Номер", Kind: schema.FieldAttribute, Type: schema.TypeString, Sortable: true},
		{Name: "date", Access: journalAccess("date"), Label: "Дата", Kind: schema.FieldDimension, Type: schema.TypeDate, Sortable: true},
		{Name: "posted", Access: journalAccess("posted"), Label: "Проведён", Kind: schema.FieldAttribute, Type: schema.TypeBoolean, Sortable: true},
		{Name: "counterparty_name", Access: journalMerchantAccess(), Label: "Мерчант", Kind: schema.FieldAttribute, Type: schema.TypeString, Sortable: true},
		{Name: "total_amount", Access: journalAmountAccess(), Label: "Сумма", Kind: schema.FieldMeasure, Type: schema.TypeMoney, Agg: schema.AggSum, Sortable: true, Money: &reportcontract.MoneySources{AmountType: reportcontract.AmountTypeRaw, CurrencyID: "currency_id", CurrencyCode: "currency", DecimalPlaces: "currency_decimal_places"}},
		{Name: "currency_id", Access: journalRefAccess("token_id", "token", "id"), Label: "ID токена", Kind: schema.FieldDimension, Type: schema.TypeString, Hidden: true},
		{Name: "currency_decimal_places", Access: journalRefAccess("token_id", "token", "decimal_places"), Label: "Точность токена", Kind: schema.FieldDimension, Type: schema.TypeInteger, Hidden: true},
		{Name: "currency", Access: journalRefAccess("token_id", "token", "symbol"), Label: "Токен", Kind: schema.FieldAttribute, Type: schema.TypeString},
		{Name: "description", Access: journalAccess("description"), Label: "Комментарий", Kind: schema.FieldAttribute, Type: schema.TypeString, Hidden: true},
	},
	Filters: []schema.FilterDef{
		{Key: "from_date", Access: journalAccess("date"), Label: "Начало периода", Type: schema.FilterDate},
		{Key: "to_date", Access: journalAccess("date"), Label: "Конец периода", Type: schema.FilterDate},
		{Key: "posted", Access: journalAccess("posted"), Label: "Проведённые", Type: schema.FilterBoolean},
	},
	DefaultSort:   &schema.SortDef{Column: "date", Direction: "desc"},
	ExportFormats: []string{"csv", "xlsx"},
	ExecutorKey:   "document-journal",
}

// journalDocTypes lists all crypto document entity keys used as UNION sources.
var journalDocTypes = []string{
	"crypto_invoice",
	"crypto_payment",
	"crypto_withdrawal",
	"treasury_withdrawal",
}

type documentJournalExecutor struct{}

func (e *documentJournalExecutor) BuildQuery(ctx context.Context, params map[string]any, access readpolicy.ReadConstraints) (squirrel.SelectBuilder, error) {
	sources := make([]squirrel.Sqlizer, 0, len(journalDocTypes))
	for _, key := range journalDocTypes {
		q, err := access.Source(key)
		if err != nil {
			return squirrel.SelectBuilder{}, err
		}
		merchant := "m.name"
		if key == "treasury_withdrawal" {
			merchant = "NULL::text"
		}
		q = q.Columns(
			"d.id",
			"'"+key+"' AS document_type",
			"d.number",
			"d.date",
			"d.posted",
			merchant+" AS counterparty_name",
			"tok.id AS currency_id",
			"tok.symbol AS currency",
			"tok.decimal_places AS currency_decimal_places",
			"d."+journalAmountColumn(key)+" AS total_amount",
			"d.description",
		).Where(squirrel.Eq{"d.deletion_mark": false})

		if fromDate, ok := extractOptionalDate(params, "from_date"); ok {
			q = q.Where(squirrel.GtOrEq{"d.date": fromDate})
		}
		if toDate, ok := extractOptionalDate(params, "to_date"); ok {
			q = q.Where(squirrel.Lt{"d.date": toDate})
		}
		if posted, ok := params["posted"].(bool); ok {
			q = q.Where(squirrel.Eq{"d.posted": posted})
		}
		sources = append(sources, q)
	}

	// Build UNION ALL from all sources dynamically.
	if len(sources) == 0 {
		return squirrel.SelectBuilder{}, nil
	}
	union := sources[0]
	for i := 1; i < len(sources); i++ {
		union = squirrel.Expr("(?) UNION ALL (?)", union, sources[i])
	}
	return squirrel.Select().PrefixExpr(squirrel.Expr("WITH base AS (?)", union)).From("base"), nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func journalSources() []schema.Source {
	sources := make([]schema.Source, 0, len(journalDocTypes))
	for _, key := range journalDocTypes {
		sources = append(sources, schema.Source{
			Key:       key,
			Entity:    key,
			Alias:     "d",
			Relations: journalRelations(key),
		})
	}
	return sources
}

func journalRelations(key string) []schema.SourceRelation {
	relations := []schema.SourceRelation{{Entity: "token", Alias: "tok", Column: "token_id", Required: true}}
	if key != "treasury_withdrawal" {
		relations = append(relations, schema.SourceRelation{Entity: "merchant", Alias: "m", Column: "merchant_id"})
	}
	return relations
}

func journalAmountColumn(key string) string {
	if key == "crypto_invoice" {
		return "expected_amount"
	}
	return "amount"
}

func journalAmountAccess() []schema.FieldAccess {
	access := make([]schema.FieldAccess, 0, len(journalDocTypes))
	for _, key := range journalDocTypes {
		access = append(access, schema.FieldAccess{Entity: key, Fields: []string{journalAmountColumn(key)}})
	}
	return access
}

func journalMerchantAccess() []schema.FieldAccess {
	access := []schema.FieldAccess{{Entity: "merchant", Fields: []string{"name"}}}
	for _, key := range journalDocTypes {
		if key != "treasury_withdrawal" {
			access = append(access, schema.FieldAccess{Entity: key, Fields: []string{"merchant_id"}})
		}
	}
	return access
}

func journalAccess(fields ...string) []schema.FieldAccess {
	access := make([]schema.FieldAccess, 0, len(journalDocTypes))
	for _, key := range journalDocTypes {
		access = append(access, schema.FieldAccess{Entity: key, Fields: fields})
	}
	return access
}

func journalRefAccess(column, entity, field string) []schema.FieldAccess {
	return append(journalAccess(column), schema.FieldAccess{Entity: entity, Fields: []string{field}})
}

func extractOptionalDate(params map[string]any, key string) (time.Time, bool) {
	v, ok := params[key]
	if !ok {
		return time.Time{}, false
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// DatasetExecutors binds SQL implementations to framework-free dataset keys.
func DatasetExecutors() map[string]readpolicy.DatasetExecutor {
	return map[string]readpolicy.DatasetExecutor{"document-journal": &documentJournalExecutor{}}
}
