package cryptohandlers

import (
	"github.com/gin-gonic/gin"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/http/cryptohttp/cryptomiddleware"
)

// PortalWebhookRegistryHandler reuses the merchant webhook application
// handlers after the portal authorization layer has resolved merchant scope.
type PortalWebhookRegistryHandler struct{ merchant *MerchantWebhookHandler }

func NewPortalWebhookRegistryHandler(merchant *MerchantWebhookHandler) *PortalWebhookRegistryHandler {
	return &PortalWebhookRegistryHandler{merchant: merchant}
}

func (h *PortalWebhookRegistryHandler) withMerchant(c *gin.Context, next gin.HandlerFunc) {
	merchantID, err := parseRequiredMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	ctx := cryptomiddleware.WithMerchant(c.Request.Context(), &cryptomiddleware.MerchantContext{MerchantID: merchantID})
	c.Request = c.Request.WithContext(ctx)
	next(c)
}

func (h *PortalWebhookRegistryHandler) CreateEndpoint(c *gin.Context) {
	h.withMerchant(c, h.merchant.CreateEndpoint)
}
func (h *PortalWebhookRegistryHandler) ListEndpoints(c *gin.Context) {
	h.withMerchant(c, h.merchant.ListEndpoints)
}
func (h *PortalWebhookRegistryHandler) GetEndpoint(c *gin.Context) {
	h.withMerchant(c, h.merchant.GetEndpoint)
}
func (h *PortalWebhookRegistryHandler) UpdateEndpoint(c *gin.Context) {
	h.withMerchant(c, h.merchant.UpdateEndpoint)
}
func (h *PortalWebhookRegistryHandler) DeleteEndpoint(c *gin.Context) {
	h.withMerchant(c, h.merchant.DeleteEndpoint)
}
func (h *PortalWebhookRegistryHandler) RotateSecret(c *gin.Context) {
	h.withMerchant(c, h.merchant.RotateSecret)
}
func (h *PortalWebhookRegistryHandler) TestEndpoint(c *gin.Context) {
	h.withMerchant(c, h.merchant.TestEndpoint)
}
func (h *PortalWebhookRegistryHandler) ListEvents(c *gin.Context) {
	h.withMerchant(c, h.merchant.ListEvents)
}
func (h *PortalWebhookRegistryHandler) ListEventDeliveries(c *gin.Context) {
	h.withMerchant(c, h.merchant.ListEventDeliveries)
}
func (h *PortalWebhookRegistryHandler) RedeliverEvent(c *gin.Context) {
	h.withMerchant(c, h.merchant.RedeliverEvent)
}
