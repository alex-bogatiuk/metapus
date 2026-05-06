package automation

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"strconv"
	"time"
)

//go:embed templates
var emailTemplateFS embed.FS

// ReportEmailData is the context passed to the report_email template.
type ReportEmailData struct {
	ReportName  string
	PeriodFrom  string
	PeriodTo    string
	RowCount    int
	GeneratedAt string
	CustomBody  template.HTML // Pre-rendered action_template (safe HTML)
	AppName     string
	Year        int
}

// EmailTemplateRenderer renders HTML email bodies for report distribution.
// Uses embedded Go templates (same pattern as printing.Renderer).
type EmailTemplateRenderer struct {
	templates *template.Template
}

// NewEmailTemplateRenderer loads all embedded email templates and returns a ready renderer.
func NewEmailTemplateRenderer() (*EmailTemplateRenderer, error) {
	funcMap := template.FuncMap{
		"itoa": strconv.Itoa,
	}
	tmpl, err := template.New("").Funcs(funcMap).ParseFS(emailTemplateFS, "templates/*.gohtml")
	if err != nil {
		return nil, fmt.Errorf("load email templates: %w", err)
	}
	return &EmailTemplateRenderer{templates: tmpl}, nil
}

// RenderReportEmail renders the report_email template with the given data.
// Returns the full HTML string ready for use as the email body.
func (r *EmailTemplateRenderer) RenderReportEmail(ref *GeneratedReportRef, customBody string) (string, error) {
	data := ReportEmailData{
		ReportName:  ref.DatasetName,
		PeriodFrom:  ref.Period.From.Format("02.01.2006"),
		PeriodTo:    ref.Period.To.Format("02.01.2006"),
		RowCount:    ref.RowCount,
		GeneratedAt: time.Now().Format("02.01.2006 15:04"),
		CustomBody:  template.HTML(customBody), //nolint:gosec // customBody is from action_template, admin-controlled
		AppName:     "Metapus ERP",
		Year:        time.Now().Year(),
	}

	var buf bytes.Buffer
	if err := r.templates.ExecuteTemplate(&buf, "report_email", data); err != nil {
		return "", fmt.Errorf("render report email: %w", err)
	}
	return buf.String(), nil
}
