package handlers

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/security"
	"metapus/internal/domain"
	"metapus/internal/domain/printing"
)

// DocumentPrintHandlerConfig configures a document print handler.
type DocumentPrintHandlerConfig[T any] struct {
	// Service is the document service used to fetch the document (includes RLS check).
	Service domain.DocumentService[T]
	// EntityName is the snake_case entity name for FLS lookup, e.g. "goods_receipt".
	EntityName string
	// DocType is the print registry key, e.g. "goods_receipt".
	DocType string
	// Registry holds available print form definitions.
	Registry *printing.PrintFormRegistry
	// Renderer renders HTML from templates.
	Renderer *printing.Renderer
	// ResolveRefs resolves FK references for display names (nil = skip).
	ResolveRefs func(ctx context.Context, entities ...T) (any, error)
	// BuildPrintData converts the FLS-masked entity + resolved refs to print template context.
	BuildPrintData func(entity T, refs any, showPrices bool) *printing.PrintData
}

// DocumentPrintHandler provides the Print HTTP handler for a single document type.
// Embed this struct in a concrete handler and it automatically satisfies
// DocumentPrintHandlerInterface, causing RegisterDocumentRoutes to add the print route.
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

	// Fetch document — triggers RLS dimension check via BaseDocumentService.
	doc, err := h.cfg.Service.GetByID(ctx, docID)
	if err != nil {
		h.Error(c, err)
		return
	}

	// FLS: determine visibility of price/amount columns before masking the entity.
	showPrices := true
	readPolicy := security.GetFieldPolicy(ctx, h.cfg.EntityName, "read")
	if readPolicy != nil {
		if !readPolicy.IsTablePartFieldAllowed("lines", "unit_price") {
			showPrices = false
		}
	}

	// Apply FLS masking to the domain entity in place.
	if readPolicy != nil {
		security.MaskForRead(doc, readPolicy)
	}

	// Resolve reference display names for the (now-masked) entity.
	var refs any
	if h.cfg.ResolveRefs != nil {
		refs, err = h.cfg.ResolveRefs(ctx, doc)
		if err != nil {
			h.Error(c, err)
			return
		}
	}

	// Build the template data context (includes Table for XLSX/DOCX).
	printData := h.cfg.BuildPrintData(doc, refs, showPrices)

	var buf bytes.Buffer

	switch output {
	case "pdf":
		// Render HTML first (same template as output=html), then convert to PDF.
		var htmlBuf bytes.Buffer
		if err := h.cfg.Renderer.Render(&htmlBuf, formDef.Template, printData); err != nil {
			h.Error(c, apperror.NewInternal(err))
			return
		}
		if err := printing.RenderPDF(&buf, htmlBuf.Bytes()); err != nil {
			h.Error(c, apperror.NewInternal(err))
			return
		}
		filename := sanitizeFilename(printData.Table.Title + " " + printData.Table.Subtitle)
		c.Header("Content-Type", "application/pdf")
		c.Header("Content-Disposition", contentDisposition(filename, "pdf"))

	case "docx":
		if err := printing.RenderDOCX(&buf, printData); err != nil {
			h.Error(c, apperror.NewInternal(err))
			return
		}
		filename := sanitizeFilename(printData.Table.Title + " " + printData.Table.Subtitle)
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
