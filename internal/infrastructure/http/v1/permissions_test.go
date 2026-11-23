package v1

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/domain/reports/schema"
	"github.com/alex-bogatiuk/metapus/internal/metadata"
)

type testCatalogRouteHandler struct{ testDocumentRouteHandler }

func (testCatalogRouteHandler) GetTree(c *gin.Context) { c.Status(http.StatusOK) }

// requiredPermissions calls every route mounted by mount as a user without
// permissions and collects the permission each route refuses with.
func requiredPermissions(t *testing.T, mount func(*gin.RouterGroup)) []string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	seen := map[string]bool{}
	router.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(appctx.WithUser(c.Request.Context(), &appctx.UserContext{UserID: "probe"}))
		c.Next()
		for _, err := range c.Errors {
			var appErr *apperror.AppError
			if errors.As(err.Err, &appErr) {
				if code, ok := appErr.Details["required_permission"].(string); ok {
					seen[code] = true
				}
			}
		}
	})
	mount(router.Group("/probe"))
	for _, route := range router.Routes() {
		path := strings.ReplaceAll(route.Path, ":id", "00000000-0000-0000-0000-000000000001")
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(route.Method, path, nil))
	}
	codes := make([]string, 0, len(seen))
	for code := range seen {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

func derivedCodes(reg *FactoryRegistry) []string {
	var codes []string
	for _, permission := range reg.Permissions() {
		codes = append(codes, permission.Code)
	}
	return codes
}

func TestDerivedCatalogPermissionsAreTheOnesItsRoutesCheck(t *testing.T) {
	def := entity.Definition{Kind: entity.CatalogKind, Key: "perm_probe", Name: "PermProbe", Route: "perm-probes", Table: "cat_perm_probes"}
	reg := NewFactoryRegistry()
	reg.RegisterCatalog(&definitionCatalog{def})

	checked := requiredPermissions(t, func(g *gin.RouterGroup) {
		RegisterCatalogRoutes(g, testCatalogRouteHandler{}, def.Permission())
	})
	require.Equal(t, checked, derivedCodes(reg))
}

type capabilityDocument struct {
	definitionDocument
	caps    metadata.DocumentCapabilities
	actions []metadata.EntityActionDef
}

func (d *capabilityDocument) DocumentCapabilities() metadata.DocumentCapabilities { return d.caps }
func (d *capabilityDocument) DocumentActions() []metadata.EntityActionDef         { return d.actions }

func TestDerivedDocumentPermissionsFollowItsCapabilities(t *testing.T) {
	def := entity.Definition{Kind: entity.DocumentKind, Key: "perm_doc", Name: "PermDoc", Route: "perm-docs", Table: "doc_perm_docs"}
	full := InferDocumentCapabilities(fullDocumentRouteHandler{})
	readOnly := metadata.DocumentCapabilities{List: true, Read: true, PrintForms: true, Movements: true}
	approvals := metadata.DocumentCapabilities{List: true, Read: true, BatchAction: true, Copy: true}
	for name, caps := range map[string]metadata.DocumentCapabilities{"full": full, "read only": readOnly, "approvals": approvals} {
		t.Run(name, func(t *testing.T) {
			reg := NewFactoryRegistry()
			reg.RegisterDocument(&capabilityDocument{definitionDocument: definitionDocument{def}, caps: caps})
			checked := requiredPermissions(t, func(g *gin.RouterGroup) {
				RegisterDocumentRoutes(g, fullDocumentRouteHandler{}, def.Permission(), caps)
			})
			require.Equal(t, checked, derivedCodes(reg))
		})
	}
}

func TestPermissionsCoverDocumentActionsAndDatasets(t *testing.T) {
	def := entity.Definition{Kind: entity.DocumentKind, Key: "perm_doc", Name: "PermDoc", Route: "perm-docs", Table: "doc_perm_docs"}
	reg := NewFactoryRegistry()
	reg.RegisterDocument(&capabilityDocument{
		definitionDocument: definitionDocument{def},
		caps:               metadata.DocumentCapabilities{List: true, Read: true},
		actions: []metadata.EntityActionDef{
			{Key: "approve", Label: "Одобрить", Permission: def.Permission() + ":approve"},
			{Key: "retry", Label: "Повторить", Permission: def.Permission() + ":post"},
		},
	})
	reg.RegisterDataset(&schema.Dataset{Key: "perm-journal", Name: "Журнал проб", Permission: "report:perm-journal:read"})

	byCode := map[string][3]string{}
	for _, p := range reg.Permissions() {
		byCode[p.Code] = [3]string{p.Resource, p.Action, p.Name}
	}
	require.Equal(t, map[string][3]string{
		"document:perm_doc:read":    {"perm_doc", "read", "PermDoc: чтение"},
		"document:perm_doc:approve": {"perm_doc", "approve", "PermDoc: одобрить"},
		"document:perm_doc:post":    {"perm_doc", "post", "PermDoc: проведение"},
		"report:perm-journal:read":  {"report_perm_journal", "read", "Журнал проб: чтение"},
	}, byCode)
}

func TestPermissionResourceAction(t *testing.T) {
	for code, want := range map[string][2]string{
		"catalog:currency:read":        {"currency", "read"},
		"document:crypto_invoice:post": {"crypto_invoice", "post"},
		"crypto_receipt:approve":       {"crypto_receipt", "approve"},
		"report:document-journal:read": {"report_document_journal", "read"},
		"register:exchange_rate:read":  {"exchange_rate", "read"},
	} {
		resource, action := PermissionResourceAction(code)
		require.Equal(t, want, [2]string{resource, action}, code)
	}
}
