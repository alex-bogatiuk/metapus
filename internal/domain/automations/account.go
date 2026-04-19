package automations

import (
	"context"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
)

// AccountType defines the supported types of automation accounts.
type AccountType string

const (
	AccountTelegram   AccountType = "telegram"
	AccountEmail      AccountType = "email"
	AccountWebhook    AccountType = "webhook"
	AccountRocketChat AccountType = "rocketchat"
	AccountSlack      AccountType = "slack"
)

// AccountStatus is the operational status of an account.
type AccountStatus string

const (
	AccountStatusActive   AccountStatus = "active"
	AccountStatusError    AccountStatus = "error"
	AccountStatusDisabled AccountStatus = "disabled"
)

// Account represents a centralized sender with encrypted credentials.
// One Account (Bot Token) → many Channels (Chat IDs).
// Credentials are never exposed in this model — managed via CredentialManager.
type Account struct {
	ID             id.ID          `json:"id"`
	Name           string         `json:"name"`
	AccountType    AccountType    `json:"accountType"`
	Config         map[string]any `json:"config"`
	OrganizationID *id.ID         `json:"organizationId,omitempty"`
	IsActive       bool           `json:"isActive"`
	Status         AccountStatus  `json:"status"`
	LastError      *string        `json:"lastError,omitempty"`
	LastSuccessAt  *time.Time     `json:"lastSuccessAt,omitempty"`
	ChannelCount   int            `json:"channelCount"` // Denormalized count
	DeletionMark   bool           `json:"deletionMark"`
	Version        int            `json:"version"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

// CreateAccountRequest encapsulates data for creating a new account.
type CreateAccountRequest struct {
	Name           string         `json:"name"`
	AccountType    AccountType    `json:"accountType"`
	Config         map[string]any `json:"config"`
	OrganizationID *id.ID         `json:"organizationId,omitempty"`
	IsActive       bool           `json:"isActive"`
	Credentials    string         `json:"credentials,omitempty"` // Plaintext, encrypted before storage
}

// Validate checks if the CreateAccountRequest is valid.
func (r *CreateAccountRequest) Validate(_ context.Context) error {
	if r.Name == "" {
		return apperror.NewValidation("name is required").WithDetail("field", "name")
	}
	if r.AccountType == "" {
		return apperror.NewValidation("account type is required").WithDetail("field", "accountType")
	}
	switch r.AccountType {
	case AccountTelegram, AccountEmail, AccountWebhook, AccountRocketChat, AccountSlack:
		// OK
	default:
		return apperror.NewValidation("invalid account type").WithDetail("accountType", string(r.AccountType))
	}
	if r.Config == nil {
		r.Config = make(map[string]any)
	}
	return nil
}

// UpdateAccountRequest encapsulates data for updating an existing account.
type UpdateAccountRequest struct {
	Name           string         `json:"name"`
	Config         map[string]any `json:"config"`
	OrganizationID *id.ID         `json:"organizationId,omitempty"`
	IsActive       bool           `json:"isActive"`
	Version        int            `json:"version"` // Optimistic Locking
}

// Validate checks if the UpdateAccountRequest is valid.
func (r *UpdateAccountRequest) Validate(_ context.Context) error {
	if r.Name == "" {
		return apperror.NewValidation("name is required").WithDetail("field", "name")
	}
	if r.Config == nil {
		r.Config = make(map[string]any)
	}
	if r.Version < 1 {
		return apperror.NewValidation("version is required for optimistic locking").WithDetail("field", "version")
	}
	return nil
}
