package cryptodto

import (
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto/rpcconfig"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/http/dto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

type CryptoRPCEndpointFields struct {
	Code                 string  `json:"code"`
	Name                 string  `json:"name" binding:"required"`
	NetworkID            string  `json:"networkId"`
	Role                 string  `json:"role"`
	ProviderID           string  `json:"providerId"`
	TrustGroup           string  `json:"trustGroup"`
	URL                  string  `json:"url"`
	PoolID               *string `json:"poolId"`
	Slot                 string  `json:"credentialSlot"`
	Priority             int     `json:"priority"`
	MinRequestIntervalMS int     `json:"minRequestIntervalMs"`
	DisableKeepAlives    bool    `json:"disableKeepAlives"`
	IsActive             bool    `json:"isActive"`
}
type CreateCryptoRPCEndpointRequest struct{ CryptoRPCEndpointFields }
type UpdateCryptoRPCEndpointRequest struct {
	CryptoRPCEndpointFields
	Version int `json:"version" binding:"required"`
}

func (r CryptoRPCEndpointFields) ApplyTo(e *rpcconfig.RPCEndpoint) {
	e.Code = r.Code
	e.Name = r.Name
	e.NetworkID, _ = id.Parse(r.NetworkID)
	e.Role = rpcconfig.Role(r.Role)
	e.ProviderID = r.ProviderID
	e.TrustGroup = r.TrustGroup
	e.URL = r.URL
	e.PoolID = dto.StringPtrToIDPtr(r.PoolID)
	e.Slot = r.Slot
	e.Priority = r.Priority
	e.MinRequestIntervalMS = r.MinRequestIntervalMS
	e.DisableKeepAlives = r.DisableKeepAlives
	e.IsActive = r.IsActive
}
func (r *CreateCryptoRPCEndpointRequest) ToEntity() *rpcconfig.RPCEndpoint {
	e := rpcconfig.NewRPCEndpoint(r.Code, r.Name)
	r.ApplyTo(e)
	return e
}
func (r *UpdateCryptoRPCEndpointRequest) Apply(e *rpcconfig.RPCEndpoint) {
	r.ApplyTo(e)
	e.Version = r.Version
}

type CryptoRPCEndpointResponse struct{ rpcconfig.RPCEndpoint }
