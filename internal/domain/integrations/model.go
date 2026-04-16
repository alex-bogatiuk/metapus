package integrations

import (
	"context"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
)

// AccountType defines the supported types of service accounts.
type AccountType string

const (
	AccountTypeTelegram   AccountType = "telegram"
	AccountTypeEmail      AccountType = "email"
	AccountTypeWebhook    AccountType = "webhook"
	AccountTypeRocketChat AccountType = "rocketchat"
	AccountTypeSlack      AccountType = "slack"
)

// AccountStatus defines the operational status of a service account.
type AccountStatus string

const (
	AccountStatusActive   AccountStatus = "active"
	AccountStatusError    AccountStatus = "error"
	AccountStatusDisabled AccountStatus = "disabled"
)

// ServiceAccount represents a configured external integration.
// Note: Credentials are intentionally omitted from this model. They are managed
// separately via CredentialWriter and are never exposed through the domain model or API.
type ServiceAccount struct {
	ID             id.ID                  `json:"id"`
	Name           string                 `json:"name"`
	AccountType    AccountType            `json:"accountType"`
	Config         map[string]interface{} `json:"config"`
	OrganizationID *id.ID                 `json:"organizationId,omitempty"`
	Status         AccountStatus          `json:"status"`
	IsDefault      bool                   `json:"isDefault"`
	LastError      *string                `json:"lastError,omitempty"`
	LastSuccessAt  *time.Time             `json:"lastSuccessAt,omitempty"`
	CreatedAt      time.Time              `json:"createdAt"`
	UpdatedAt      time.Time              `json:"updatedAt"`
}

// CreateRequest encapsulates the data needed to create a new service account.
type CreateRequest struct {
	Name           string                 `json:"name"`
	AccountType    AccountType            `json:"accountType"`
	Config         map[string]interface{} `json:"config"`
	OrganizationID *id.ID                 `json:"organizationId,omitempty"`
	IsDefault      bool                   `json:"isDefault"`
	Credentials    []byte                 `json:"credentials,omitempty"` // Only used during creation
}

// UpdateRequest encapsulates the data needed to update an existing service account.
type UpdateRequest struct {
	Name           string                 `json:"name"`
	Config         map[string]interface{} `json:"config"`
	OrganizationID *id.ID                 `json:"organizationId,omitempty"`
	Status         AccountStatus          `json:"status"`
	IsDefault      bool                   `json:"isDefault"`
}

// UpdateCredentialsRequest is used for the dedicated credentials update endpoint.
type UpdateCredentialsRequest struct {
	Credentials []byte `json:"credentials"`
}

// Validate checks if the CreateRequest is valid.
func (r *CreateRequest) Validate(ctx context.Context) error {
	if r.Name == "" {
		return apperror.NewValidation("name is required").WithDetail("field", "name")
	}
	if string(r.AccountType) == "" {
		return apperror.NewValidation("account type is required").WithDetail("field", "accountType")
	}
	if r.Config == nil {
		r.Config = make(map[string]interface{})
	}
	// Note: We don't strictly require credentials on creation as some integrations
	// (like simple webhooks without auth) might not need them.
	return nil
}

// Validate checks if the UpdateRequest is valid.
func (r *UpdateRequest) Validate(ctx context.Context) error {
	if r.Name == "" {
		return apperror.NewValidation("name is required").WithDetail("field", "name")
	}
	if string(r.Status) == "" {
		return apperror.NewValidation("status is required").WithDetail("field", "status")
	}
	if r.Config == nil {
		r.Config = make(map[string]interface{})
	}
	return nil
}
