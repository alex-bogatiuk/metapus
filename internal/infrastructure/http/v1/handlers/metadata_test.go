package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/json"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/metadata"
)

func TestListEntitiesSummaryIncludesDocumentCapabilitiesAndActions(t *testing.T) {
	gin.SetMode(gin.TestMode)

	caps := metadata.DocumentCapabilities{List: true, Read: true}
	reg := metadata.NewRegistry()
	reg.Register(metadata.EntityDef{
		Key:         "withdrawal_request",
		Name:        "WithdrawalRequest",
		Type:        metadata.TypeDocument,
		RoutePrefix: "withdrawal-request",
		Presentation: metadata.Presentation{
			Singular: "Заявка на вывод",
			Plural:   "Заявки на вывод",
		},
		Capabilities: &caps,
		Actions: []metadata.EntityActionDef{
			{
				Key:        "approve",
				Label:      "Утвердить",
				Method:     "POST",
				Path:       "/:id/approve",
				Scope:      metadata.EntityActionScopeItem,
				Permission: "document:withdrawal_request:post",
			},
		},
	})

	router := gin.New()
	handler := NewMetadataHandler(reg, nil)
	router.GET("/meta/entities", handler.ListEntitiesSummary)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/meta/entities", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}

	var summaries []dto.EntitySummaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &summaries); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected one summary, got %d", len(summaries))
	}
	if summaries[0].Capabilities == nil || !summaries[0].Capabilities.List || !summaries[0].Capabilities.Read || summaries[0].Capabilities.Create {
		t.Fatalf("unexpected capabilities: %#v", summaries[0].Capabilities)
	}
	if len(summaries[0].Actions) != 1 || summaries[0].Actions[0].Key != "approve" {
		t.Fatalf("unexpected actions: %#v", summaries[0].Actions)
	}
}
