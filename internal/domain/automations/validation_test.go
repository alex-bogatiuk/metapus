package automations

import (
	"context"
	"testing"

	"metapus/internal/core/id"
)

// ── Account Validation ─────────────────────────────────────────────────────

func TestCreateAccountRequest_Validate_OK(t *testing.T) {
	req := &CreateAccountRequest{
		Name:        "Main Telegram Bot",
		AccountType: AccountTelegram,
		IsActive:    true,
		Credentials: "123456:TOKEN",
	}
	if err := req.Validate(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Config should be auto-initialized
	if req.Config == nil {
		t.Fatal("Config should be auto-initialized to empty map")
	}
}

func TestCreateAccountRequest_Validate_Errors(t *testing.T) {
	cases := []struct {
		name string
		req  CreateAccountRequest
	}{
		{"empty name", CreateAccountRequest{AccountType: AccountTelegram}},
		{"empty account type", CreateAccountRequest{Name: "y"}},
		{"invalid account type", CreateAccountRequest{Name: "y", AccountType: "fax"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.req.Validate(context.Background()); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestUpdateAccountRequest_Validate_VersionRequired(t *testing.T) {
	req := &UpdateAccountRequest{Name: "x", Version: 0}
	if err := req.Validate(context.Background()); err == nil {
		t.Fatal("expected validation error for version < 1")
	}
}

func TestCreateAccountRequest_AllAccountTypes(t *testing.T) {
	types := []AccountType{AccountTelegram, AccountEmail, AccountWebhook, AccountRocketChat, AccountSlack}
	for _, at := range types {
		req := &CreateAccountRequest{Name: "n", AccountType: at}
		if err := req.Validate(context.Background()); err != nil {
			t.Fatalf("type %s should be valid: %v", at, err)
		}
	}
}

// ── Channel Validation ─────────────────────────────────────────────────────

func TestCreateChannelRequest_Validate_OK(t *testing.T) {
	accID := id.New()
	req := &CreateChannelRequest{
		Code:        "ch_main",
		Name:        "Main Chat",
		AccountID:   accID,
		Destination: map[string]any{"chat_id": "-100123"},
		IsActive:    true,
	}
	if err := req.Validate(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateChannelRequest_Validate_Errors(t *testing.T) {
	accID := id.New()
	cases := []struct {
		name string
		req  CreateChannelRequest
	}{
		{"empty code", CreateChannelRequest{Name: "x", AccountID: accID}},
		{"empty name", CreateChannelRequest{Code: "x", AccountID: accID}},
		{"nil accountId", CreateChannelRequest{Code: "x", Name: "y"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.req.Validate(context.Background()); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestUpdateChannelRequest_Validate_VersionRequired(t *testing.T) {
	accID := id.New()
	req := &UpdateChannelRequest{Name: "x", AccountID: accID, Version: 0}
	if err := req.Validate(context.Background()); err == nil {
		t.Fatal("expected validation error for version < 1")
	}
}

// ── Rule Validation ─────────────────────────────────────────────────────────

func TestCreateRuleRequest_Validate_OK(t *testing.T) {
	chID := id.New()
	req := &CreateRuleRequest{
		Name:           "Price Alert",
		TriggerType:    TriggerEntityEvent,
		EventType:      "posted",
		ReactionType:   ReactionNotify,
		ActionTemplate: "Doc #{{ .doc.number }}",
		Subscribers: []SubscriberInput{
			{SubscriberType: SubChannel, ChannelID: &chID, DeliveryMethod: "push"},
		},
		IsActive: true,
	}
	if err := req.Validate(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Defaults should be applied
	if req.MessageFormat != "text" {
		t.Fatalf("expected default MessageFormat 'text', got %q", req.MessageFormat)
	}
	if req.MaxRetries != 3 {
		t.Fatalf("expected default MaxRetries 3, got %d", req.MaxRetries)
	}
}

func TestCreateRuleRequest_Validate_Errors(t *testing.T) {
	cases := []struct {
		name string
		req  CreateRuleRequest
	}{
		{"empty name", CreateRuleRequest{EventType: "posted", TriggerType: TriggerEntityEvent, ReactionType: ReactionNotify}},
		{"empty eventType", CreateRuleRequest{Name: "x", TriggerType: TriggerEntityEvent, ReactionType: ReactionNotify}},
		{"invalid triggerType", CreateRuleRequest{Name: "x", EventType: "posted", TriggerType: "bad", ReactionType: ReactionNotify}},
		{"invalid reactionType", CreateRuleRequest{Name: "x", EventType: "posted", TriggerType: TriggerEntityEvent, ReactionType: "bad"}},
		{"unknown entity event action", CreateRuleRequest{Name: "x", EventType: "unknown_action", TriggerType: TriggerEntityEvent, ReactionType: ReactionNotify}},
		{"scheduled without cron prefix", CreateRuleRequest{Name: "x", EventType: "daily", TriggerType: TriggerScheduled, ReactionType: ReactionNotify}},
		{"chain without chain_rule_ids", CreateRuleRequest{Name: "x", EventType: "posted", TriggerType: TriggerEntityEvent, ReactionType: ReactionChain}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.req.Validate(context.Background()); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestCreateRuleRequest_ScheduledWithCron(t *testing.T) {
	req := &CreateRuleRequest{
		Name:         "Hourly Report",
		TriggerType:  TriggerScheduled,
		EventType:    "cron:0 0 * * * *", // 6-field: sec min hr dom mon dow
		ReactionType: ReactionNotify,
		IsActive:     true,
	}
	if err := req.Validate(context.Background()); err != nil {
		t.Fatalf("scheduled rule with cron: prefix should be valid: %v", err)
	}
}

func TestCreateRuleRequest_ChainWithIDs(t *testing.T) {
	chainID := id.New()
	req := &CreateRuleRequest{
		Name:         "Chain Rule",
		TriggerType:  TriggerEntityEvent,
		EventType:    "posted",
		ReactionType: ReactionChain,
		ChainRuleIDs: []id.ID{chainID},
		IsActive:     true,
	}
	if err := req.Validate(context.Background()); err != nil {
		t.Fatalf("chain rule with IDs should be valid: %v", err)
	}
}

func TestUpdateRuleRequest_Validate_VersionRequired(t *testing.T) {
	req := &UpdateRuleRequest{
		Name:         "x",
		EventType:    "posted",
		TriggerType:  TriggerEntityEvent,
		ReactionType: ReactionNotify,
		Version:      0,
	}
	if err := req.Validate(context.Background()); err == nil {
		t.Fatal("expected validation error for version < 1")
	}
}

// ── Subscriber Validation ───────────────────────────────────────────────────

func TestSubscriberInput_Validate_Channel(t *testing.T) {
	chID := id.New()
	s := &SubscriberInput{SubscriberType: SubChannel, ChannelID: &chID, DeliveryMethod: "push"}
	if err := s.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSubscriberInput_Validate_ChannelMissing(t *testing.T) {
	s := &SubscriberInput{SubscriberType: SubChannel, DeliveryMethod: "push"}
	if err := s.Validate(); err == nil {
		t.Fatal("expected validation error for missing channelId")
	}
}

func TestSubscriberInput_Validate_User(t *testing.T) {
	uID := id.New()
	s := &SubscriberInput{SubscriberType: SubUser, UserID: &uID, DeliveryMethod: "email"}
	if err := s.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSubscriberInput_Validate_UserMissing(t *testing.T) {
	s := &SubscriberInput{SubscriberType: SubUser, DeliveryMethod: "email"}
	if err := s.Validate(); err == nil {
		t.Fatal("expected validation error for missing userId")
	}
}

func TestSubscriberInput_Validate_Role(t *testing.T) {
	role := "Accountant"
	s := &SubscriberInput{SubscriberType: SubRole, RoleName: &role, DeliveryMethod: "ui_notification"}
	if err := s.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSubscriberInput_Validate_RoleMissing(t *testing.T) {
	s := &SubscriberInput{SubscriberType: SubRole, DeliveryMethod: "ui_notification"}
	if err := s.Validate(); err == nil {
		t.Fatal("expected validation error for missing roleName")
	}
}

func TestSubscriberInput_Validate_DocField(t *testing.T) {
	field := "responsibleUserId"
	s := &SubscriberInput{SubscriberType: SubDocField, DocFieldPath: &field, DeliveryMethod: "email"}
	if err := s.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSubscriberInput_Validate_DocFieldMissing(t *testing.T) {
	s := &SubscriberInput{SubscriberType: SubDocField, DeliveryMethod: "email"}
	if err := s.Validate(); err == nil {
		t.Fatal("expected validation error for missing docFieldPath")
	}
}

func TestSubscriberInput_Validate_InvalidType(t *testing.T) {
	s := &SubscriberInput{SubscriberType: "telegram_group", DeliveryMethod: "push"}
	if err := s.Validate(); err == nil {
		t.Fatal("expected validation error for invalid subscriber type")
	}
}
