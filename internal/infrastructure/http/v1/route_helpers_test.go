package v1

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1/middleware"
	"github.com/alex-bogatiuk/metapus/internal/metadata"
)

type testDocumentRouteHandler struct{}

func (testDocumentRouteHandler) List(c *gin.Context)            { c.Status(http.StatusOK) }
func (testDocumentRouteHandler) Create(c *gin.Context)          { c.Status(http.StatusCreated) }
func (testDocumentRouteHandler) Get(c *gin.Context)             { c.Status(http.StatusOK) }
func (testDocumentRouteHandler) Update(c *gin.Context)          { c.Status(http.StatusOK) }
func (testDocumentRouteHandler) Delete(c *gin.Context)          { c.Status(http.StatusNoContent) }
func (testDocumentRouteHandler) SetDeletionMark(c *gin.Context) { c.Status(http.StatusNoContent) }
func (testDocumentRouteHandler) Post(c *gin.Context)            { c.Status(http.StatusOK) }
func (testDocumentRouteHandler) Unpost(c *gin.Context)          { c.Status(http.StatusOK) }

func (testDocumentRouteHandler) RegisterCustomDocumentRoutes(group *gin.RouterGroup, permission string) {
	group.GET("/addresses", func(c *gin.Context) { c.Status(http.StatusOK) })
	group.GET("/availability", func(c *gin.Context) { c.Status(http.StatusOK) })
	group.GET("/source-wallets", func(c *gin.Context) { c.Status(http.StatusOK) })
	group.GET("/reconciliation/outflows", func(c *gin.Context) { c.Status(http.StatusOK) })
	group.POST("/:id/approve", func(c *gin.Context) { c.Status(http.StatusOK) })
	group.POST("/:id/reject", func(c *gin.Context) { c.Status(http.StatusOK) })
}

type fullDocumentRouteHandler struct {
	testDocumentRouteHandler
}

type guardedDocumentRouteHandler struct {
	testDocumentRouteHandler
}

func (guardedDocumentRouteHandler) RequiresCommandIdempotency() bool { return true }

func (fullDocumentRouteHandler) Copy(c *gin.Context)                { c.Status(http.StatusCreated) }
func (fullDocumentRouteHandler) UpdateAndRepost(c *gin.Context)     { c.Status(http.StatusOK) }
func (fullDocumentRouteHandler) Print(c *gin.Context)               { c.Status(http.StatusOK) }
func (fullDocumentRouteHandler) ListPrintForms(c *gin.Context)      { c.Status(http.StatusOK) }
func (fullDocumentRouteHandler) GetRelatedDocuments(c *gin.Context) { c.Status(http.StatusOK) }
func (fullDocumentRouteHandler) GetMovements(c *gin.Context)        { c.Status(http.StatusOK) }
func (fullDocumentRouteHandler) BatchAction(c *gin.Context)         { c.Status(http.StatusOK) }
func (fullDocumentRouteHandler) BatchActionByFilter(c *gin.Context) { c.Status(http.StatusOK) }
func (fullDocumentRouteHandler) ExportList(c *gin.Context)          { c.Status(http.StatusOK) }

func TestInferDocumentCapabilitiesFromHandler(t *testing.T) {
	caps := InferDocumentCapabilities(fullDocumentRouteHandler{})

	if !caps.List || !caps.Read || !caps.Create || !caps.Update || !caps.Delete || !caps.SetDeletionMark || !caps.Post || !caps.Unpost {
		t.Fatalf("expected default CRUD/posting capabilities to be enabled: %#v", caps)
	}
	if !caps.Copy || !caps.Repost || !caps.PrintForms || !caps.RelatedDocuments || !caps.Movements || !caps.BatchAction || !caps.BatchActionByFilter || !caps.ExportList {
		t.Fatalf("expected optional handler capabilities to be enabled: %#v", caps)
	}
}

func TestRegisterDocumentRoutesMountsDocumentScopedCustomRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := testDocumentRouteHandler{}

	RegisterDocumentRoutes(
		router.Group("/api/v1/document/treasury-withdrawal"),
		handler,
		"document:treasury_withdrawal",
		InferDocumentCapabilities(handler),
	)

	routes := map[string]bool{}
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = true
	}

	expected := []string{
		"GET /api/v1/document/treasury-withdrawal",
		"POST /api/v1/document/treasury-withdrawal",
		"GET /api/v1/document/treasury-withdrawal/addresses",
		"GET /api/v1/document/treasury-withdrawal/availability",
		"GET /api/v1/document/treasury-withdrawal/source-wallets",
		"GET /api/v1/document/treasury-withdrawal/reconciliation/outflows",
		"POST /api/v1/document/treasury-withdrawal/:id/approve",
		"POST /api/v1/document/treasury-withdrawal/:id/reject",
	}
	for _, path := range expected {
		if !routes[path] {
			t.Fatalf("expected route %s to be registered", path)
		}
	}

	for _, oldPath := range []string{
		"GET /api/v1/admin/crypto/treasury-withdrawals",
		"POST /api/v1/admin/crypto/treasury-withdrawals/:id/approve",
		"GET /api/v1/system/withdrawal-requests",
	} {
		if routes[oldPath] {
			t.Fatalf("old route %s must not be registered", oldPath)
		}
	}
}

func TestRegisterDocumentRoutesSkipsDisabledCapabilities(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	RegisterDocumentRoutes(
		router.Group("/api/v1/document/withdrawal-request"),
		testDocumentRouteHandler{},
		"document:withdrawal_request",
		metadata.DocumentCapabilities{List: true, Read: true},
	)

	routes := map[string]bool{}
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = true
	}

	for _, path := range []string{
		"GET /api/v1/document/withdrawal-request",
		"GET /api/v1/document/withdrawal-request/:id",
		"POST /api/v1/document/withdrawal-request/:id/approve",
	} {
		if !routes[path] {
			t.Fatalf("expected route %s to be registered", path)
		}
	}

	for _, path := range []string{
		"POST /api/v1/document/withdrawal-request",
		"PUT /api/v1/document/withdrawal-request/:id",
		"DELETE /api/v1/document/withdrawal-request/:id",
		"POST /api/v1/document/withdrawal-request/:id/post",
		"POST /api/v1/document/withdrawal-request/:id/unpost",
		"POST /api/v1/document/withdrawal-request/:id/deletion-mark",
		"POST /api/v1/document/withdrawal-request/batch-action",
		"POST /api/v1/document/withdrawal-request/batch-action-by-filter",
	} {
		if routes[path] {
			t.Fatalf("route %s must not be registered when capability is disabled", path)
		}
	}
}

func TestDocumentCommandGuardAlsoProtectsCustomFinancialRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.ErrorHandler())
	handler := guardedDocumentRouteHandler{}
	RegisterDocumentRoutes(
		router.Group("/api/v1/document/treasury-withdrawal"),
		handler,
		"document:treasury_withdrawal",
		InferDocumentCapabilities(handler),
	)

	request := httptest.NewRequest(http.MethodPost,
		"/api/v1/document/treasury-withdrawal/01999999-9999-7999-8999-999999999999/approve",
		strings.NewReader(`{"reason":"approve"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "IDEMPOTENCY_KEY_REQUIRED") {
		t.Fatalf("custom financial route bypassed command guard: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
