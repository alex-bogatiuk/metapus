package printing

// PrintTable is a format-agnostic representation of a document print form.
// HTML renderer uses PrintData.Doc + Go templates; XLSX/DOCX renderers use this flat structure.
// All layout decisions (signature orientation, header grid, etc.) are expressed here —
// renderers must NOT hardcode layout, they interpret this struct.
type PrintTable struct {
	// Title is the document type name, e.g. "Goods Receipt".
	Title string
	// Subtitle is "№ ... from ...", e.g. "№ GR-001 from 19.03.2026".
	Subtitle string
	// HeaderRows contains key-value pairs displayed above the table.
	// Each inner slice represents one visual row (typically 2 pairs per row for 2-column layout).
	HeaderRows [][]PrintHeaderField
	// Columns is the list of column headers for the line items table.
	Columns []string
	// Rows contains pre-formatted string values for each line item.
	Rows []PrintTableRow
	// Totals are label-value pairs shown below the table.
	Totals []PrintTotalLine
	// SignatureBlock describes the signature area with layout mode and entries.
	SignatureBlock *PrintSignatureBlock
}

// SignatureLayout determines how signature entries are arranged.
type SignatureLayout string

const (
	// SignatureLayoutHorizontal renders all entries side-by-side (e.g. 3-column flex/table).
	// This matches HTML's `display: flex; justify-content: space-between`.
	SignatureLayoutHorizontal SignatureLayout = "horizontal"
	// SignatureLayoutVertical renders entries stacked one below another.
	SignatureLayoutVertical SignatureLayout = "vertical"
)

// PrintSignatureBlock describes the complete signature area of a print form.
type PrintSignatureBlock struct {
	// Layout controls whether entries are rendered side-by-side or stacked.
	Layout SignatureLayout
	// Entries are the individual signature slots.
	Entries []PrintSignatureEntry
}

// PrintSignatureEntry is a single signature slot with role label and hint text.
type PrintSignatureEntry struct {
	// Role is the signer role label, e.g. "Shipper (Supplier):".
	Role string
	// Hint is the text below the signature line, e.g. "signature / full name".
	Hint string
}

// PrintHeaderField is a label-value pair in the document header area.
type PrintHeaderField struct {
	Label string
	Value string
}

// PrintTableRow is a single line item row with pre-formatted cell values.
type PrintTableRow struct {
	Values []string
}

// PrintTotalLine is a label-value pair in the totals section.
type PrintTotalLine struct {
	Label string
	Value string
	Grand bool // if true, render with emphasis (bold / larger font)
}

// ── Convenience constructors ─────────────────────────────────────────────

// HorizontalSignatures builds a horizontal signature block with the default hint "signature / full name".
func HorizontalSignatures(roles ...string) *PrintSignatureBlock {
	entries := make([]PrintSignatureEntry, len(roles))
	for i, r := range roles {
		entries[i] = PrintSignatureEntry{Role: r, Hint: "signature / full name"}
	}
	return &PrintSignatureBlock{Layout: SignatureLayoutHorizontal, Entries: entries}
}

// VerticalSignatures builds a vertical signature block with the default hint "signature / full name".
func VerticalSignatures(roles ...string) *PrintSignatureBlock {
	entries := make([]PrintSignatureEntry, len(roles))
	for i, r := range roles {
		entries[i] = PrintSignatureEntry{Role: r, Hint: "signature / full name"}
	}
	return &PrintSignatureBlock{Layout: SignatureLayoutVertical, Entries: entries}
}
