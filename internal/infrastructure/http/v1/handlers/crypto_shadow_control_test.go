package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/eventlog"
	"metapus/internal/infrastructure/http/v1/middleware"
)

type shadowHandlerRepo struct {
	state  crypto.ShadowControlState
	writes int
}

func (r *shadowHandlerRepo) GetShadowControl(context.Context, id.ID) (crypto.ShadowControlState, error) {
	return r.state, nil
}
func (r *shadowHandlerRepo) SetShadowPaused(_ context.Context, _ id.ID, _ int, paused bool) (crypto.ShadowControlState, error) {
	r.writes++
	r.state.Paused = paused
	r.state.Version++
	return r.state, nil
}

type shadowHandlerTx struct{}

func (shadowHandlerTx) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

type shadowHandlerAudit struct{}

func (shadowHandlerAudit) Write(context.Context, eventlog.Event) error        { return nil }
func (shadowHandlerAudit) WriteBatch(context.Context, []eventlog.Event) error { return nil }

func TestShadowControlHTTPGuardsAndExplicitFalse(t *testing.T) {
	tests := []struct {
		name, body               string
		admin, activated, paused bool
		want, writes             int
	}{
		{name: "pause", body: `{"version":1,"paused":true,"reason":"maintenance"}`, admin: true, want: 202, writes: 1},
		{name: "resume accepts explicit false", body: `{"version":1,"paused":false,"reason":"resume"}`, admin: true, paused: true, want: 202, writes: 1},
		{name: "omitted intent", body: `{"version":1,"reason":"maintenance"}`, admin: true, want: 400},
		{name: "null intent", body: `{"version":1,"paused":null,"reason":"maintenance"}`, admin: true, want: 400},
		{name: "stale version", body: `{"version":2,"paused":true,"reason":"maintenance"}`, admin: true, want: 409},
		{name: "permission alone is insufficient", body: `{"version":1,"paused":true,"reason":"maintenance"}`, want: 403},
		{name: "activation fences prepared command", body: `{"version":1,"paused":true,"reason":"maintenance"}`, admin: true, activated: true, want: 400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &shadowHandlerRepo{state: crypto.ShadowControlState{Family: crypto.ChainFamilyTRON, Mode: "shadow", Version: 1, Paused: tt.paused}}
			if tt.activated {
				now := time.Now()
				repo.state.ActivatedAt = &now
				repo.state.Mode = "hybrid"
			}
			router := gin.New()
			router.Use(middleware.ErrorHandler(), func(c *gin.Context) {
				ctx := tenant.WithTxManager(c.Request.Context(), shadowHandlerTx{})
				ctx = appctx.WithUser(ctx, &appctx.UserContext{UserID: id.New().String(), IsAdmin: tt.admin, Permissions: []string{"crypto_network:update"}})
				c.Request = c.Request.WithContext(ctx)
				c.Next()
			})
			h := NewCryptoNetworkHandler(NewBaseHandler(), nil, crypto.NewShadowControlAdminService(repo, shadowHandlerAudit{}))
			h.RegisterRoutes(router.Group(""), func(c *gin.Context) { c.Next() })
			request := httptest.NewRequest(http.MethodPost, "/admin/crypto/networks/"+id.New().String()+"/shadow", strings.NewReader(tt.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, tt.want, response.Code, response.Body.String())
			require.Equal(t, tt.writes, repo.writes)
		})
	}
}
