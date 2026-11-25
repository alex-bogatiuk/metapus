package rpcconfig

import (
	"context"
	"net/url"
	"slices"
	"strings"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/domain"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/entity"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/numerator"
)

// RPCEndpointDefinition identifies the RPC endpoint catalog.
var RPCEndpointDefinition = entity.Definition{
	Key:   "crypto_rpc_endpoint",
	Name:  "CryptoRPCEndpoint",
	Route: "crypto-rpc-endpoints",
	Table: "sys_crypto_rpc_endpoints",
	Kind:  entity.CatalogKind,
}

func (RPCEndpoint) EntityDefinition() entity.Definition { return RPCEndpointDefinition }

// Role is what an endpoint serves. The network family decides which roles
// exist; a provider profile may narrow them further.
type Role string

const (
	RoleRPC      Role = "rpc"
	RoleIndexer  Role = "indexer"
	RoleFullNode Role = "fullnode"
	RoleSolidity Role = "solidity"
)

// Roles lists every role in the order forms offer them.
func Roles() []Role { return []Role{RoleRPC, RoleFullNode, RoleSolidity, RoleIndexer} }

// Valid reports whether the role is one of Roles.
func (r Role) Valid() bool { return slices.Contains(Roles(), r) }

// RolesForFamily lists the roles a network family supports. It is empty for a
// family without an RPC runtime, whose endpoints are not narrowed.
func RolesForFamily(family string) []Role {
	switch family {
	case "evm":
		return []Role{RoleRPC}
	case "tron":
		return []Role{RoleFullNode, RoleSolidity, RoleIndexer}
	}
	return nil
}

// RPCEndpoint keeps chain-specific bindings separate from reusable accounts.
//
// ProviderID identifies the endpoint as a source in block and fee evidence. The
// server assigns it once from the endpoint code and never changes it, so stored
// evidence keeps naming the same source. TrustGroup is the operator behind the
// RPC: known hosts determine it, and only another host needs it stated.
type RPCEndpoint struct {
	entity.Catalog
	NetworkID            id.ID  `db:"network_id" json:"networkId" meta:"label:Сеть,ref:blockchain_network"`
	Role                 Role   `db:"role" json:"role" meta:"label:Назначение подключения"`
	ProviderID           string `db:"provider_id" json:"providerId" meta:"label:Код источника,readOnly:true"`
	TrustGroup           string `db:"trust_group" json:"trustGroup" meta:"label:Кто обслуживает RPC"`
	URL                  string `db:"url" json:"url" meta:"label:Адрес RPC"`
	PoolID               *id.ID `db:"pool_id" json:"poolId,omitempty" meta:"label:Пул аккаунтов,ref:integration_pool"`
	Slot                 string `db:"credential_slot" json:"credentialSlot" meta:"label:Имя ключа"`
	Priority             int    `db:"priority" json:"priority" meta:"label:Приоритет"`
	MinRequestIntervalMS int    `db:"min_request_interval_ms" json:"minRequestIntervalMs" meta:"label:Минимальный интервал (мс)"`
	DisableKeepAlives    bool   `db:"disable_keep_alives" json:"disableKeepAlives" meta:"label:Отключить keep-alive"`
	IsActive             bool   `db:"is_active" json:"isActive" meta:"label:Активен"`
}

func (RPCEndpoint) TableName() string { return "sys_crypto_rpc_endpoints" }
func NewRPCEndpoint(code, name string) *RPCEndpoint {
	return &RPCEndpoint{Catalog: entity.NewCatalog(code, name), Role: RoleRPC, Slot: "default", IsActive: true}
}
func (e *RPCEndpoint) Validate(ctx context.Context) error {
	if err := e.Catalog.Validate(ctx); err != nil {
		return err
	}
	if id.IsNil(e.NetworkID) || e.IsFolder || e.ParentID != nil {
		return apperror.NewValidation("Укажите сеть").WithDetail("field", "networkId")
	}
	if !e.Role.Valid() {
		return apperror.NewValidation("Выберите назначение подключения").WithDetail("field", "role")
	}
	u, err := url.Parse(e.URL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(e.URL, "\r\n\t ") {
		return apperror.NewValidation("Укажите HTTPS-адрес RPC без ключа, query и userinfo")
	}
	if e.Priority < 0 || e.MinRequestIntervalMS < 0 || e.MinRequestIntervalMS > 30000 || e.MinRequestIntervalMS > 0 && e.MinRequestIntervalMS < 100 {
		return apperror.NewValidation("Некорректные приоритет или интервал запросов")
	}
	if len(e.ProviderID) > 64 || len(e.TrustGroup) > 64 || len(e.Slot) > 64 || len(e.URL) > 2048 {
		return apperror.NewValidation("Слишком длинные реквизиты RPC")
	}
	if ResolveRPCTrustGroup(e.URL, e.TrustGroup) == "" {
		return apperror.NewValidation("Укажите, кто обслуживает этот RPC: адрес не принадлежит известному провайдеру").WithDetail("field", "trustGroup")
	}
	return nil
}

// AssignSource fills what the server derives before an endpoint is stored: the
// operator of a known host and, for a new endpoint, its source identifier.
func (e *RPCEndpoint) AssignSource() {
	e.TrustGroup = ResolveRPCTrustGroup(e.URL, e.TrustGroup)
	if e.ProviderID == "" {
		e.ProviderID = e.Code
	}
}
func NewRPCEndpointService(repo domain.CatalogRepository[*RPCEndpoint], num numerator.Generator) *domain.CatalogService[*RPCEndpoint] {
	return domain.NewCatalogService(domain.CatalogServiceConfig[*RPCEndpoint]{Repo: repo, Numerator: num, Definition: &RPCEndpointDefinition})
}
