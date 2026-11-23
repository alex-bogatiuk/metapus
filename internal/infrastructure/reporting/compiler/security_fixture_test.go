package compiler_test

import (
	"context"

	"github.com/Masterminds/squirrel"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/security"
	reportcontract "github.com/alex-bogatiuk/metapus/internal/domain/reports/contract"
	"github.com/alex-bogatiuk/metapus/internal/domain/reports/schema"
	"github.com/alex-bogatiuk/metapus/internal/domain/security_profile"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/readpolicy"
	"github.com/alex-bogatiuk/metapus/internal/metadata"
)

// Independent sources exercise the compiler's security contract without tying
// policy coverage to the lifecycle of any particular business document.
type reportSecurityRow struct {
	ID             id.ID  `db:"id" json:"id"`
	Number         string `db:"number" json:"number"`
	OrganizationID *id.ID `db:"organization_id" json:"organizationId"`
	CurrencyID     id.ID  `db:"currency_id" json:"currencyId"`
	Amount         int64  `db:"amount" json:"amount"`
}
type reportSecurityCurrency struct {
	ID            id.ID  `db:"id" json:"id"`
	Code          string `db:"code" json:"code"`
	DecimalPlaces int    `db:"decimal_places" json:"decimalPlaces"`
}
type securityUnionExecutor struct{}

func (securityUnionExecutor) BuildQuery(_ context.Context, _ map[string]any, constraints readpolicy.ReadConstraints) (squirrel.SelectBuilder, error) {
	a, err := constraints.Source("a")
	if err != nil {
		return squirrel.SelectBuilder{}, err
	}
	b, err := constraints.Source("b")
	if err != nil {
		return squirrel.SelectBuilder{}, err
	}
	columns := []string{"d.number", "d.amount", "c.id AS currency_id", "c.code AS currency_code", "c.decimal_places"}
	union := squirrel.Expr("(?) UNION ALL (?)", a.Columns(columns...), b.Columns(columns...))
	return squirrel.Select().PrefixExpr(squirrel.Expr("WITH base AS (?)", union)).From("base"), nil
}

// securityFixture is a dataset over two document sources (a, b) that share a
// currency catalog; both sources carry the organization RLS dimension.
func securityFixture() (*metadata.Registry, schema.Dataset) {
	registry := metadata.NewRegistry()
	for _, key := range []string{"a", "b"} {
		def := metadata.Inspect(reportSecurityRow{}, key, metadata.TypeDocument)
		def.Key, def.TableName, def.ReadPermission = key, "report_policy_"+key, key+":read"
		def.RLSDimensions = map[string]string{"organization": "organization_id"}
		registry.Register(def)
	}
	currency := metadata.Inspect(reportSecurityCurrency{}, "currency", metadata.TypeCatalog)
	currency.Key, currency.TableName, currency.ReadPermission = "currency", "report_policy_currency", "currency:read"
	registry.Register(currency)
	amountAccess := []schema.FieldAccess{{Entity: "a", Fields: []string{"amount"}}, {Entity: "b", Fields: []string{"amount"}}}
	ds := schema.Dataset{Key: "security-union", Permission: "report:security-union:read", ExecutorKey: "security-union",
		Sources: []schema.Source{
			{Key: "a", Entity: "a", Alias: "d", Relations: []schema.SourceRelation{{Entity: "currency", Alias: "c", Column: "currency_id", Required: true}}},
			{Key: "b", Entity: "b", Alias: "d", Relations: []schema.SourceRelation{{Entity: "currency", Alias: "c", Column: "currency_id", Required: true}}},
		},
		Fields: []schema.Field{
			{Name: "number", Type: schema.TypeString, Kind: schema.FieldDimension, Access: []schema.FieldAccess{{Entity: "a", Fields: []string{"number"}}, {Entity: "b", Fields: []string{"number"}}}},
			{Name: "amount", Type: schema.TypeMoney, Kind: schema.FieldMeasure, Agg: schema.AggSum, Access: amountAccess, Money: &reportcontract.MoneySources{CurrencyID: "currency_id", CurrencyCode: "currency_code", DecimalPlaces: "decimal_places"}},
			{Name: "currency_id", Type: schema.TypeString, Kind: schema.FieldDimension, Access: []schema.FieldAccess{{Entity: "currency", Fields: []string{"id"}}}},
			{Name: "currency_code", Type: schema.TypeString, Kind: schema.FieldDimension, Access: []schema.FieldAccess{{Entity: "currency", Fields: []string{"code"}}}},
			{Name: "decimal_places", Type: schema.TypeInteger, Kind: schema.FieldDimension, Access: []schema.FieldAccess{{Entity: "currency", Fields: []string{"decimal_places"}}}},
		},
	}
	return registry, ds
}

func readRule(entity, expression, effect string) security.PolicyRule {
	return &security_profile.PolicyRule{ID: id.New(), EntityName: entity, Expression: expression, Effect: effect, Actions: []string{"read"}, Enabled: true}
}
