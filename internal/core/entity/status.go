package entity

import (
	"fmt"
	"slices"
)

// StatusModel declares a document's statuses (статусная модель): the column
// that holds the status, the status a new document starts in, every status
// with its name, and the changes allowed between them. The zero value
// declares none.
//
// The document's repository enforces it on every write, whichever path the
// write takes (the form, posting, a draft filled from another document, a
// copy, a hook): a status outside Values is refused, a change is refused
// unless a transition allows it and the user holds its permission, and each
// change is recorded in the document's status history.
type StatusModel struct {
	// Column is the document's column holding the status, a string-kind field.
	Column string
	// Initial is the status a document written without one gets.
	Initial string
	// Values are the statuses in the order the UI lists them.
	Values []StatusValue
	// Transitions are the allowed changes. None declared allows every change
	// between Values; once one is declared, a change no transition names is
	// refused.
	Transitions []StatusTransition
}

// StatusValue is one status and its name.
type StatusValue struct {
	Value string
	Label string
}

// StatusTransition allows a change into To from any status of From (from
// every other status when From is empty). With a Permission, only a user
// holding "<kind>:<key>:<Permission>" may make the change; Label names that
// permission in the role editor. Several transitions may share a permission.
type StatusTransition struct {
	From       []string
	To         string
	Permission string
	Label      string
}

// Declared reports whether the model declares any statuses.
func (m StatusModel) Declared() bool { return m.Column != "" }

// Known reports whether value is one of the model's statuses.
func (m StatusModel) Known(value string) bool {
	return slices.ContainsFunc(m.Values, func(v StatusValue) bool { return v.Value == value })
}

// Label is the name of value, or value itself when it is not one of the
// model's statuses.
func (m StatusModel) Label(value string) string {
	for _, v := range m.Values {
		if v.Value == value {
			return v.Label
		}
	}
	return value
}

// TransitionsFor returns the transitions that allow a change from → to. A
// change into the status a document already has needs none and returns
// (nil, true); a change no transition allows returns (nil, false). A new
// document comes from Initial: starting in Initial needs no transition.
func (m StatusModel) TransitionsFor(from, to string) ([]StatusTransition, bool) {
	if from == "" {
		from = m.Initial
	}
	if from == to {
		return nil, true
	}
	if len(m.Transitions) == 0 {
		return nil, true
	}
	var allowed []StatusTransition
	for _, t := range m.Transitions {
		if t.To == to && (len(t.From) == 0 || slices.Contains(t.From, from)) {
			allowed = append(allowed, t)
		}
	}
	return allowed, len(allowed) > 0
}

// Permissions returns the distinct permission actions of the transitions,
// each with the label of its first transition.
func (m StatusModel) Permissions() []StatusTransition {
	var result []StatusTransition
	for _, t := range m.Transitions {
		if t.Permission != "" && !slices.ContainsFunc(result, func(p StatusTransition) bool { return p.Permission == t.Permission }) {
			result = append(result, StatusTransition{Permission: t.Permission, Label: t.Label})
		}
	}
	return result
}

// standardDocumentActions are the actions of a document's standard
// permissions; a transition's permission may not take one of them.
var standardDocumentActions = []string{"read", "create", "update", "delete", "post", "unpost"}

func (m StatusModel) validate(entityName string) error {
	if !m.Declared() {
		if m.Initial != "" || len(m.Values) > 0 || len(m.Transitions) > 0 {
			return fmt.Errorf("entity %q: statuses need the column that holds them", entityName)
		}
		return nil
	}
	if !sqlName.MatchString(m.Column) {
		return fmt.Errorf("entity %q: status column %q must be words joined by single underscores", entityName, m.Column)
	}
	if len(m.Values) == 0 {
		return fmt.Errorf("entity %q: statuses declare no values", entityName)
	}
	seen := map[string]bool{}
	for _, v := range m.Values {
		if v.Value == "" || v.Label == "" || seen[v.Value] {
			return fmt.Errorf("entity %q: status %q must have a name and be declared once", entityName, v.Value)
		}
		seen[v.Value] = true
	}
	if !m.Known(m.Initial) {
		return fmt.Errorf("entity %q: initial status %q is not one of the statuses", entityName, m.Initial)
	}
	for _, t := range m.Transitions {
		if !m.Known(t.To) {
			return fmt.Errorf("entity %q: transition into unknown status %q", entityName, t.To)
		}
		for _, from := range t.From {
			if !m.Known(from) {
				return fmt.Errorf("entity %q: transition from unknown status %q", entityName, from)
			}
		}
		if t.Permission != "" && (!definitionKey.MatchString(t.Permission) || slices.Contains(standardDocumentActions, t.Permission)) {
			return fmt.Errorf("entity %q: transition permission %q must be a snake_case action other than the standard ones", entityName, t.Permission)
		}
	}
	return nil
}
