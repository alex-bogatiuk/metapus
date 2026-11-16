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
	"metapus/internal/core/security"
	"metapus/internal/domain"
	"metapus/internal/domain/printing"
	"metapus/internal/infrastructure/http/v1/middleware"
)

type printFixtureDoc struct {
	TotalAmount int64 `db:"total_amount" json:"totalAmount"`
}

type printFixtureService struct {
	domain.DocumentService[*printFixtureDoc]
}

func (printFixtureService) GetByID(context.Context, id.ID) (*printFixtureDoc, error) {
	return &printFixtureDoc{TotalAmount: 9007199254740993}, nil
}

func fixturePrintRegistry() *printing.PrintFormRegistry {
	registry := printing.NewPrintFormRegistry()
	registry.Register("fixture_doc", printing.PrintFormDef{Name: "standard", Label: "Форма", Template: printing.TableTemplate})
	return registry
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
			Service: printFixtureService{}, EntityName: "fixture_doc", DocType: "fixture_doc",
			Registry: fixturePrintRegistry(), Renderer: renderer, PDFRenderer: pdf,
			BuildPrintData: func(_ context.Context, doc *printFixtureDoc, _ *security.FieldPolicy) (*printing.PrintData, error) {
				return &printing.PrintData{Doc: doc, Table: &printing.PrintTable{Title: "Документ", Columns: []string{"Сумма"}}}, nil
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

func TestPrintBuildsFromReadProjectionWithCallerPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	renderer, err := printadapter.NewRenderer()
	require.NoError(t, err)
	var seen *printFixtureDoc
	var seenPolicy *security.FieldPolicy
	handler := NewDocumentPrintHandler(NewBaseHandler(), DocumentPrintHandlerConfig[*printFixtureDoc]{
		Service: printFixtureService{}, EntityName: "fixture_doc", DocType: "fixture_doc",
		Registry: fixturePrintRegistry(), Renderer: renderer,
		BuildPrintData: func(_ context.Context, doc *printFixtureDoc, policy *security.FieldPolicy) (*printing.PrintData, error) {
			seen, seenPolicy = doc, policy
			return &printing.PrintData{Table: &printing.PrintTable{Title: "Документ"}}, nil
		},
	})
	policy := &security.FieldPolicy{AllowedFields: []string{"*", "-total_amount"}}
	router := gin.New()
	router.Use(middleware.ErrorHandler(), func(c *gin.Context) {
		c.Request = c.Request.WithContext(security.WithFieldPolicies(c.Request.Context(), map[string]*security.FieldPolicy{"fixture_doc:read": policy}))
	})
	router.GET("/document/:id/print", handler.Print)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/document/"+id.New().String()+"/print", nil))

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Zero(t, seen.TotalAmount, "the template must see the FLS projection")
	require.Same(t, policy, seenPolicy)
	require.Contains(t, recorder.Body.String(), "Документ")
}

func TestPrintWithoutTemplatesFailsInsteadOfPanicking(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewDocumentPrintHandler(NewBaseHandler(), DocumentPrintHandlerConfig[*printFixtureDoc]{
		Service: printFixtureService{}, EntityName: "fixture_doc", DocType: "fixture_doc",
		Registry: fixturePrintRegistry(),
	})
	router := gin.New()
	router.Use(middleware.ErrorHandler())
	router.GET("/document/:id/print", handler.Print)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/document/"+id.New().String()+"/print", nil))
	require.Equal(t, http.StatusInternalServerError, recorder.Code)
}
