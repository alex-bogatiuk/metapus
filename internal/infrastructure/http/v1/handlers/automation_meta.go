package handlers

import (
	"github.com/gin-gonic/gin"
)

// AutomationMetaHandler serves static metadata for automation UI.
type AutomationMetaHandler struct {
	*BaseHandler
}

// NewAutomationMetaHandler creates a new handler.
func NewAutomationMetaHandler(base *BaseHandler) *AutomationMetaHandler {
	return &AutomationMetaHandler{BaseHandler: base}
}

// GetMeta returns all automation metadata in one call (account types, trigger types, reaction types, etc.).
func (h *AutomationMetaHandler) GetMeta(c *gin.Context) {
	h.OK(c, gin.H{
		"accountTypes": []map[string]string{
			{"value": "telegram", "label": "Telegram Bot"},
			{"value": "email", "label": "Email (SMTP)"},
			{"value": "webhook", "label": "Webhook (HTTP)"},
			{"value": "internal_notification", "label": "Internal Notification"},
		},
		"triggerTypes": []map[string]string{
			{"value": "entity_event", "label": "Entity Event (CRUD)"},
			{"value": "business_event", "label": "Business Event"},
			{"value": "scheduled", "label": "Scheduled (CRON)"},
			{"value": "incoming_webhook", "label": "Incoming Webhook"},
		},
		"reactionTypes": []map[string]string{
			{"value": "notify", "label": "Send Notification"},
			{"value": "webhook_call", "label": "Call Webhook"},
			{"value": "chain", "label": "Chain Reaction"},
			{"value": "create_record", "label": "Create Record"},
		},
		"subscriberTypes": []map[string]string{
			{"value": "channel", "label": "Delivery Channel"},
			{"value": "user", "label": "Specific User"},
			{"value": "role", "label": "All Users in Role"},
			{"value": "doc_field", "label": "Dynamic (Document Field)"},
		},
		"deliveryMethods": []map[string]string{
			{"value": "push", "label": "Push / In-App"},
			{"value": "email", "label": "Email"},
			{"value": "telegram", "label": "Telegram"},
		},
		"messageFormats": []map[string]string{
			{"value": "text", "label": "Plain Text"},
			{"value": "html", "label": "HTML"},
			{"value": "markdown", "label": "Markdown"},
		},
		"historyStatuses": []map[string]string{
			{"value": "success", "label": "Success"},
			{"value": "error", "label": "Error"},
			{"value": "condition_false", "label": "Condition Not Met"},
			{"value": "skipped", "label": "Skipped (Cooldown)"},
			{"value": "pending", "label": "Pending"},
		},
	})
}

// RegisterRoutes registers the meta endpoints.
func (h *AutomationMetaHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/automation-meta", h.GetMeta)
}
