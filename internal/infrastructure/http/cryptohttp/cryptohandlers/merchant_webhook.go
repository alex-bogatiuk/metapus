package cryptohandlers

import (
	"metapus/internal/infrastructure/crypto/webhook"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/http/cryptohttp/cryptodto"
	"metapus/internal/infrastructure/http/cryptohttp/cryptomiddleware"
	"metapus/internal/infrastructure/storage/postgres"
	platformtelemetry "metapus/internal/platform/telemetry"
)

type MerchantWebhookHandler struct {
	service      *cryptoapp.MerchantWebhookService
	repo         crypto.MerchantWebhookRepository
	secrets      crypto.WebhookSecretStore
	dispatcher   crypto.WebhookDispatcher
	deliveryRepo crypto.WebhookDeliveryRepository
}

func NewMerchantWebhookHandler(repo crypto.MerchantWebhookRepository, secrets crypto.WebhookSecretStore, deliveryRepo crypto.WebhookDeliveryRepository) *MerchantWebhookHandler {
	service, err := cryptoapp.NewMerchantWebhookService(repo, secrets, postgres.NewContextTxManager(), webhook.ValidateWebhookURL)
	if err != nil {
		panic("create merchant webhook handler: " + err.Error())
	}
	return &MerchantWebhookHandler{service: service, repo: repo, secrets: secrets, dispatcher: webhook.NewDispatcher(), deliveryRepo: deliveryRepo}
}

func (h *MerchantWebhookHandler) CreateEndpoint(c *gin.Context) {
	mc := cryptomiddleware.GetMerchant(c.Request.Context())
	var request cryptodto.CreateWebhookEndpointRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failMerchantWebhook(c, apperror.NewInvalidRequest(err))
		return
	}
	endpoint, secret, err := h.service.CreateEndpoint(c.Request.Context(), cryptoapp.CreateWebhookEndpointInput{
		MerchantID: mc.MerchantID, Name: request.Name, URL: request.URL,
		EnabledEvents: request.EnabledEvents, IsDefault: request.IsDefault,
	})
	if err != nil {
		failMerchantWebhook(c, err)
		return
	}
	response := cryptodto.WebhookEndpointFromEntity(endpoint)
	response.Secret = secret // one-time reveal
	c.JSON(http.StatusCreated, response)
}

func (h *MerchantWebhookHandler) ListEndpoints(c *gin.Context) {
	mc := cryptomiddleware.GetMerchant(c.Request.Context())
	endpoints, err := h.repo.ListEndpoints(c.Request.Context(), mc.MerchantID)
	if err != nil {
		failMerchantWebhook(c, err)
		return
	}
	items := make([]cryptodto.WebhookEndpointResponse, len(endpoints))
	for i := range endpoints {
		items[i] = cryptodto.WebhookEndpointFromEntity(&endpoints[i])
	}
	c.JSON(http.StatusOK, cryptodto.WebhookEndpointListResponse{Object: "list", Items: items, Total: len(items)})
}

func (h *MerchantWebhookHandler) GetEndpoint(c *gin.Context) {
	endpoint := h.mustEndpoint(c)
	if endpoint == nil {
		return
	}
	c.JSON(http.StatusOK, cryptodto.WebhookEndpointFromEntity(endpoint))
}

func (h *MerchantWebhookHandler) UpdateEndpoint(c *gin.Context) {
	endpoint := h.mustEndpoint(c)
	if endpoint == nil {
		return
	}
	var request cryptodto.UpdateWebhookEndpointRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failMerchantWebhook(c, apperror.NewInvalidRequest(err))
		return
	}
	endpoint.Version = request.Version
	if request.Name != nil {
		endpoint.Name = *request.Name
	}
	if request.URL != nil {
		endpoint.URL = *request.URL
	}
	if request.EnabledEvents != nil {
		endpoint.EnabledEvents = *request.EnabledEvents
	}
	if request.IsDefault != nil {
		endpoint.IsDefault = *request.IsDefault
	}
	if request.IsActive != nil {
		endpoint.IsActive = *request.IsActive
	}
	if err := h.service.UpdateEndpoint(c.Request.Context(), endpoint); err != nil {
		failMerchantWebhook(c, err)
		return
	}
	updated, err := h.repo.GetEndpoint(c.Request.Context(), endpoint.MerchantID, endpoint.ID)
	if err != nil {
		failMerchantWebhook(c, err)
		return
	}
	c.JSON(http.StatusOK, cryptodto.WebhookEndpointFromEntity(updated))
}

func (h *MerchantWebhookHandler) DeleteEndpoint(c *gin.Context) {
	mc := cryptomiddleware.GetMerchant(c.Request.Context())
	endpointID, err := id.Parse(c.Param("id"))
	if err != nil {
		failMerchantWebhook(c, apperror.NewValidation("invalid webhook endpoint id"))
		return
	}
	if err := h.service.DeleteEndpoint(c.Request.Context(), mc.MerchantID, endpointID); err != nil {
		failMerchantWebhook(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *MerchantWebhookHandler) RotateSecret(c *gin.Context) {
	mc := cryptomiddleware.GetMerchant(c.Request.Context())
	endpointID, err := id.Parse(c.Param("id"))
	if err != nil {
		failMerchantWebhook(c, apperror.NewValidation("invalid webhook endpoint id"))
		return
	}
	endpoint, secret, err := h.service.RotateSecret(c.Request.Context(), mc.MerchantID, endpointID)
	if err != nil {
		failMerchantWebhook(c, err)
		return
	}
	response := cryptodto.WebhookEndpointFromEntity(endpoint)
	response.Secret = secret // one-time reveal
	c.JSON(http.StatusOK, response)
}

func (h *MerchantWebhookHandler) TestEndpoint(c *gin.Context) {
	endpoint := h.mustEndpoint(c)
	if endpoint == nil {
		return
	}
	secret, err := h.secrets.Get(c.Request.Context(), endpoint.SecretRef)
	if err != nil {
		failMerchantWebhook(c, err)
		return
	}
	previous := ""
	if endpoint.PreviousSecretRef != "" && endpoint.PreviousSecretValidUntil != nil && time.Now().Before(*endpoint.PreviousSecretValidUntil) {
		previous, err = h.secrets.Get(c.Request.Context(), endpoint.PreviousSecretRef)
		if err != nil {
			failMerchantWebhook(c, err)
			return
		}
	}
	eventID := "evt_" + id.Compact(id.New())
	business := platformtelemetry.BusinessIDs(c.Request.Context(), eventID)
	delivery, dispatchErr := h.dispatcher.Dispatch(c.Request.Context(), h.deliveryRepo, crypto.WebhookDispatchRequest{
		MerchantID: endpoint.MerchantID, WebhookURL: endpoint.URL, ActiveSecret: secret,
		PreviousSecret: previous, EventID: eventID, APIVersion: crypto.MerchantWebhookAPIVersion,
		Event: "endpoint.test", OccurredAt: time.Now().UTC(), Data: map[string]any{"test": true}, Attempt: 1,
		Traceparent:   platformtelemetry.Traceparent(c.Request.Context()),
		CorrelationID: business.CorrelationID, CausationID: business.CausationID,
	})
	response := cryptodto.WebhookTestResponse{EventID: eventID, Success: dispatchErr == nil}
	if delivery != nil {
		response.DeliveryID = &delivery.DeliveryID
		response.StatusCode = &delivery.StatusCode
		response.DurationMs = &delivery.ResponseTimeMs
	}
	if dispatchErr != nil {
		message := dispatchErr.Error()
		response.Error = &message
	}
	c.JSON(http.StatusOK, response)
}

func (h *MerchantWebhookHandler) ListEvents(c *gin.Context) {
	mc := cryptomiddleware.GetMerchant(c.Request.Context())
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if limit < 1 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	records, total, err := h.repo.ListEvents(c.Request.Context(), mc.MerchantID, limit, offset)
	if err != nil {
		failMerchantWebhook(c, err)
		return
	}
	items := make([]cryptodto.MerchantEventResponse, 0, len(records))
	for i := range records {
		response, mapErr := merchantEventResponse(&records[i])
		if mapErr != nil {
			failMerchantWebhook(c, mapErr)
			return
		}
		items = append(items, response)
	}
	c.JSON(http.StatusOK, cryptodto.MerchantEventListResponse{Object: "list", Items: items, Total: total})
}

func (h *MerchantWebhookHandler) ListEventDeliveries(c *gin.Context) {
	mc := cryptomiddleware.GetMerchant(c.Request.Context())
	eventID := strings.TrimSpace(c.Param("id"))
	if _, err := h.repo.GetEvent(c.Request.Context(), mc.MerchantID, eventID); err != nil {
		failMerchantWebhook(c, err)
		return
	}
	deliveries, err := h.repo.ListEventDeliveries(c.Request.Context(), mc.MerchantID, eventID)
	if err != nil {
		failMerchantWebhook(c, err)
		return
	}
	c.JSON(http.StatusOK, cryptodto.MerchantEventDeliveriesResponse{Object: "list", Items: deliveries, Total: len(deliveries)})
}

func (h *MerchantWebhookHandler) RedeliverEvent(c *gin.Context) {
	mc := cryptomiddleware.GetMerchant(c.Request.Context())
	var request cryptodto.RedeliverMerchantEventRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failMerchantWebhook(c, apperror.NewInvalidRequest(err))
		return
	}
	endpointID, err := id.Parse(request.EndpointID)
	if err != nil {
		failMerchantWebhook(c, apperror.NewValidation("invalid endpointId"))
		return
	}
	if err := h.service.Redeliver(c.Request.Context(), mc.MerchantID, strings.TrimSpace(c.Param("id")), endpointID); err != nil {
		failMerchantWebhook(c, err)
		return
	}
	c.JSON(http.StatusAccepted, cryptodto.StatusResponse{Status: "pending"})
}

func (h *MerchantWebhookHandler) mustEndpoint(c *gin.Context) *crypto.MerchantWebhookEndpoint {
	mc := cryptomiddleware.GetMerchant(c.Request.Context())
	endpointID, err := id.Parse(c.Param("id"))
	if err != nil {
		failMerchantWebhook(c, apperror.NewValidation("invalid webhook endpoint id"))
		return nil
	}
	endpoint, err := h.repo.GetEndpoint(c.Request.Context(), mc.MerchantID, endpointID)
	if err != nil {
		failMerchantWebhook(c, err)
		return nil
	}
	return endpoint
}

func merchantEventResponse(record *crypto.MerchantEventRecord) (cryptodto.MerchantEventResponse, error) {
	var lifecycle crypto.LifecycleWebhookEvent
	if err := json.Unmarshal(record.Payload, &lifecycle); err != nil {
		return cryptodto.MerchantEventResponse{}, err
	}
	payload := crypto.WebhookPayload{ID: record.EventID, Object: "event", APIVersion: record.APIVersion,
		Type: record.EventType, CreatedAt: record.CreatedAt, Data: crypto.WebhookEventData{Object: lifecycle.Data}}
	if len(lifecycle.DeliveryPayload) > 0 {
		if err := json.Unmarshal(lifecycle.DeliveryPayload, &payload); err != nil {
			return cryptodto.MerchantEventResponse{}, err
		}
	}
	response := cryptodto.MerchantEventResponse{
		ID: record.EventID, Object: "event", Type: string(record.EventType),
		APIVersion: record.APIVersion, ResourceVersion: record.ResourceVersion,
		CreatedAt: record.CreatedAt,
		Payload:   payload,
	}
	if record.InvoiceID != nil {
		value := record.InvoiceID.String()
		response.InvoiceID = &value
	}
	return response, nil
}

func failMerchantWebhook(c *gin.Context, err error) {
	_ = c.Error(err)
	c.Abort()
}
