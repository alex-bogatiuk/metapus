package automations

import (
	"context"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
)

// Channel represents a delivery destination that references an Account for credentials.
// One Account (Bot Token) → many Channels (different Chat IDs / email addresses / URLs).
type Channel struct {
	ID           id.ID          `json:"id"`
	Code         string         `json:"code"`
	Name         string         `json:"name"`
	AccountID    id.ID          `json:"accountId"`
	Destination  map[string]any `json:"destination"`
	IsActive     bool           `json:"isActive"`
	DeletionMark bool           `json:"deletionMark"`
	Version      int            `json:"version"`
	CreatedAt    time.Time      `json:"createdAt"`
	UpdatedAt    time.Time      `json:"updatedAt"`

	// Denormalized from Account (populated by service/handler layer)
	AccountName string      `json:"accountName,omitempty"`
	AccountType AccountType `json:"accountType,omitempty"`

	// Stats
	RuleCount int `json:"ruleCount"` // How many rules reference this channel via subscribers
}

// CreateChannelRequest encapsulates data for creating a new channel.
type CreateChannelRequest struct {
	Code        string         `json:"code"`
	Name        string         `json:"name"`
	AccountID   id.ID          `json:"accountId"`
	Destination map[string]any `json:"destination"`
	IsActive    bool           `json:"isActive"`
}

// Validate checks if the CreateChannelRequest is valid.
func (r *CreateChannelRequest) Validate(_ context.Context) error {
	if r.Code == "" {
		return apperror.NewValidation("code is required").WithDetail("field", "code")
	}
	if r.Name == "" {
		return apperror.NewValidation("name is required").WithDetail("field", "name")
	}
	if id.IsNil(r.AccountID) {
		return apperror.NewValidation("account is required").WithDetail("field", "accountId")
	}
	if r.Destination == nil {
		r.Destination = make(map[string]any)
	}
	return nil
}

// UpdateChannelRequest encapsulates data for updating an existing channel.
type UpdateChannelRequest struct {
	Name        string         `json:"name"`
	AccountID   id.ID          `json:"accountId"`
	Destination map[string]any `json:"destination"`
	IsActive    bool           `json:"isActive"`
	Version     int            `json:"version"` // Optimistic Locking
}

// Validate checks if the UpdateChannelRequest is valid.
func (r *UpdateChannelRequest) Validate(_ context.Context) error {
	if r.Name == "" {
		return apperror.NewValidation("name is required").WithDetail("field", "name")
	}
	if id.IsNil(r.AccountID) {
		return apperror.NewValidation("account is required").WithDetail("field", "accountId")
	}
	if r.Destination == nil {
		r.Destination = make(map[string]any)
	}
	if r.Version < 1 {
		return apperror.NewValidation("version is required for optimistic locking").WithDetail("field", "version")
	}
	return nil
}
