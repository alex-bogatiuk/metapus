package automation

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/json"
	"github.com/alex-bogatiuk/metapus/internal/core/types"
	"github.com/alex-bogatiuk/metapus/internal/domain/automations"
)

func TestMoneyTemplatesPreserveEveryMinorUnit(t *testing.T) {
	engine, err := NewEngine(nil, nil, nil, nil, nil, nil, nil)
	require.NoError(t, err)
	for _, test := range []struct {
		name     string
		amount   any
		scale    any
		expected string
	}{
		{"above JS integer range", json.Number("9007199254740993"), json.Number("2"), "90 071 992 547 409,93"},
		{"wire string", "9007199254740993", 2, "90 071 992 547 409,93"},
		{"maximum int64", int64(math.MaxInt64), 2, "92 233 720 368 547 758,07"},
		{"minimum int64", int64(math.MinInt64), 9, "-9 223 372 036,854775808"},
		{"minor units type", types.MinorUnits(-1), 2, "-0,01"},
		{"zero scale", "9223372036854775807", 0, "9 223 372 036 854 775 807"},
		{"maximum scale", "1", 18, "0,000000000000000001"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := engine.RenderTemplate("{{ money .amount .scale }}", map[string]any{"amount": test.amount, "scale": test.scale})
			require.NoError(t, err)
			require.Equal(t, test.expected, got)
		})
	}
}

type deliveryFailureRules struct{ automations.RuleRepository }

func (deliveryFailureRules) IncrementStats(context.Context, id.ID, bool) error { return nil }

type failingAutomationAdapter struct{ err error }

func (a failingAutomationAdapter) Deliver(context.Context, map[string]any, map[string]any, []byte, string, []Attachment) error {
	return a.err
}

func TestScheduledDeliveryFailurePropagatesToDurableExecutor(t *testing.T) {
	failure := errors.New("temporary delivery failure")
	engine, err := NewEngine(deliveryFailureRules{}, nil, nil, nil, nil, map[string]Adapter{"internal_notification": failingAutomationAdapter{failure}}, nil)
	require.NoError(t, err)
	rule := automations.Rule{ID: id.New(), ReactionType: automations.ReactionNotify, ActionTemplate: "test", Subscribers: []automations.Subscriber{{SubscriberType: automations.SubDocField}}}
	require.ErrorIs(t, engine.HandleScheduledRule(t.Context(), rule, nil), failure)
}

func TestMoneyTemplateRejectsAmbiguousInput(t *testing.T) {
	engine, err := NewEngine(nil, nil, nil, nil, nil, nil, nil)
	require.NoError(t, err)
	for _, amount := range []any{"+1", "01", "-0", float64(9007199254740993), 1.5, math.Inf(1), math.NaN(), "9223372036854775808", json.Number("1.5"), nil} {
		_, err := engine.RenderTemplate("{{ money .amount 2 }}", map[string]any{"amount": amount})
		require.Error(t, err)
	}
	for _, scale := range []any{-1, 19, 2.5, "invalid", nil} {
		_, err := engine.RenderTemplate("{{ money 100 .scale }}", map[string]any{"scale": scale})
		require.Error(t, err)
	}
	got, err := engine.RenderTemplate("{{ number .count }}", map[string]any{"count": json.Number("9007199254740993")})
	require.NoError(t, err)
	require.Equal(t, "9 007 199 254 740 993", got)
	_, err = engine.RenderTemplate("{{ currency 100 }}", nil)
	require.Error(t, err, "removed legacy formatter must not remain available")
}

type unsupportedRuleRepository struct {
	automations.RuleRepository
	reaction automations.ReactionType
}

func (r unsupportedRuleRepository) ListActiveByEvent(context.Context, string, string) ([]automations.Rule, error) {
	return []automations.Rule{{ReactionType: r.reaction}}, nil
}

func TestUnsupportedPersistedReactionsDoNotAcknowledgeEvents(t *testing.T) {
	for _, reaction := range []automations.ReactionType{"chain", "create_record", "typo"} {
		engine, err := NewEngine(unsupportedRuleRepository{reaction: reaction}, nil, nil, nil, nil, nil, nil)
		require.NoError(t, err)
		require.Error(t, engine.HandleEvent(context.Background(), "posted", map[string]any{}))
		require.Error(t, engine.HandleScheduledRule(context.Background(), automations.Rule{ReactionType: reaction}, nil))
	}
}
