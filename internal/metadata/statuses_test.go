package metadata

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/core/entity"
)

func TestBindStatusesDescribesTheModelForTheForm(t *testing.T) {
	t.Parallel()
	identity := entity.Definition{Key: "ticket", Name: "Ticket", Kind: entity.DocumentKind, Statuses: entity.StatusModel{
		Column: "status", Initial: "new",
		Values:      []entity.StatusValue{{Value: "new", Label: "Новая"}, {Value: "closed", Label: "Закрыта"}},
		Transitions: []entity.StatusTransition{{To: "closed"}, {From: []string{"closed"}, To: "new", Permission: "reopen"}},
	}}
	def := &EntityDef{Fields: []FieldDef{{DBColumn: "number", Name: "number"}, {DBColumn: "status", Name: "status"}}}
	require.NoError(t, BindStatuses(def, identity, func(action string) string { return "document:ticket:" + action }))

	require.Equal(t, &StatusModelDef{
		Field: "status", Initial: "new",
		Values: []EnumValue{{Value: "new", Label: "Новая"}, {Value: "closed", Label: "Закрыта"}},
		Transitions: []StatusTransitionDef{
			{To: "closed"},
			{From: []string{"closed"}, To: "new", Permission: "document:ticket:reopen"},
		},
	}, def.Statuses)
	require.Equal(t, def.Statuses.Values, def.Fields[1].EnumValues, "the status field offers the statuses")

	missing := &EntityDef{Fields: []FieldDef{{DBColumn: "number", Name: "number"}}}
	require.Error(t, BindStatuses(missing, identity, nil))
	none := &EntityDef{}
	require.NoError(t, BindStatuses(none, entity.Definition{}, nil))
	require.Nil(t, none.Statuses)
}
