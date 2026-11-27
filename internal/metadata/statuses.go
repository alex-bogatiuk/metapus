package metadata

import (
	"fmt"

	"github.com/alex-bogatiuk/metapus/internal/core/entity"
)

// StatusModelDef is a document's status model (entity.StatusModel) as the
// UI reads it: the JSON name of the status field, the initial status, every
// status with its name in order, and the allowed changes. The form offers
// the statuses a transition from the stored one reaches and shows the
// document's status history.
type StatusModelDef struct {
	Field   string      `json:"field"`
	Initial string      `json:"initial"`
	Values  []EnumValue `json:"values"`
	// Transitions are empty when every change between Values is allowed.
	Transitions []StatusTransitionDef `json:"transitions,omitempty"`
}

// StatusTransitionDef allows a change into To from a status of From (from
// any when empty); Permission is the code a user needs for it, empty when
// the right to change the document is enough.
type StatusTransitionDef struct {
	From       []string `json:"from,omitempty"`
	To         string   `json:"to"`
	Permission string   `json:"permission,omitempty"`
}

// BindStatuses describes the status model of identity on def and gives the
// status field the model's statuses as its choices. permissionOf turns a
// transition's permission action into its code. A model whose column is no
// field of def is a registration error.
func BindStatuses(def *EntityDef, identity entity.Definition, permissionOf func(action string) string) error {
	model := identity.Statuses
	if !model.Declared() {
		return nil
	}
	values := make([]EnumValue, len(model.Values))
	for i, v := range model.Values {
		values[i] = EnumValue{Value: v.Value, Label: v.Label}
	}
	field := -1
	for i := range def.Fields {
		if def.Fields[i].DBColumn == model.Column {
			field = i
		}
	}
	if field < 0 {
		return fmt.Errorf("document %q: status column %q is not a field", identity.Name, model.Column)
	}
	def.Fields[field].EnumValues = values
	result := &StatusModelDef{Field: def.Fields[field].Name, Initial: model.Initial, Values: values}
	for _, t := range model.Transitions {
		transition := StatusTransitionDef{From: t.From, To: t.To}
		if t.Permission != "" {
			transition.Permission = permissionOf(t.Permission)
		}
		result.Transitions = append(result.Transitions, transition)
	}
	def.Statuses = result
	return nil
}
