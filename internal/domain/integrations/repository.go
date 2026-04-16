package integrations

import (
	"context"

	"metapus/internal/core/id"
)

// Repository provides data access for service accounts.
type Repository interface {
	// List returns all service accounts.
	List(ctx context.Context) ([]ServiceAccount, error)

	// GetByID retrieves a service account by ID.
	GetByID(ctx context.Context, accountID id.ID) (*ServiceAccount, error)

	// Create creates a new service account.
	Create(ctx context.Context, req CreateRequest) (*ServiceAccount, error)

	// Update modifies an existing service account (excluding credentials).
	Update(ctx context.Context, accountID id.ID, req UpdateRequest) (*ServiceAccount, error)

	// Delete removes a service account.
	Delete(ctx context.Context, accountID id.ID) error

	// UpdateStatus updates the operational status and logging fields.
	UpdateStatus(ctx context.Context, accountID id.ID, status AccountStatus, lastError *string, success bool) error
}

// CredentialManager handles writing and reading encrypted credentials.
// It is separated from the main Repository to emphasize that credentials
// are managed through a distinct lifecycle and security context.
type CredentialManager interface {
	// WriteCredentials sets or updates encrypted credentials for an account.
	WriteCredentials(ctx context.Context, accountID id.ID, credentials []byte) error

	// ReadCredentials retrieves and decrypts credentials for an account.
	// This should ONLY be used by the Automation Engine worker.
	ReadCredentials(ctx context.Context, accountID id.ID) ([]byte, error)
}
