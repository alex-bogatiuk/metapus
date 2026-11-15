package cryptodto

import (
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/integrations"
)

type CryptoRPCConfigurationResponse struct {
	integrations.RPCConfiguration
	CanManage    bool   `json:"canManage"`
	LockedReason string `json:"lockedReason,omitempty"`
}

type CryptoRPCConfigurationChange struct {
	ID                   string `json:"id" binding:"required,uuid"`
	Version              int    `json:"version" binding:"required,min=1"`
	IsActive             *bool  `json:"isActive" binding:"required"`
	Priority             *int   `json:"priority" binding:"required,min=0,max=1000000"`
	MinRequestIntervalMS *int   `json:"minRequestIntervalMs,omitempty" binding:"omitempty,min=0,max=30000"`
}

type UpdateCryptoRPCConfigurationRequest struct {
	ExpectedRevision string                           `json:"expectedRevision" binding:"required,len=64"`
	Endpoints        []CryptoRPCConfigurationChange   `json:"endpoints" binding:"max=256,dive"`
	Comment          string                           `json:"comment" binding:"max=500"`
	Settings         *integrations.RPCNetworkSettings `json:"settings,omitempty"`
}

func (r UpdateCryptoRPCConfigurationRequest) ToDomain() (integrations.RPCConfigurationUpdate, error) {
	out := integrations.RPCConfigurationUpdate{ExpectedRevision: r.ExpectedRevision, Comment: r.Comment, Settings: r.Settings, Endpoints: make([]integrations.RPCConfigurationChange, 0, len(r.Endpoints))}
	for _, e := range r.Endpoints {
		uid, err := id.Parse(e.ID)
		if err != nil || e.IsActive == nil || e.Priority == nil {
			return out, apperror.NewValidation("Проверьте параметры RPC-подключения")
		}
		out.Endpoints = append(out.Endpoints, integrations.RPCConfigurationChange{ID: uid, Version: e.Version, IsActive: *e.IsActive, Priority: *e.Priority, MinRequestIntervalMS: e.MinRequestIntervalMS})
	}
	return out, out.Validate()
}
