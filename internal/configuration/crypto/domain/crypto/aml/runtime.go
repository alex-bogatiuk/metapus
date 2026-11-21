package aml

import "metapus/internal/core/id"

// RuntimeConfig contains one immutable accepted mapping and credentials for
// the exact account pinned by a job. It never participates in HTTP responses.
type RuntimeConfig struct {
	Provider          string
	NetworkFamily     string
	NetworkIdentifier string
	Emulator          *EmulatorConfig
	AccountID         id.ID
	BearerToken       string `json:"-"`
	MappingVersion    string
	ContractVersion   string
	NetworkID         id.ID
	TokenID           id.ID
	ContractAddress   string
	Currency          string
	ProviderTokenID   string
	Native            bool
	PendingStatuses   []string
	FailureStatuses   []string
}
