package rpcconfig

import (
	"context"
	"net/url"
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

// RPCEndpoint keeps chain-specific bindings separate from reusable accounts.
type RPCEndpoint struct {
	entity.Catalog
	NetworkID            id.ID  `db:"network_id" json:"networkId" meta:"label:Сеть,ref:blockchain_network"`
	Role                 string `db:"role" json:"role" meta:"label:Роль"`
	ProviderID           string `db:"provider_id" json:"providerId" meta:"label:Код провайдера"`
	TrustGroup           string `db:"trust_group" json:"trustGroup" meta:"label:Группа доверия"`
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
	return &RPCEndpoint{Catalog: entity.NewCatalog(code, name), Role: "rpc", Slot: "default", IsActive: true}
}
func (e *RPCEndpoint) Validate(ctx context.Context) error {
	if err := e.Catalog.Validate(ctx); err != nil {
		return err
	}
	if id.IsNil(e.NetworkID) || e.ProviderID == "" || e.TrustGroup == "" || e.IsFolder || e.ParentID != nil {
		return apperror.NewValidation("Укажите сеть, код провайдера и группу доверия")
	}
	if e.Role != "rpc" && e.Role != "indexer" && e.Role != "fullnode" && e.Role != "solidity" {
		return apperror.NewValidation("Неизвестная роль RPC")
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
	return nil
}
func NewRPCEndpointService(repo domain.CatalogRepository[*RPCEndpoint], num numerator.Generator) *domain.CatalogService[*RPCEndpoint] {
	return domain.NewCatalogService(domain.CatalogServiceConfig[*RPCEndpoint]{Repo: repo, Numerator: num, Definition: &RPCEndpointDefinition})
}
