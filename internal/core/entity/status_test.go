package entity

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func orderStatuses() StatusModel {
	return StatusModel{
		Column: "status", Initial: "open",
		Values: []StatusValue{{"open", "Открыт"}, {"in_progress", "В работе"}, {"done", "Выполнен"}},
		Transitions: []StatusTransition{
			{From: []string{"open"}, To: "in_progress"},
			{To: "done"},
			{From: []string{"done"}, To: "open", Permission: "reopen", Label: "Возобновление"},
		},
	}
}

func TestStatusModelAllowsTheDeclaredChanges(t *testing.T) {
	t.Parallel()
	model := orderStatuses()

	_, ok := model.TransitionsFor("open", "in_progress")
	require.True(t, ok)
	_, ok = model.TransitionsFor("in_progress", "open")
	require.False(t, ok, "a change no transition names is refused")
	transitions, ok := model.TransitionsFor("in_progress", "done")
	require.True(t, ok, "a transition without From comes from any status")
	require.Len(t, transitions, 1)
	transitions, ok = model.TransitionsFor("done", "open")
	require.True(t, ok)
	require.Equal(t, "reopen", transitions[0].Permission)

	transitions, ok = model.TransitionsFor("done", "done")
	require.True(t, ok, "keeping the status needs no transition")
	require.Empty(t, transitions)
	_, ok = model.TransitionsFor("", "open")
	require.True(t, ok, "a new document starts in the initial status")
	_, ok = model.TransitionsFor("", "in_progress")
	require.True(t, ok, "a new document may start where the initial status leads")

	model.Transitions = nil
	_, ok = model.TransitionsFor("in_progress", "open")
	require.True(t, ok, "without transitions every change is allowed")
}

func TestStatusModelLabelsAndPermissions(t *testing.T) {
	t.Parallel()
	model := orderStatuses()
	require.Equal(t, "В работе", model.Label("in_progress"))
	require.Equal(t, "lost", model.Label("lost"))
	require.True(t, model.Known("done"))
	require.False(t, model.Known(""))
	require.Equal(t, []StatusTransition{{Permission: "reopen", Label: "Возобновление"}}, model.Permissions())
}

func TestDefinitionRefusesAnInconsistentStatusModel(t *testing.T) {
	t.Parallel()
	document := func(model StatusModel) Definition {
		return Definition{Key: "order", Name: "Order", Route: "orders", Table: "doc_orders", Kind: DocumentKind, Statuses: model}
	}
	require.NoError(t, document(orderStatuses()).Validate())
	require.NoError(t, document(StatusModel{}).Validate())

	broken := map[string]func(*StatusModel){
		"no column":        func(m *StatusModel) { m.Column = "" },
		"no values":        func(m *StatusModel) { m.Values = nil },
		"unknown initial":  func(m *StatusModel) { m.Initial = "lost" },
		"twice":            func(m *StatusModel) { m.Values = append(m.Values, StatusValue{"open", "Снова"}) },
		"unnamed":          func(m *StatusModel) { m.Values[0].Label = "" },
		"unknown target":   func(m *StatusModel) { m.Transitions[0].To = "lost" },
		"unknown source":   func(m *StatusModel) { m.Transitions[0].From = []string{"lost"} },
		"standard action":  func(m *StatusModel) { m.Transitions[2].Permission = "update" },
		"malformed action": func(m *StatusModel) { m.Transitions[2].Permission = "Re-open" },
		"bad column":       func(m *StatusModel) { m.Column = "Status" },
	}
	for name, breakIt := range broken {
		model := orderStatuses()
		breakIt(&model)
		require.Error(t, document(model).Validate(), name)
	}

	catalog := document(orderStatuses())
	catalog.Kind, catalog.Table = CatalogKind, "cat_orders"
	require.Error(t, catalog.Validate(), "only documents declare statuses")
}
