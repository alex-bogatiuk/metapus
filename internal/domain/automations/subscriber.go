package automations

import (
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
)

// SubscriberType defines how a subscriber receives notifications.
type SubscriberType string

const (
	SubChannel  SubscriberType = "channel"   // External channel (Telegram chat, email, webhook)
	SubUser     SubscriberType = "user"      // Specific user (UI notification or email)
	SubRole     SubscriberType = "role"      // All users with this role
	SubDocField SubscriberType = "doc_field" // User ID extracted from document field
)

// Subscriber represents a polymorphic binding between a Rule and a delivery target.
// Acts as a "table part" (табличная часть) of the Rule.
type Subscriber struct {
	ID             id.ID          `json:"id"`
	RuleID         id.ID          `json:"ruleId"`
	SubscriberType SubscriberType `json:"subscriberType"`
	ChannelID      *id.ID         `json:"channelId,omitempty"`
	UserID         *id.ID         `json:"userId,omitempty"`
	RoleName       *string        `json:"roleName,omitempty"`
	DocFieldPath   *string        `json:"docFieldPath,omitempty"`
	DeliveryMethod string         `json:"deliveryMethod"` // ui_notification | email
	Idx            int            `json:"idx"`

	// Denormalized for UI display (populated by service/handler)
	ChannelName *string `json:"channelName,omitempty"`
	UserName    *string `json:"userName,omitempty"`
}

// SubscriberInput is the create/update DTO for subscribers (sent inline with Rule).
type SubscriberInput struct {
	SubscriberType SubscriberType `json:"subscriberType"`
	ChannelID      *id.ID         `json:"channelId,omitempty"`
	UserID         *id.ID         `json:"userId,omitempty"`
	RoleName       *string        `json:"roleName,omitempty"`
	DocFieldPath   *string        `json:"docFieldPath,omitempty"`
	DeliveryMethod string         `json:"deliveryMethod"`
	Idx            int            `json:"idx"`
}

// Validate checks if the SubscriberInput is valid.
func (s *SubscriberInput) Validate() error {
	switch s.SubscriberType {
	case SubChannel:
		if s.ChannelID == nil || id.IsNil(*s.ChannelID) {
			return apperror.NewValidation("channelId is required for channel subscriber")
		}
	case SubUser:
		if s.UserID == nil || id.IsNil(*s.UserID) {
			return apperror.NewValidation("userId is required for user subscriber")
		}
	case SubRole:
		if s.RoleName == nil || *s.RoleName == "" {
			return apperror.NewValidation("roleName is required for role subscriber")
		}
	case SubDocField:
		if s.DocFieldPath == nil || *s.DocFieldPath == "" {
			return apperror.NewValidation("docFieldPath is required for doc_field subscriber")
		}
	default:
		return apperror.NewValidation("invalid subscriber type").WithDetail("subscriberType", string(s.SubscriberType))
	}
	return nil
}
