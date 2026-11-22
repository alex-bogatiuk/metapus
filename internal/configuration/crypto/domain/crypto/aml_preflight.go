package crypto

import (
	"context"
	"encoding/hex"
	"strings"
	"time"

	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/types"
)

type EmptyCheckpointSource struct {
	ProviderID string          `json:"providerId"`
	TrustGroup string          `json:"trustGroup"`
	Height     int64           `json:"height"`
	Hash       string          `json:"hash"`
	Balance    types.RawAmount `json:"balance"`
	Method     string          `json:"method"`
}

// EmptyCheckpoint contains two pinned state observations at one financially
// final checkpoint. A nonzero consensus is evidence too; it never seeds money.
type EmptyCheckpoint struct {
	NetworkID  id.ID                   `json:"networkId"`
	Asset      AssetRef                `json:"asset"`
	Address    string                  `json:"address"`
	Height     int64                   `json:"height"`
	Hash       string                  `json:"hash"`
	ObservedAt time.Time               `json:"observedAt"`
	Sources    []EmptyCheckpointSource `json:"sources"`
}

func (p EmptyCheckpoint) Validate(now time.Time) error {
	hash := strings.TrimPrefix(strings.ToLower(p.Hash), "0x")
	decoded, err := hex.DecodeString(hash)
	if err != nil || len(decoded) != 32 || id.IsNil(p.NetworkID) || id.IsNil(p.Asset.TokenID) || p.Address == "" || p.Height < 0 || len(p.Sources) != 2 || p.ObservedAt.IsZero() || p.ObservedAt.After(now.Add(5*time.Second)) || now.Sub(p.ObservedAt) > time.Minute {
		return apperror.NewValidation("preflight requires fresh exact network/asset/address and a final quorum checkpoint")
	}
	providers, groups := map[string]bool{}, map[string]bool{}
	for _, source := range p.Sources {
		provider, group := strings.ToLower(strings.TrimSpace(source.ProviderID)), strings.ToLower(strings.TrimSpace(source.TrustGroup))
		if provider == "" || group == "" || providers[provider] || groups[group] || source.Height != p.Height || !strings.EqualFold(source.Hash, p.Hash) || source.Balance.Cmp(p.Sources[0].Balance) != 0 ||
			(source.Method != "solidified_bracket" && source.Method != "finalized_block_bracket") {
			return apperror.NewValidation("preflight sources must independently agree on checkpoint and balance")
		}
		providers[provider], groups[group] = true, true
	}
	return nil
}

func (p EmptyCheckpoint) Empty() bool {
	return len(p.Sources) == 2 && p.Sources[0].Balance.IsZero() && p.Sources[1].Balance.IsZero()
}

type EmptyCheckpointReader interface {
	EmptyCheckpoint(context.Context, AssetRef, string) (EmptyCheckpoint, error)
}

type AddressWatchCoverage struct {
	NetworkID    id.ID
	TokenID      id.ID
	WatcherKey   string
	StartHeight  int64
	LeaseOwner   string
	FencingToken int64
}

// AddressCoverageReporter is read after ReplaceMonitoredAddresses. The barrier
// excludes any window fetched using the previous address snapshot.
type AddressCoverageReporter interface {
	AddressCoverage() (AddressWatchCoverage, bool)
}

type AMLPreflightJob struct {
	ID, WalletID, MerchantID, NetworkID, TokenID, PolicyID id.ID
	LeaseToken                                             id.ID
	ScopeVersion, PolicyVersion                            int64
	Address                                                string
	Asset                                                  AssetRef
}

type AMLPreflightRepository interface {
	ClaimAMLPreflight(context.Context, string, time.Duration) (AMLPreflightJob, bool, error)
	SaveAMLPreflight(context.Context, AMLPreflightJob, EmptyCheckpoint) error
	RetryAMLPreflight(context.Context, AMLPreflightJob, string) error
}
