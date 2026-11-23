package v1

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/domain/printing"
)

type printableDocument struct{ definitionDocument }

func (*printableDocument) PrintForms() []printing.PrintFormDef {
	return []printing.PrintFormDef{
		{Name: "standard", Label: "Форма", Template: printing.TableTemplate},
		{Name: "short", Label: "Краткая", Template: printing.TableTemplate, SortOrder: 1},
	}
}

func TestDocumentPrintFormsComeFromRegistrations(t *testing.T) {
	reg := NewFactoryRegistry()
	reg.RegisterDocument(&printableDocument{definitionDocument{entity.Definition{
		Kind: entity.DocumentKind, Key: "printable_probe", Name: "PrintableProbe", Route: "printable-probes", Table: "doc_printable_probes",
	}}})
	reg.RegisterDocument(&definitionDocument{entity.Definition{
		Kind: entity.DocumentKind, Key: "plain_probe", Name: "PlainProbe", Route: "plain-probes", Table: "doc_plain_probes",
	}})

	forms := documentPrintForms(reg)

	form, ok := forms.GetForm("printable_probe", "")
	require.True(t, ok)
	require.Equal(t, "standard", form.Name)
	require.Len(t, forms.ListForms("printable_probe"), 2)
	require.Empty(t, forms.ListForms("plain_probe"))
}
