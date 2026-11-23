package integrations

import (
	"context"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/core/id"
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

// AccountKeys lists key names, never values, of one pool account.
type AccountKeys struct {
	ID             string
	Name           string
	HasCredentials bool
	Keys           []string
}

type AdministrationRepository interface {
	Status(context.Context, id.ID) (PoolStatus, error)
	Command(context.Context, id.ID, string, int64) error
}
