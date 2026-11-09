package entity

// ActionAvailability is an advisory, actor-specific read projection. A command
// must authorize again; this value is never accepted in a mutation DTO.
type ActionAvailability struct {
	Allowed      bool   `json:"allowed"`
	ReasonCode   string `json:"reasonCode,omitempty"`
	DecisionMode string `json:"decisionMode,omitempty"`
}
