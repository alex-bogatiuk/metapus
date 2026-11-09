package printing

import "context"

// PDFRenderer converts self-contained HTML without accepting a navigation URL.
// Browser execution belongs to the infrastructure service, outside the ERP server.
type PDFRenderer interface {
	Render(context.Context, []byte, PDFOptions) ([]byte, error)
}

type PDFOptions struct {
	PaperSize string `json:"paperSize"`
	Landscape bool   `json:"landscape"`
}
