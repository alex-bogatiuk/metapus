package handlers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	printadapter "metapus/internal/infrastructure/printing"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/security"
	"metapus/internal/domain"
	"metapus/internal/domain/printing"
	"metapus/internal/infrastructure/pdfrender"
)

// DocumentPrintHandlerConfig configures a document print handler.
type DocumentPrintHandlerConfig[T any] struct {
	// Service is the document service used to fetch the document (includes RLS check).
	Service domain.DocumentService[T]
	// EntityName is the snake_case entity name for FLS lookup, e.g. "crypto_invoice".
	EntityName string
	// DocType is the print registry key, e.g. "crypto_invoice".
	DocType string
	// Registry holds available print form definitions.
	Registry *printing.PrintFormRegistry
	// Renderer renders HTML from templates; nil makes every print request fail
	// with an internal error instead of panicking.
	Renderer    *printadapter.Renderer
	PDFRenderer printing.PDFRenderer
	// BuildPrintData converts the read projection of the document into the
	// template context. Restricted fields are already zero in doc; policy is the
	// caller's read policy (nil = unrestricted) so the builder can show such a
	// field as hidden instead of as a real zero. Referenced entities must be read
	// through an authorized reader, never directly.
	BuildPrintData func(ctx context.Context, doc T, policy *security.FieldPolicy) (*printing.PrintData, error)
}

// DocumentPrintHandler provides the print HTTP handlers for a single document type.
// A document handler keeps it in a named field and delegates Print and
// ListPrintForms to it (see CryptoInvoiceHandler); embedding would make the
// shared BaseHandler methods ambiguous. The routes exist only when the
// registration also declares the PrintForms capability.
type DocumentPrintHandler[T any] struct {
	*BaseHandler
	cfg DocumentPrintHandlerConfig[T]
}

// NewDocumentPrintHandler creates a DocumentPrintHandler with the given config.
func NewDocumentPrintHandler[T any](
	base *BaseHandler,
	cfg DocumentPrintHandlerConfig[T],
) *DocumentPrintHandler[T] {
	return &DocumentPrintHandler[T]{BaseHandler: base, cfg: cfg}
}

// Print handles GET /document/{type}/:id/print?format=<name>&output=html|pdf|docx
// output=html (default) returns a self-contained HTML page.
// output=pdf returns a PDF file rendered from the HTML template via headless Chrome.
// output=docx returns a Word file (Content-Disposition: attachment).
func (h *DocumentPrintHandler[T]) Print(c *gin.Context) {
	ctx := c.Request.Context()

	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	formName := c.DefaultQuery("format", "")
	formDef, ok := h.cfg.Registry.GetForm(h.cfg.DocType, formName)
	if !ok {
		h.Error(c, apperror.NewNotFound("print form", formName).
			WithDetail("docType", h.cfg.DocType))
		return
	}

	output := c.DefaultQuery("output", "html")
	if output != "html" && output != "pdf" && output != "docx" {
		h.Error(c, apperror.NewValidation("output must be one of: html, pdf, docx"))
		return
	}

	if h.cfg.Renderer == nil {
		h.Error(c, apperror.NewInternal(errors.New("print templates are not loaded")))
		return
	}

	// Fetch document — triggers RLS dimension check via BaseDocumentService.
	doc, err := h.cfg.Service.GetByID(ctx, docID)
	if err != nil {
		h.Error(c, err)
		return
	}

	// FLS: the template sees only the read projection, exactly like GET /:id.
	readPolicy := security.GetFieldPolicy(ctx, h.cfg.EntityName, "read")
	printData, err := h.cfg.BuildPrintData(ctx, security.ProjectForRead(doc, readPolicy), readPolicy)
	if err != nil {
		h.Error(c, err)
		return
	}
	if printData.FormLabel == "" {
		printData.FormLabel = formDef.Label
	}

	var buf bytes.Buffer

	switch output {
	case "pdf":
		// Render HTML first (same template as output=html), then convert to PDF.
		var htmlBuf bytes.Buffer
		if err := h.cfg.Renderer.Render(&htmlBuf, formDef.Template, printData); err != nil {
			h.Error(c, apperror.NewInternal(err))
			return
		}
		if h.cfg.PDFRenderer == nil {
			h.Error(c, pdfrender.Unavailable(nil))
			return
		}
		pdf, err := h.cfg.PDFRenderer.Render(ctx, htmlBuf.Bytes(), printing.PDFOptions{PaperSize: formDef.PaperSize})
		if err != nil {
			h.Error(c, pdfrender.Unavailable(err))
			return
		}
		buf.Write(pdf)
		filename := printFilename(printData)
		c.Header("Content-Type", "application/pdf")
		c.Header("Content-Disposition", contentDisposition(filename, "pdf"))

	case "docx":
		if err := printadapter.RenderDOCX(&buf, printData); err != nil {
			h.Error(c, apperror.NewInternal(err))
			return
		}
		filename := printFilename(printData)
		c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
		c.Header("Content-Disposition", contentDisposition(filename, "docx"))

	default: // html
		if err := h.cfg.Renderer.Render(&buf, formDef.Template, printData); err != nil {
			h.Error(c, apperror.NewInternal(err))
			return
		}
		c.Header("Content-Type", "text/html; charset=utf-8")
	}

	c.Status(http.StatusOK)
	_, _ = c.Writer.Write(buf.Bytes())
}

// ListPrintForms handles GET /document/{type}/print-forms
// Returns []PrintFormSummary with available print forms for the document type.
func (h *DocumentPrintHandler[T]) ListPrintForms(c *gin.Context) {
	forms := h.cfg.Registry.ListForms(h.cfg.DocType)
	c.JSON(http.StatusOK, forms)
}

// printFilename names a download after the document title and number.
func printFilename(data *printing.PrintData) string {
	if data.Table == nil {
		return sanitizeFilename(data.FormLabel)
	}
	return sanitizeFilename(strings.TrimSpace(data.Table.Title + " " + data.Table.Subtitle))
}

// contentDisposition builds a Content-Disposition header with RFC 5987 encoding
// for non-ASCII filenames. The `ext` parameter is the file extension without dot.
func contentDisposition(name, ext string) string {
	full := name + "." + ext
	encoded := url.PathEscape(full)
	// ASCII fallback (replace non-ASCII with _) + RFC 5987 filename* with UTF-8 percent-encoding.
	return fmt.Sprintf(`attachment; filename="%s.%s"; filename*=UTF-8''%s`, sanitizeASCII(name), ext, encoded)
}

// sanitizeFilename removes characters unsafe for HTTP Content-Disposition filenames.
func sanitizeFilename(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '"', '\\', '/', ':', '*', '?', '<', '>', '|':
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// sanitizeASCII replaces non-ASCII runes with underscores for the fallback filename.
func sanitizeASCII(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r > 127 || r == '"' || r == '\\' || r == '/' || r == ':' || r == '*' || r == '?' || r == '<' || r == '>' || r == '|' {
			b.WriteRune('_')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
