package printing

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	printmodel "metapus/internal/domain/printing"
	"strings"
	"time"

	"metapus/internal/core/types"
)

//go:embed templates
var templateFS embed.FS

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

// Render executes the named template (e.g. printing.TableTemplate) into w.
func (r *Renderer) Render(w io.Writer, templateName string, data *printmodel.PrintData) error {
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
		"formatMoney": func(v interface{ MinorUnitsValue() int64 }, dp int) string {
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
// Exported for use by handler BuildPrintData closures building printmodel.PrintTable.
func FormatMoney(v types.MinorUnits, dp int) string {
	return formatMoneyStr(v, dp)
}

// FormatQty formats Quantity to a human-readable string.
// Exported for use by handler BuildPrintData closures building printmodel.PrintTable.
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
func formatMoneyStr(v interface{ MinorUnitsValue() int64 }, dp int) string {
	exact, err := types.FormatMinorUnits(v.MinorUnitsValue(), dp)
	if err != nil {
		return "—"
	}
	return groupDecimal(exact)
}

// FormatCryptoUnits formats raw token units with the token's own scale.
// The conversion is exact: no floating point and no assumed scale. Trailing
// fractional zeros beyond two digits (or the token scale, if smaller) are
// dropped because they carry no value.
// Example: FormatCryptoUnits(1500000, 6) → "1,50"; (1234567, 6) → "1,234567"
func FormatCryptoUnits(v types.CryptoAmount, decimalPlaces int) string {
	if decimalPlaces < 0 || decimalPlaces > _maxCryptoDecimals {
		return "—"
	}
	exact := v.ToDecimal(decimalPlaces).StringFixed(int32(decimalPlaces))
	if whole, fraction, ok := strings.Cut(exact, "."); ok {
		fraction = strings.TrimRight(fraction, "0")
		for len(fraction) < min(2, decimalPlaces) {
			fraction += "0"
		}
		exact = whole + "." + fraction
	}
	return groupDecimal(exact)
}

// _maxCryptoDecimals bounds token metadata accepted for display.
const _maxCryptoDecimals = 36

// groupDecimal renders a canonical "-1234.5600" string with non-breaking
// thousands separators and a decimal comma.
func groupDecimal(exact string) string {
	parts := strings.SplitN(exact, ".", 2)
	negative := strings.HasPrefix(parts[0], "-")
	digits := strings.TrimPrefix(parts[0], "-")
	for i := len(digits) - 3; i > 0; i -= 3 {
		digits = digits[:i] + "\u00a0" + digits[i:]
	}
	if negative {
		digits = "-" + digits
	}
	if len(parts) == 2 {
		digits += "," + parts[1]
	}
	return digits
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
