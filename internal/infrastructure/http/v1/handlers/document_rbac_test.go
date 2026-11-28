package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/domain"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1/middleware"
	"github.com/alex-bogatiuk/metapus/internal/metadata"
)

type rbacDoc struct{ id id.ID }

type rbacCreateDTO struct {
	PostImmediately bool `json:"postImmediately"`
}

type rbacDocService struct {
	domain.DocumentService[*rbacDoc]
	created, postedAndSaved, posted, unposted, marked int
}

func (s *rbacDocService) Create(context.Context, *rbacDoc) error {
	s.created++
	return nil
}

func (s *rbacDocService) PostAndSave(context.Context, *rbacDoc) error {
	s.postedAndSaved++
	return nil
}

func (s *rbacDocService) Post(context.Context, id.ID) error {
	s.posted++
	return nil
}

func (s *rbacDocService) Unpost(context.Context, id.ID) error {
	s.unposted++
	return nil
}

func (s *rbacDocService) SetDeletionMark(context.Context, id.ID, bool) error {
	s.marked++
	return nil
}

func (s *rbacDocService) ListIDs(context.Context, domain.ListFilter, int) ([]id.ID, error) {
	return []id.ID{id.New()}, nil
}

func setupRBACDocRouter(svc *rbacDocService, perms ...string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	permSet := make(map[string]struct{}, len(perms))
	for _, p := range perms {
		permSet[p] = struct{}{}
	}
	r.Use(func(c *gin.Context) {
		c.Set("permissions_set", permSet)
		c.Request = c.Request.WithContext(appctx.WithUser(c.Request.Context(), &appctx.UserContext{
			UserID:      id.New().String(),
			Permissions: perms,
		}))
		c.Next()
	})

	h := NewBaseDocumentHandler(NewBaseHandler(), BaseDocumentHandlerConfig[*rbacDoc, rbacCreateDTO, struct{}]{
		Service:           svc,
		EntityName:        "rbac_doc",
		Permission:        "document:rbac_doc",
		MapCreateDTO:      func(rbacCreateDTO) (*rbacDoc, error) { return &rbacDoc{id: id.New()}, nil },
		MapToDTO:          func(*rbacDoc) any { return map[string]any{"ok": true} },
		IsPostImmediately: func(dto rbacCreateDTO) bool { return dto.PostImmediately },
	})
	h.ConfigureRouteAccess("document:rbac_doc", metadata.DocumentCapabilities{
		Create: true, Post: true, Unpost: true, SetDeletionMark: true, BatchAction: true, BatchActionByFilter: true,
	})

	r.POST("/docs", middleware.RequirePermission("document:rbac_doc:create"), h.Create)
	r.POST("/docs/batch-action", middleware.RequireAnyPermission("document:rbac_doc:post", "document:rbac_doc:unpost", "document:rbac_doc:delete"), h.BatchAction)
	r.POST("/docs/batch-action-by-filter", middleware.RequireAnyPermission("document:rbac_doc:post", "document:rbac_doc:unpost", "document:rbac_doc:delete"), h.BatchActionByFilter)
	return r
}

func TestCreatePostImmediatelyRequiresPostPermission(t *testing.T) {
	tests := []struct {
		name       string
		givePerms  []string
		giveBody   string
		wantStatus int
		wantPosted int
		wantDraft  int
	}{
		{
			name:       "draft allowed with create only",
			givePerms:  []string{"document:rbac_doc:create"},
			giveBody:   `{"postImmediately":false}`,
			wantStatus: http.StatusCreated,
			wantDraft:  1,
		},
		{
			name:       "postImmediately denied without post permission",
			givePerms:  []string{"document:rbac_doc:create"},
			giveBody:   `{"postImmediately":true}`,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "postImmediately allowed with create and post permissions",
			givePerms:  []string{"document:rbac_doc:create", "document:rbac_doc:post"},
			giveBody:   `{"postImmediately":true}`,
			wantStatus: http.StatusCreated,
			wantPosted: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &rbacDocService{}
			router := setupRBACDocRouter(svc, tt.givePerms...)
			req := httptest.NewRequest(http.MethodPost, "/docs", strings.NewReader(tt.giveBody))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantPosted, svc.postedAndSaved)
			require.Equal(t, tt.wantDraft, svc.created)
		})
	}
}

func TestBatchActionEnforcesPerActionPermissions(t *testing.T) {
	docID := id.New().String()
	tests := []struct {
		name       string
		givePerms  []string
		givePath   string
		giveBody   string
		wantStatus int
	}{
		{
			name:       "unpost denied when caller only has post",
			givePerms:  []string{"document:rbac_doc:post"},
			givePath:   "/docs/batch-action",
			giveBody:   `{"ids":["` + docID + `"],"action":"unpost"}`,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "setDeletionMark denied when caller only has post",
			givePerms:  []string{"document:rbac_doc:post"},
			givePath:   "/docs/batch-action",
			giveBody:   `{"ids":["` + docID + `"],"action":"setDeletionMark"}`,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "by-filter setDeletionMark denied before listing IDs when caller only has post",
			givePerms:  []string{"document:rbac_doc:post"},
			givePath:   "/docs/batch-action-by-filter",
			giveBody:   `{"action":"setDeletionMark"}`,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "unpost allowed when caller has unpost only",
			givePerms:  []string{"document:rbac_doc:unpost"},
			givePath:   "/docs/batch-action",
			giveBody:   `{"ids":["` + docID + `"],"action":"unpost"}`,
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &rbacDocService{}
			router := setupRBACDocRouter(svc, tt.givePerms...)
			req := httptest.NewRequest(http.MethodPost, tt.givePath, strings.NewReader(tt.giveBody))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			require.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}
