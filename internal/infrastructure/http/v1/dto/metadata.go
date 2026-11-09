package dto

import "metapus/internal/metadata"

// EntitySummaryResponse is the lightweight metadata contract used by navigation.
type EntitySummaryResponse struct {
	Key          string                         `json:"key"`
	Name         string                         `json:"name"`
	Type         metadata.EntityType            `json:"type"`
	Presentation metadata.Presentation          `json:"presentation"`
	RoutePrefix  string                         `json:"routePrefix,omitempty"`
	Capabilities *metadata.DocumentCapabilities `json:"capabilities,omitempty"`
	Actions      []metadata.EntityActionDef     `json:"actions,omitempty"`
}
