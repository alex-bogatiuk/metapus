package printing

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"strconv"
	"strings"
	"time"

	"metapus/internal/core/types"
)

//go:embed templates
var templateFS embed.FS

// PrintData is the context passed to every print template.
type PrintData struct {
	// FormLabel is the document type label shown in the title, e.g. "Goods Receipt".
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

// Renderer renders print form HTML using embedded Go templates.
type Renderer struct {
	templates *template.Template
}

// NewRenderer loads all embedded templates and returns a ready Renderer.
func NewRenderer() (*Renderer, error) {
	tmpl, err := template.New("").Funcs(buildFuncMap()).ParseFS(templateFS, "templates/*.gohtml")
	if err != nil {
		return nil, fmt.Errorf("load print templates: %w", err)
	}
	return &Renderer{templates: tmpl}, nil
}

// Render executes the named template (e.g. "goods_receipt.gohtml") into w.
func (r *Renderer) Render(w io.Writer, templateName string, data *PrintData) error {
	return r.templates.ExecuteTemplate(w, templateName, data)
}

// buildFuncMap returns the template helper functions.
func buildFuncMap() template.FuncMap {
	return template.FuncMap{
		"formatDate": func(t time.Time) string {
			if t.IsZero() {
				return ""
			}
			return t.Format("02.01.2006")
		},
		"formatDatePtr": func(t *time.Time) string {
			if t == nil || t.IsZero() {
				return ""
			}
			return t.Format("02.01.2006")
		},
		"formatMoney": func(v types.MinorUnits, dp int) string {
			return formatMoneyStr(v, dp)
		},
		"formatQty": func(v types.Quantity) string {
			return formatQtyStr(v)
		},
		"derefStr": func(s *string) string {
			if s == nil {
				return ""
			}
			return *s
		},
		"add": func(a, b int) int { return a + b },
	}
}

// FormatMoney formats MinorUnits to a Russian-locale decimal string.
// Exported for use by handler BuildPrintData closures building PrintTable.
func FormatMoney(v types.MinorUnits, dp int) string {
	return formatMoneyStr(v, dp)
}

// FormatQty formats Quantity to a human-readable string.
// Exported for use by handler BuildPrintData closures building PrintTable.
func FormatQty(v types.Quantity) string {
	return formatQtyStr(v)
}

// FormatDate formats time.Time to DD.MM.YYYY.
func FormatDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("02.01.2006")
}

// formatMoneyStr converts MinorUnits to a Russian-locale decimal string.
// Example: formatMoneyStr(123456, 2) → "1 234,56"
func formatMoneyStr(v types.MinorUnits, dp int) string {
	if dp < 0 {
		dp = 2
	}
	abs := int64(v)
	negative := abs < 0
	if negative {
		abs = -abs
	}

	scale := int64(1)
	for i := 0; i < dp; i++ {
		scale *= 10
	}

	intPart := abs / scale
	fracPart := abs % scale

	intStr := formatIntSpaces(intPart)
	if dp == 0 {
		if negative {
			return "-" + intStr
		}
		return intStr
	}

	fracStr := fmt.Sprintf("%0*d", dp, fracPart)
	result := intStr + "," + fracStr
	if negative {
		return "-" + result
	}
	return result
}

// formatIntSpaces formats an int64 with space thousands separators.
// Example: 1234567 → "1 234 567", 0 → "0"
func formatIntSpaces(n int64) string {
	if n == 0 {
		return "0"
	}
	s := strconv.FormatInt(n, 10)
	var sb strings.Builder
	start := len(s) % 3
	if start > 0 {
		sb.WriteString(s[:start])
	}
	for i := start; i < len(s); i += 3 {
		if sb.Len() > 0 {
			sb.WriteRune('\u00a0') // non-breaking space
		}
		sb.WriteString(s[i : i+3])
	}
	return sb.String()
}

// formatQtyStr converts Quantity to a human-readable string.
// Keeps at least 3 decimal places; trims excess trailing zeros.
// Example: Quantity(10000) → "1.000", Quantity(15500) → "1.550"
func formatQtyStr(v types.Quantity) string {
	f := v.Float64()
	s := fmt.Sprintf("%.4f", f)
	parts := strings.SplitN(s, ".", 2)
	if len(parts) == 2 {
		frac := strings.TrimRight(parts[1], "0")
		if len(frac) < 3 {
			frac = frac + strings.Repeat("0", 3-len(frac))
		}
		return parts[0] + "." + frac
	}
	return s
}
