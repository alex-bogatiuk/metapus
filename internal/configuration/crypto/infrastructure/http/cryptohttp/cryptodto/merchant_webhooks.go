package cryptodto

import (
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
)

type CreateWebhookEndpointRequest struct {
	Name          string                    `json:"name" binding:"required"`
	URL           string                    `json:"url" binding:"required"`
	EnabledEvents []crypto.WebhookEventType `json:"enabledEvents" binding:"required"`
	IsDefault     bool                      `json:"isDefault"`
}

type UpdateWebhookEndpointRequest struct {
	Version       int                        `json:"version" binding:"required,min=1"`
	Name          *string                    `json:"name"`
	URL           *string                    `json:"url"`
	EnabledEvents *[]crypto.WebhookEventType `json:"enabledEvents"`
	IsDefault     *bool                      `json:"isDefault"`
	IsActive      *bool                      `json:"isActive"`
}

type WebhookEndpointResponse struct {
	ID                       string                    `json:"id"`
	Object                   string                    `json:"object"`
	Name                     string                    `json:"name"`
	URL                      string                    `json:"url"`
	EnabledEvents            []crypto.WebhookEventType `json:"enabledEvents"`
	IsDefault                bool                      `json:"isDefault"`
	IsActive                 bool                      `json:"isActive"`
	SecretVersion            int                       `json:"secretVersion"`
	Version                  int                       `json:"version"`
	PreviousSecretValidUntil *time.Time                `json:"previousSecretValidUntil,omitempty"`
	CreatedAt                time.Time                 `json:"createdAt"`
	UpdatedAt                time.Time                 `json:"updatedAt"`
	Secret                   string                    `json:"secret,omitempty"`
}

func WebhookEndpointFromEntity(endpoint *crypto.MerchantWebhookEndpoint) WebhookEndpointResponse {
	return WebhookEndpointResponse{
		ID: endpoint.ID.String(), Object: "webhookEndpoint", Name: endpoint.Name,
		URL: endpoint.URL, EnabledEvents: endpoint.EnabledEvents,
		IsDefault: endpoint.IsDefault, IsActive: endpoint.IsActive,
		SecretVersion:            endpoint.SecretVersion,
		Version:                  endpoint.Version,
		PreviousSecretValidUntil: endpoint.PreviousSecretValidUntil,
		CreatedAt:                endpoint.CreatedAt, UpdatedAt: endpoint.UpdatedAt,
	}
}

type MerchantEventResponse struct {
	ID              string                `json:"id"`
	Object          string                `json:"object"`
	Type            string                `json:"type"`
	APIVersion      string                `json:"apiVersion"`
	ResourceVersion int                   `json:"resourceVersion"`
	InvoiceID       *string               `json:"invoiceId,omitempty"`
	CreatedAt       time.Time             `json:"createdAt"`
	Payload         crypto.WebhookPayload `json:"payload"`
}

type RedeliverMerchantEventRequest struct {
	EndpointID string `json:"endpointId" binding:"required"`
}
