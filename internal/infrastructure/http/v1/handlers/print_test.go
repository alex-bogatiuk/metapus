package handlers

import (
	"context"
	"errors"
	printadapter "metapus/internal/infrastructure/printing"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"metapus/internal/domain"
	"metapus/internal/domain/printing"
	"metapus/internal/infrastructure/http/v1/middleware"
)

type printFixtureDoc struct {
	TotalAmount int64
}

type printFixtureService struct {
	domain.DocumentService[*printFixtureDoc]
}

func (printFixtureService) GetByID(context.Context, id.ID) (*printFixtureDoc, error) {
	return &printFixtureDoc{TotalAmount: 9007199254740993}, nil
}

type unavailablePrintRenderer struct{}

func (unavailablePrintRenderer) Render(context.Context, []byte, printing.PDFOptions) ([]byte, error) {
	return nil, errors.New("renderer offline")
}

func TestPDFOutageLeavesHTMLAndDOCXAvailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	renderer, err := printadapter.NewRenderer()
	require.NoError(t, err)
	for _, pdf := range []printing.PDFRenderer{nil, unavailablePrintRenderer{}} {
		handler := NewDocumentPrintHandler(NewBaseHandler(), DocumentPrintHandlerConfig[*printFixtureDoc]{
			Service: printFixtureService{}, EntityName: "crypto_invoice", DocType: "crypto_invoice",
			Registry: printing.NewPrintFormRegistry(), Renderer: renderer, PDFRenderer: pdf,
			BuildPrintData: func(doc *printFixtureDoc, _ any, showPrices bool) *printing.PrintData {
				return &printing.PrintData{Doc: doc, ShowPrices: showPrices, DecimalPlaces: 9, Table: &printing.PrintTable{Title: "Инвойс", Columns: []string{"Сумма"}}}
			},
		})
		router := gin.New()
		router.Use(middleware.ErrorHandler())
		router.GET("/document/:id/print", handler.Print)
		for _, format := range []string{"pdf", "html", "docx"} {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/document/"+id.New().String()+"/print?output="+format, nil))
			if format == "pdf" {
				require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
				require.Contains(t, recorder.Body.String(), "PDF_RENDERER_UNAVAILABLE")
			} else {
				require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
				require.NotEmpty(t, recorder.Body.Bytes())
				if format == "docx" {
					require.Equal(t, "PK", recorder.Body.String()[:2])
				}
			}
		}
	}
}
