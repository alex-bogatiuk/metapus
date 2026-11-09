package handlers

import (
	"github.com/gin-gonic/gin"

	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/reporting/compiler"
)

// AutomationMetaHandler serves static metadata for automation UI.
type AutomationMetaHandler struct {
	*BaseHandler
	compiler *compiler.Compiler // nil-safe — datasets endpoint returns empty list if not set
}

// NewAutomationMetaHandler creates a new handler.
func NewAutomationMetaHandler(base *BaseHandler, comp *compiler.Compiler) *AutomationMetaHandler {
	return &AutomationMetaHandler{BaseHandler: base, compiler: comp}
}

// GetMeta returns all automation metadata in one call (account types, trigger types, reaction types, etc.).
func (h *AutomationMetaHandler) GetMeta(c *gin.Context) {
	h.OK(c, dto.AutomationMetadataResponse{
		AccountTypes: []dto.AutomationEnumItem{
			{Value: "telegram", Label: "Telegram Bot"},
			{Value: "email", Label: "Email (SMTP)"},
			{Value: "webhook", Label: "Webhook (HTTP)"},
			{Value: "internal_notification", Label: "Internal Notification"},
		},
		TriggerTypes: []dto.AutomationEnumItem{
			{Value: "entity_event", Label: "Entity Event (CRUD)"},
			{Value: "business_event", Label: "Business Event"},
			{Value: "scheduled", Label: "Scheduled (CRON)"},
			{Value: "incoming_webhook", Label: "Incoming Webhook"},
		},
		ReactionTypes: []dto.AutomationEnumItem{
			{Value: "notify", Label: "Send Notification"},
			{Value: "webhook_call", Label: "Call Webhook"},
			{Value: "generate_report", Label: "Generate & Send Report"},
		},
		SubscriberTypes: []dto.AutomationEnumItem{
			{Value: "channel", Label: "Delivery Channel"},
			{Value: "user", Label: "Specific User"},
			{Value: "role", Label: "All Users in Role"},
			{Value: "doc_field", Label: "Dynamic (Document Field)"},
		},
		DeliveryMethods: []dto.AutomationEnumItem{
			{Value: "push", Label: "Push / In-App"},
			{Value: "email", Label: "Email"},
			{Value: "telegram", Label: "Telegram"},
		},
		MessageFormats: []dto.AutomationEnumItem{
			{Value: "text", Label: "Plain Text"},
			{Value: "html", Label: "HTML"},
			{Value: "markdown", Label: "Markdown"},
		},
		HistoryStatuses: []dto.AutomationEnumItem{
			{Value: "success", Label: "Success"},
			{Value: "error", Label: "Error"},
			{Value: "condition_false", Label: "Condition Not Met"},
			{Value: "skipped", Label: "Skipped (Cooldown)"},
			{Value: "pending", Label: "Pending"},
		},
	})
}

// GetDatasets returns available report datasets for the report configuration UI.
// Used when reaction_type = 'generate_report' to populate the dataset selector.
func (h *AutomationMetaHandler) GetDatasets(c *gin.Context) {

	if h.compiler == nil {
		h.OK(c, []dto.AutomationDatasetSummary{})
		return
	}

	all := h.compiler.AllDatasets()
	result := make([]dto.AutomationDatasetSummary, 0, len(all))
	for _, ds := range all {
		result = append(result, dto.AutomationDatasetSummary{
			Key:  ds.Key,
			Name: ds.Name,
		})
	}

	h.OK(c, result)
}

// RegisterRoutes registers the meta endpoints.
func (h *AutomationMetaHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/automation-meta", h.GetMeta)
	rg.GET("/automation-meta/datasets", h.GetDatasets)
}
