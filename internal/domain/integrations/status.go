package integrations

import (
	"context"
	"time"

	"metapus/internal/core/id"
)

type AccountStatus struct {
	ID                   string
	Name                 string
	IsActive             bool
	HasCredentials       bool
	QuotaPolicy          QuotaPolicy
	CredentialsExpiresAt *time.Time
	ManualPeriodVersion  int64
	Availability         string
	CredentialsVersion   int
	ExhaustedUntil       *time.Time
	ThrottledUntil       *time.Time
	LastError            string
	NextResetAt          *time.Time
	InvalidSlots         []string
}
type PoolStatus struct {
	PoolID           string
	AccountID        *string
	SelectionVersion int64
	Reason           string
	Accounts         []AccountStatus
}

// RPCKeyAccount lists key names, never values, of one pool account.
type RPCKeyAccount struct {
	ID             string
	Name           string
	HasCredentials bool
	Keys           []string
}

// RPCKeyOptions tells an endpoint form which account key it may read.
// Required is set when the provider profile fixes the name; otherwise Names
// lists the names every pool account with credentials has.
type RPCKeyOptions struct {
	Required string
	Names    []string
	Accounts []RPCKeyAccount
}

type RPCKeyQuery struct {
	PoolID    id.ID
	NetworkID id.ID
	Role      string
	URL       string
}

type AdministrationRepository interface {
	Status(context.Context, id.ID) (PoolStatus, error)
	Command(context.Context, id.ID, string, int64) error
	RPCKeyOptions(context.Context, RPCKeyQuery) (RPCKeyOptions, error)
}
