package printing

// PrintData is the context passed to every print template.
type PrintData struct {
	// FormLabel is the document type label shown in the title, e.g. "Crypto Invoice".
	FormLabel string
	// ShowPrices controls whether price/amount columns are visible (FLS-aware).
	ShowPrices bool
	// DecimalPlaces is the number of decimal places for monetary formatting.
	DecimalPlaces int
	// CurrencySymbol is the currency symbol, e.g. "₽".
	CurrencySymbol string
	// Doc is the typed document response DTO; templates access its fields via reflection.
	Doc any
	// Table is a format-agnostic pre-formatted representation used by XLSX/DOCX renderers.
	// HTML renderer ignores this field and uses Doc + Go templates instead.
	Table *PrintTable
}
