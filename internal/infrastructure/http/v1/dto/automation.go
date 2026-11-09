package dto

import (
	"metapus/internal/core/id"
	"metapus/internal/domain/automations"
)

type UpdateAutomationCredentialsRequest struct {
	Credentials string `json:"credentials"`
}

type AutomationToggleResponse struct {
	IsActive bool `json:"isActive"`
}

type AutomationHistoryListResponse struct {
	Items []automations.HistoryEntry `json:"items"`
	Total int                        `json:"total"`
}

type AutomationReplayResponse struct {
	ID     id.ID  `json:"id"`
	Status string `json:"status"`
}

type AutomationBatchReplayResponse struct {
	Queued int `json:"queued"`
}

type AutomationEnumItem struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type AutomationMetadataResponse struct {
	AccountTypes    []AutomationEnumItem `json:"accountTypes"`
	TriggerTypes    []AutomationEnumItem `json:"triggerTypes"`
	ReactionTypes   []AutomationEnumItem `json:"reactionTypes"`
	SubscriberTypes []AutomationEnumItem `json:"subscriberTypes"`
	DeliveryMethods []AutomationEnumItem `json:"deliveryMethods"`
	MessageFormats  []AutomationEnumItem `json:"messageFormats"`
	HistoryStatuses []AutomationEnumItem `json:"historyStatuses"`
}

type AutomationDatasetSummary struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}
