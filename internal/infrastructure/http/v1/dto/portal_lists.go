package dto

import "metapus/internal/domain/crypto"

type PortalMerchantListResponse struct {
	Items []PortalMerchantItem `json:"items"`
}

type PortalCurrencyListResponse struct {
	Items []PortalCurrencyItem `json:"items"`
}

type PortalChartResponse struct {
	Items []PortalChartPoint `json:"items"`
}

type MerchantAPIKeyListResponse struct {
	Items []MerchantAPIKeyListItem `json:"items"`
	Total int                      `json:"total"`
}

type MerchantUserListResponse struct {
	Items []MerchantUserItem `json:"items"`
	Total int                `json:"total"`
}

type PortalWithdrawalAddressCreatedResponse struct {
	Object  string `json:"object"`
	ID      string `json:"id"`
	Version int    `json:"version"`
}
type PortalWithdrawalCreatedResponse struct {
	Object  string `json:"object"`
	ID      string `json:"id"`
	Version int    `json:"version"`
	Number  string `json:"number"`
	Status  string `json:"status"`
}
type StatusResponse struct {
	Status string `json:"status"`
}
type ReasonRequest struct {
	Reason string `json:"reason"`
}

type WebhookEndpointListResponse struct {
	Object string                    `json:"object"`
	Items  []WebhookEndpointResponse `json:"items"`
	Total  int                       `json:"total"`
}
type MerchantEventListResponse struct {
	Object string                  `json:"object"`
	Items  []MerchantEventResponse `json:"items"`
	Total  int                     `json:"total"`
}
type MerchantEventDeliveriesResponse struct {
	Object string                   `json:"object"`
	Items  []crypto.WebhookDelivery `json:"items"`
	Total  int                      `json:"total"`
}

// A delivery can exist with an unknown HTTP status. The outer pointer preserves
// absence (no delivery) separately from an explicitly null delivery result.
type WebhookTestResponse struct {
	EventID    string  `json:"eventId"`
	Success    bool    `json:"success"`
	DeliveryID *string `json:"deliveryId,omitempty"`
	StatusCode **int   `json:"statusCode,omitzero"`
	DurationMs **int   `json:"durationMs,omitzero"`
	Error      *string `json:"error,omitempty"`
}
