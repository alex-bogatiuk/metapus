package integrations

import (
	"context"
	"slices"
	"strings"
	"unicode/utf8"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
)

const MaxRPCConfigurationEndpoints = 256

// RPCConfiguration is the secret-free administrative view of a network's RPCs.
type RPCConfiguration struct {
	Family            string                     `json:"family"`
	Settings          RPCNetworkSettings         `json:"settings"`
	DefaultSettings   RPCNetworkSettings         `json:"defaultSettings"`
	EffectiveSettings RPCNetworkSettings         `json:"effectiveSettings"`
	NetworkID         id.ID                      `json:"networkId"`
	Source            string                     `json:"source"`
	Revision          string                     `json:"revision"`
	EffectiveRevision string                     `json:"effectiveRevision"`
	Endpoints         []RPCConfigurationEndpoint `json:"endpoints"`
	Requirements      []RPCRoleRequirement       `json:"requirements"`
}

type RPCConfigurationEndpoint struct {
	ID                   id.ID  `json:"id"`
	Version              int    `json:"version"`
	Name                 string `json:"name"`
	Role                 string `json:"role"`
	ProviderID           string `json:"providerId"`
	TrustGroup           string `json:"trustGroup"`
	IsActive             bool   `json:"isActive"`
	Priority             int    `json:"priority"`
	MinRequestIntervalMS int    `json:"minRequestIntervalMs"`
}

type RPCRoleRequirement struct {
	Role                string `json:"role"`
	RequiredProviders   int    `json:"requiredProviders"`
	ConfiguredProviders int    `json:"configuredProviders"`
}

type RPCConfigurationChange struct {
	ID                   id.ID `json:"id"`
	Version              int   `json:"version"`
	IsActive             bool  `json:"isActive"`
	Priority             int   `json:"priority"`
	MinRequestIntervalMS *int  `json:"minRequestIntervalMs,omitempty"`
}

type RPCConfigurationUpdate struct {
	ExpectedRevision string                   `json:"expectedRevision"`
	Endpoints        []RPCConfigurationChange `json:"endpoints"`
	Comment          string                   `json:"comment"`
	Settings         *RPCNetworkSettings      `json:"settings,omitempty"`
}

func (u RPCConfigurationUpdate) Validate() error {
	if len(u.ExpectedRevision) != 64 || len(u.Endpoints) > MaxRPCConfigurationEndpoints || utf8.RuneCountInString(u.Comment) > 500 {
		return apperror.NewValidation("Некорректные параметры изменения RPC")
	}
	seen := make(map[id.ID]bool, len(u.Endpoints))
	for _, e := range u.Endpoints {
		if id.IsNil(e.ID) || seen[e.ID] || e.Version < 1 || e.Priority < 0 || e.Priority > 1_000_000 {
			return apperror.NewValidation("Проверьте подключения, версии и порядок RPC")
		}
		if e.MinRequestIntervalMS != nil && (*e.MinRequestIntervalMS < 0 || *e.MinRequestIntervalMS > 30000 || *e.MinRequestIntervalMS > 0 && *e.MinRequestIntervalMS < 100) {
			return apperror.NewValidation("Интервал RPC должен быть 0 (автоматически) или от 100 до 30000 мс")
		}
		seen[e.ID] = true
	}
	return nil
}

func (c RPCConfiguration) ValidateCoverage() error {
	for _, role := range c.Requirements {
		if role.ConfiguredProviders < role.RequiredProviders {
			return apperror.NewValidation("Недостаточно независимых RPC для обязательной роли").WithDetail("role", role.Role).WithDetail("reasonCode", "rpc_coverage_insufficient")
		}
	}
	return nil
}

// ValidateRPCCoverageChange permits incremental setup and repair through the
// catalog, but never lets a mutation reduce an already insufficient coverage.
func ValidateRPCCoverageChange(before, after RPCConfiguration) error {
	if after.Source != "database" {
		return nil
	}
	if before.Source != "database" {
		return after.ValidateCoverage()
	}
	previous := make(map[string]int, len(before.Requirements))
	for _, r := range before.Requirements {
		previous[r.Role] = r.ConfiguredProviders
	}
	for _, r := range after.Requirements {
		if _, exists := previous[r.Role]; !exists {
			return after.ValidateCoverage()
		}
		if r.ConfiguredProviders < r.RequiredProviders && r.ConfiguredProviders < previous[r.Role] {
			return after.ValidateCoverage()
		}
	}
	return nil
}

func RPCConfigurationChanged(before, after RPCConfiguration) bool {
	return before.Revision != after.Revision
}

func RPCConfigurationComment(comment string) string { return strings.TrimSpace(comment) }

type RPCConfigurationRepository interface {
	Get(context.Context, id.ID) (RPCConfiguration, error)
	// Lock and Save use the caller's transaction. Every topology writer shares
	// this tenant-local lock; Save checks the complete expected revision.
	Lock(context.Context) error
	Save(context.Context, id.ID, string, []RPCConfigurationChange, *RPCNetworkSettings) (RPCConfiguration, error)
}

// Plan builds and validates the final configuration without mutating the input
// snapshot or accessing storage. Only effective changes are returned for writing.
func (c RPCConfiguration) Plan(update RPCConfigurationUpdate) ([]RPCConfigurationChange, error) {
	if err := update.Validate(); err != nil {
		return nil, err
	}
	if c.Source != "database" {
		return nil, apperror.NewValidation("RPC этой сети настроены через файл").WithDetail("reasonCode", "rpc_configuration_file")
	}
	if c.Revision != update.ExpectedRevision {
		return nil, apperror.NewConcurrentModification("crypto_rpc_configuration", c.NetworkID.String())
	}
	if update.Settings != nil {
		if err := update.Settings.Validate(c.Family); err != nil {
			return nil, err
		}
		// Ingestion activation/mode is controlled by the cutover procedure.
		if c.Family == "tron" && update.Settings.Defaults(c.Family).TRON.IngestionMode != c.Settings.Defaults(c.Family).TRON.IngestionMode {
			return nil, apperror.NewValidation("Режим обработки блоков изменяется через процедуру переключения сети")
		}
	}
	endpoints := slices.Clone(c.Endpoints)
	byID := make(map[id.ID]int, len(endpoints))
	original := make(map[id.ID]RPCConfigurationEndpoint, len(endpoints))
	for i, e := range endpoints {
		byID[e.ID], original[e.ID] = i, e
	}
	changedRoles := make(map[string]bool)
	for _, change := range update.Endpoints {
		i, ok := byID[change.ID]
		if !ok || endpoints[i].Version != change.Version {
			return nil, apperror.NewConcurrentModification("crypto_rpc_configuration", c.NetworkID.String())
		}
		if endpoints[i].Priority != change.Priority {
			changedRoles[endpoints[i].Role] = true
		}
		endpoints[i].IsActive, endpoints[i].Priority = change.IsActive, change.Priority
		if change.MinRequestIntervalMS != nil {
			endpoints[i].MinRequestIntervalMS = *change.MinRequestIntervalMS
		}
	}
	slices.SortStableFunc(endpoints, func(a, b RPCConfigurationEndpoint) int {
		if a.Priority < b.Priority {
			return -1
		}
		if a.Priority > b.Priority {
			return 1
		}
		return strings.Compare(a.ID.String(), b.ID.String())
	})
	positions := make(map[string]int)
	changes := make([]RPCConfigurationChange, 0)
	for i := range endpoints {
		e := &endpoints[i]
		if changedRoles[e.Role] {
			positions[e.Role]++
			e.Priority = positions[e.Role] * 10
		}
		old := original[e.ID]
		if old.Priority != e.Priority || old.IsActive != e.IsActive || old.MinRequestIntervalMS != e.MinRequestIntervalMS {
			change := RPCConfigurationChange{ID: e.ID, Version: e.Version, IsActive: e.IsActive, Priority: e.Priority}
			if old.MinRequestIntervalMS != e.MinRequestIntervalMS {
				interval := e.MinRequestIntervalMS
				change.MinRequestIntervalMS = &interval
			}
			changes = append(changes, change)
		}
	}
	if len(changes) == 0 {
		return changes, nil
	}
	candidate := c
	candidate.Requirements = slices.Clone(c.Requirements)
	for i := range candidate.Requirements {
		r := &candidate.Requirements[i]
		groups := make(map[string]bool)
		for _, e := range endpoints {
			if e.IsActive && e.Role == r.Role {
				groups[e.TrustGroup] = true
			}
		}
		r.ConfiguredProviders = len(groups)
	}
	if err := candidate.ValidateCoverage(); err != nil {
		return nil, err
	}
	return changes, nil
}
