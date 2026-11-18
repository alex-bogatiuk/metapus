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
	// Code is how the server fills a catalog's code; forms show it on an
	// empty code field.
	Code *metadata.CodeAssignment `json:"code,omitempty"`
	// ReadPermission is the permission that opens the entity's list and
	// records; navigation hides the entity from users without it.
	ReadPermission string `json:"readPermission,omitempty"`
}
