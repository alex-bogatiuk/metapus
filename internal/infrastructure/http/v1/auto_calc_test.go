package v1

import (
	"reflect"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

var autoInvoiceDefinition = entity.Definition{
	Kind: entity.DocumentKind, Key: "auto_invoice", Name: "AutoInvoice", Route: "auto-invoices", Table: "doc_auto_invoices",
	Parts: []entity.Part{{Key: "lines", Field: "lines"}},
}

type autoInvoiceLine struct {
	LineID   id.ID            `db:"line_id" json:"lineId"`
	LineNo   int              `db:"line_no" json:"lineNo"`
	Quantity types.Quantity   `db:"quantity" json:"quantity"`
	Price    types.MinorUnits `db:"price" json:"price"`
	Discount *int64           `db:"discount" json:"discount"`
	Gross    types.MinorUnits `db:"gross" json:"gross" meta:"label:Без скидки,calc:quantity*price"`
	Amount   types.MinorUnits `db:"amount" json:"amount" meta:"label:Сумма,calc:gross-discount"`
	Share    decimal.Decimal  `db:"share" json:"share" meta:"calc:amount/3"`
}

type autoInvoice struct {
	entity.Document
	Rate  int64             `db:"rate" json:"rate"`
	Base  types.MinorUnits  `db:"base" json:"base"`
	Fee   types.MinorUnits  `db:"fee" json:"fee" meta:"label:Комиссия,calc:base*rate/100"`
	Total types.MinorUnits  `db:"total" json:"total" meta:"label:Итого,calc:sum(lines.amount)+fee"`
	Lines []autoInvoiceLine `db:"-" json:"lines"`
}

func (autoInvoice) EntityDefinition() entity.Definition { return autoInvoiceDefinition }
func (*autoInvoice) GetDocumentType() string            { return autoInvoiceDefinition.Name }

func TestAutoCalculatedFieldsAreTheServersAndComputedInOrder(t *testing.T) {
	shape := autoShapeFor[autoInvoice]()
	require.NotContains(t, jsonNames(shape.create), "fee", "a calculated header field is not in requests")
	require.Contains(t, jsonNames(shape.read), "fee")
	lines := shape.parts[0]
	require.Equal(t, []string{"lineId", "quantity", "price", "discount"}, jsonNames(lines.create))
	require.Contains(t, jsonNames(lines.read), "amount")

	discount := int64(150)
	invoice := &autoInvoice{Document: entity.NewDocument(), Rate: 3, Base: 10_001, Lines: []autoInvoiceLine{
		// 1.5 × 19.99 = 29.985 → 2999 minor units (half away from zero), less 1.50 of discount.
		{Quantity: types.NewQuantityFromInt64Scaled(15_000), Price: 1999, Discount: &discount},
		{Quantity: types.NewQuantityFromInt64Scaled(20_000), Price: 500},
	}}
	require.NoError(t, shape.computeAutoCalcs(invoice))
	require.Equal(t, types.MinorUnits(300), invoice.Fee, "10001 × 3 / 100 = 300.03 → 300")
	require.Equal(t, types.MinorUnits(2999), invoice.Lines[0].Gross)
	require.Equal(t, types.MinorUnits(2849), invoice.Lines[0].Amount, "a formula reads another one computed first")
	require.Equal(t, "949.6667", invoice.Lines[0].Share.String())
	require.Equal(t, types.MinorUnits(1000), invoice.Lines[1].Amount, "a nil operand counts as zero")
	require.Equal(t, types.MinorUnits(2849+1000+300), invoice.Total, "the header totals the lines' calculated amounts")

	invoice.Base = types.MinorUnits(1 << 62)
	invoice.Rate = 1000
	err := shape.computeAutoCalcs(invoice)
	require.Error(t, err)
	var appErr *apperror.AppError
	require.ErrorAs(t, err, &appErr)
	require.Contains(t, appErr.Message, "Комиссия")
}

type calcProbeLoop struct {
	entity.Document
	A int64 `db:"a" json:"a" meta:"calc:b+1"`
	B int64 `db:"b" json:"b" meta:"calc:a+1"`
}

type calcProbeUnknown struct {
	entity.Document
	A int64 `db:"a" json:"a" meta:"calc:missing*2"`
}

type calcProbeText struct {
	entity.Document
	Note string `db:"note" json:"note"`
	A    int64  `db:"a" json:"a" meta:"calc:note*2"`
}

type calcProbeNoPart struct {
	entity.Document
	A int64 `db:"a" json:"a"`
	B int64 `db:"b" json:"b" meta:"calc:sum(a.x)"`
}

type calcProbeTextColumn struct {
	entity.Document
	Lines []struct {
		Note string `db:"note" json:"note"`
	} `db:"-" json:"lines"`
	B int64 `db:"b" json:"b" meta:"calc:sum(lines.note)"`
}

type calcProbeCatalog struct {
	entity.Catalog
	A int64 `db:"a" json:"a"`
	B int64 `db:"b" json:"b" meta:"calc:a*2"`
}

func TestAutoCalculatedFieldsAreCheckedAtRegistration(t *testing.T) {
	document := entity.Definition{Kind: entity.DocumentKind, Key: "calc_probe", Name: "CalcProbe", Route: "calc-probes", Table: "doc_calc_probes"}
	cases := map[reflect.Type]string{
		reflect.TypeFor[calcProbeLoop]():    "cycle",
		reflect.TypeFor[calcProbeUnknown](): "not a field",
		reflect.TypeFor[calcProbeText]():    "not a number",
		reflect.TypeFor[calcProbeNoPart]():  "not a table part",
	}
	_, err := deriveAutoCalcs(reflect.TypeFor[calcProbeTextColumn](), entity.DocumentKind)
	require.ErrorContains(t, err, "not a number", "a total needs a numeric column")
	for model, want := range cases {
		_, err := deriveAutoShape(model, document)
		require.ErrorContains(t, err, want, model.Name())
	}
	catalog := entity.Definition{Kind: entity.CatalogKind, Key: "calc_item", Name: "CalcItem", Route: "calc-items", Table: "cat_calc_items"}
	_, err = deriveAutoShape(reflect.TypeFor[calcProbeCatalog](), catalog)
	require.ErrorContains(t, err, "documents")
}
