package cryptohandlers

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/cryptoapp"
	pbo "github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/documents/crypto_provider_balance_operation"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/appctx"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/command"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/domain"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/entity"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/http/handlers"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/http/middleware"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
)

func TestProviderOpeningDecisionRejectsClientAuthorityFields(t *testing.T) {
	for _, action := range []string{"approve", "reject"} {
		for _, body := range []string{
			`{"version":1,"reason":"checked","decisionMode":"admin_self"}`,
			`{"version":1,"reason":"checked","actorId":"10000000-0000-4000-8000-000000000001"}`,
			`{"version":1,"reason":"checked","isAdmin":true}`,
			`{"version":1,"reason":"checked","reason":"changed"}`,
		} {
			h := &CryptoProviderBalanceOperationHandler{base: handlers.NewBaseHandler()}
			router := gin.New()
			router.Use(middleware.ErrorHandler())
			if action == "approve" {
				router.POST("/:id/approve", h.Approve)
			} else {
				router.POST("/:id/reject", h.Reject)
			}
			request := httptest.NewRequest("POST", "/"+id.New().String()+"/"+action, strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, 400, response.Code)
		}
	}
}

type providerOpeningReadService struct {
	domain.DocumentService[*pbo.CryptoProviderBalanceOperation]
	doc *pbo.CryptoProviderBalanceOperation
}

func (s providerOpeningReadService) GetByID(context.Context, id.ID) (*pbo.CryptoProviderBalanceOperation, error) {
	return s.doc, nil
}

func TestProviderOpeningDetailIncludesActorSpecificAvailability(t *testing.T) {
	doc := &pbo.CryptoProviderBalanceOperation{Document: entity.NewDocument(), Status: pbo.StatusPendingApproval}
	doc.CreatedBy = id.New()
	h := NewCryptoProviderBalanceOperationHandler(handlers.NewBaseHandler(), providerOpeningReadService{doc: doc}, &cryptoapp.ProviderBalanceWorkflowService{}, nil, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(appctx.WithUser(c.Request.Context(), &appctx.UserContext{UserID: doc.CreatedBy.String(), Permissions: []string{"document:crypto_provider_balance_operation:approve"}}))
	})
	router.GET("/:id", h.Get)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/"+doc.ID.String(), nil))
	require.Equal(t, 200, response.Code)
	var detail struct {
		ID                 id.ID                                `json:"id"`
		ActionAvailability map[string]entity.ActionAvailability `json:"actionAvailability"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &detail))
	require.Equal(t, doc.ID, detail.ID)
	require.False(t, detail.ActionAvailability["approve"].Allowed)
	require.Equal(t, pbo.ReasonSelfDecisionForbidden, detail.ActionAvailability["approve"].ReasonCode)
}

func TestProviderOpeningResponseReplaysApplicationResult(t *testing.T) {
	gin.SetMode(gin.TestMode)
	doc := cryptoapp.ProviderBalanceResult{ID: id.New(), Number: "PBO-1", Status: "posted", Version: 2}
	h := &CryptoProviderBalanceOperationHandler{base: handlers.NewBaseHandler()}
	var bodies []string
	for _, replayed := range []bool{false, true} {
		response := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(response)
		h.writeCommandResult(ctx, command.Execution{Result: command.Result{Outcome: doc.Status, Payload: command.MustPayload(doc)}, Replayed: replayed}, nil)
		require.Equal(t, 200, response.Code)
		if replayed {
			require.Equal(t, "true", response.Header().Get("Idempotent-Replayed"))
		} else {
			require.Empty(t, response.Header().Get("Idempotent-Replayed"))
		}
		require.JSONEq(t, string(command.MustPayload(doc)), response.Body.String())
		bodies = append(bodies, response.Body.String())
	}
	require.Equal(t, bodies[0], bodies[1])
}
