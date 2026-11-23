package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/json"
	"github.com/alex-bogatiuk/metapus/internal/core/tenant"
)

func TestSensitiveActionGuardRequiresFreshStepUp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := id.New()
	sessionID := id.New()
	checker := &fakeStepUpChecker{ok: false}
	router := newStepUpGuardTestRouter(userID, sessionID, checker)

	req := httptest.NewRequest(http.MethodPost, "/sensitive", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Code    string         `json:"code"`
		Details map[string]any `json:"details"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != apperror.CodeStepUpRequired {
		t.Fatalf("expected %s, got %s", apperror.CodeStepUpRequired, body.Code)
	}
	if body.Details["actionCode"] != "system.settings.update" {
		t.Fatalf("unexpected actionCode detail: %#v", body.Details["actionCode"])
	}
	if checker.calls != 1 {
		t.Fatalf("expected checker to be called once, got %d", checker.calls)
	}
}

func TestSensitiveActionGuardAllowsFreshStepUp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := id.New()
	sessionID := id.New()
	checker := &fakeStepUpChecker{ok: true}
	router := newStepUpGuardTestRouter(userID, sessionID, checker)

	req := httptest.NewRequest(http.MethodPost, "/sensitive", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if checker.calls != 1 {
		t.Fatalf("expected checker to be called once, got %d", checker.calls)
	}
}

func TestRequireSystemAdminRejectsSecurityAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		user := &appctx.UserContext{Roles: []string{"security_admin"}}
		c.Request = c.Request.WithContext(appctx.WithUser(c.Request.Context(), user))
		c.Next()
	})
	router.POST("/system-admin-only", RequireSystemAdmin(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/system-admin-only", nil)
	router.ServeHTTP(recorder, req)
	if recorder.Code == http.StatusNoContent {
		t.Fatal("security_admin must not pass system administrator guard")
	}
}

func newStepUpGuardTestRouter(userID, sessionID id.ID, checker *fakeStepUpChecker) *gin.Engine {
	router := gin.New()
	router.Use(ErrorHandler())
	router.Use(func(c *gin.Context) {
		ctx := tenant.WithTenant(c.Request.Context(), &tenant.Tenant{ID: "tenant-a"})
		ctx = appctx.WithUser(ctx, &appctx.UserContext{
			UserID:    userID.String(),
			SessionID: sessionID.String(),
			Email:     "admin@example.com",
		})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	router.POST("/sensitive", SensitiveActionGuard(checker, "system.settings.update", nil), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	return router
}

type fakeStepUpChecker struct {
	ok    bool
	calls int
}

func (f *fakeStepUpChecker) HasFreshStepUp(ctx context.Context, sessionID, userID id.ID, tenantID, actionCode string, now time.Time) (bool, error) {
	f.calls++
	return f.ok, nil
}
