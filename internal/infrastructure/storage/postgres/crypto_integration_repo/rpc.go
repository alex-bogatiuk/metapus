package crypto_integration_repo

import (
	"context"
	"errors"
	"fmt"
	"metapus/internal/domain/crypto/cryptointegrations"
	"metapus/internal/domain/crypto/rpcconfig"
	"metapus/internal/infrastructure/crypto/cryptoprofiles"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/crypto"
	model "metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/blockchain/chainconfig"
	integrationhttp "metapus/internal/infrastructure/integrations"
	"metapus/internal/infrastructure/providerpolicy"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/integration_repo"
)

// LoadRPCNetwork overrides file topology only when the tenant explicitly selects database mode.
func LoadRPCNetwork(ctx context.Context, network crypto.NetworkRef, fallback chainconfig.Network) (chainconfig.Network, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var source string
	var evmRaw, tronRaw, endpointsRaw []byte
	err := q.QueryRow(ctx, `SELECT n.rpc_config_source,COALESCE(s.evm_settings,'null'::jsonb),COALESCE(s.tron_settings,'null'::jsonb),COALESCE((SELECT jsonb_agg(jsonb_build_object('poolId',e.pool_id,'id',e.id,'role',e.role,'configVersion',e.version,'providerId',e.provider_id,'trustGroup',e.trust_group,'url',e.url,'priority',e.priority,'minRequestIntervalMs',e.min_request_interval_ms,'disableKeepAlives',e.disable_keep_alives) ORDER BY e.priority,e.id) FROM sys_crypto_rpc_endpoints e WHERE e.network_id=n.id AND e.is_active AND NOT e.deletion_mark AND e._deleted_at IS NULL),'[]'::jsonb) FROM cat_blockchain_networks n LEFT JOIN sys_crypto_rpc_network_settings s ON s.network_id=n.id WHERE n.id=$1`, network.ID).Scan(&source, &evmRaw, &tronRaw, &endpointsRaw)
	if err != nil {
		return chainconfig.Network{}, fmt.Errorf("load tenant RPC configuration: %w", err)
	}
	if source != "database" {
		return fallback, nil
	}
	out := chainconfig.Network{Roles: make(map[chainconfig.Role][]chainconfig.Endpoint)}
	if err = json.Unmarshal(evmRaw, &out.EVM); err != nil {
		return out, fmt.Errorf("decode EVM settings: %w", err)
	}
	if err = json.Unmarshal(tronRaw, &out.TRON); err != nil {
		return out, fmt.Errorf("decode TRON settings: %w", err)
	}
	var endpoints []struct {
		PoolID *id.ID           `json:"poolId"`
		ID     id.ID            `json:"id"`
		Role   chainconfig.Role `json:"role"`
		chainconfig.Endpoint
	}
	if err = json.Unmarshal(endpointsRaw, &endpoints); err != nil {
		return out, fmt.Errorf("decode tenant RPC endpoints: %w", err)
	}
	for _, entry := range endpoints {
		endpoint := entry.Endpoint
		endpoint.EndpointID = entry.ID
		endpoint.AdmissionScope = tenant.GetTenantID(ctx) + "/endpoint:" + entry.ID.String()
		if entry.PoolID != nil {
			endpoint.AdmissionScope = tenant.GetTenantID(ctx) + "/integration-pool:" + entry.PoolID.String()
		}
		endpoint.WrapTransport = RPCTransport(entry.ID, endpoint.URL)
		out.Roles[entry.Role] = append(out.Roles[entry.Role], endpoint)
	}
	return out, nil
}

func RPCTransport(uid id.ID, originalURL string) func(http.RoundTripper) http.RoundTripper {
	return rpcTransportAt(uid, originalURL, time.Now)
}
func rpcTransportAt(uid id.ID, originalURL string, now func() time.Time) func(http.RoundTripper) http.RoundTripper {
	return func(base http.RoundTripper) http.RoundTripper {
		// Pin the validated public IP when opening each connection; disable proxy bypasses.
		if original, ok := base.(*http.Transport); ok {
			safe := original.Clone()
			safe.Proxy = nil
			safe.DialContext = providerpolicy.PublicDialContext(os.Getenv("CRYPTO_RPC_DNS_TLS_SERVER"))
			base = safe
		}
		var mu sync.Mutex
		var cached integrationhttp.Binding
		var checked time.Time
		load := func(ctx context.Context) (integrationhttp.Binding, error) {
			mu.Lock()
			defer mu.Unlock()
			if now().Sub(checked) < 10*time.Second {
				return cached, nil
			}
			q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
			var pool *id.ID
			var provider *model.Provider
			var b integrationhttp.Binding
			err := q.QueryRow(ctx, `SELECT e.url,e.pool_id,p.provider,e.credential_slot,e.min_request_interval_ms FROM sys_crypto_rpc_endpoints e LEFT JOIN cat_integration_pools p ON p.id=e.pool_id WHERE e.id=$1 AND e.is_active AND NOT e.deletion_mark AND e._deleted_at IS NULL`, uid).Scan(&b.URL, &pool, &provider, &b.Slot, &b.MinRequestIntervalMS)
			if err != nil {
				return b, fmt.Errorf("RPC endpoint unavailable")
			}
			if pool != nil {
				b.PoolID = *pool
			}
			if provider != nil {
				b.Provider = *provider
			}
			// A managed key can only be delivered to its provider's supported endpoint.
			u, e := url.Parse(b.URL)
			if e != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
				return b, fmt.Errorf("invalid RPC endpoint")
			}
			if b.Provider != "" {
				if _, ok := rpcconfig.RPCProfileForURL(b.Provider, b.URL); !ok {
					return b, fmt.Errorf("unsupported or invalid RPC profile")
				}
			}
			cached = b
			checked = now()
			return b, nil
		}
		runtime := integration_repo.NewRuntime(cryptointegrations.Catalog())
		runtime.Now = now
		return &integrationhttp.Transport{Base: base, Repository: runtime, Profiles: cryptoprofiles.Registry(), CursorPin: cryptoprofiles.TronGridCursor(), Target: load, OriginalURL: originalURL, Now: now, Operation: RPCOperation}
	}
}

// ConnectionTargets resolves which crypto RPC endpoint a pool's manual
// connection check exercises.
type ConnectionTargets struct{}

// NewConnectionTargets creates the crypto RPC connection target resolver.
func NewConnectionTargets() ConnectionTargets { return ConnectionTargets{} }

// ConnectionEndpoint chooses an actual binding (including its named key slot)
// for the pool's manual access check. Detailed per-network checks use preflight.
type ConnectionEndpoint struct {
	ID                id.ID
	URL               string
	Role              string
	Family            string
	NetworkIdentifier string
}

func (r ConnectionTargets) ConnectionEndpoint(ctx context.Context, poolID id.ID) (*ConnectionEndpoint, error) {
	var endpoint ConnectionEndpoint
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT e.id,e.url,e.role,n.chain_family,n.network_identifier FROM sys_crypto_rpc_endpoints e JOIN cat_blockchain_networks n ON n.id=e.network_id AND NOT n.deletion_mark AND n._deleted_at IS NULL WHERE e.pool_id=$1 AND e.is_active AND NOT e.deletion_mark AND e._deleted_at IS NULL ORDER BY e.priority,e.id LIMIT 1`, poolID).Scan(&endpoint.ID, &endpoint.URL, &endpoint.Role, &endpoint.Family, &endpoint.NetworkIdentifier)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load integration connection binding: %w", err)
	}
	return &endpoint, nil
}

func (r ConnectionTargets) ConnectionTarget(ctx context.Context, poolID id.ID) (model.ConnectionTarget, error) {
	pool, err := NewIntegrationPoolRepo().GetByID(ctx, poolID)
	if err != nil {
		return model.ConnectionTarget{}, err
	}
	out := model.ConnectionTarget{PoolID: poolID, Provider: pool.Provider}
	if !cryptointegrations.Catalog().Supports(pool.Provider, cryptointegrations.KindRPC) {
		return out, nil
	}
	endpoint, err := r.ConnectionEndpoint(ctx, poolID)
	if err != nil {
		return out, err
	}
	if endpoint != nil {
		out.EndpointID, out.URL = endpoint.ID, endpoint.URL
		out.Attributes = RPCTargetAttributes(endpoint.Family, endpoint.NetworkIdentifier, endpoint.Role)
	}
	return out, nil
}

// RPCOperation names a TRON HTTP API call by its /wallet/ or /walletsolidity/
// method for the integration wire metric; other RPC calls count as "api".
func RPCOperation(req *http.Request) string {
	_, operation, _ := strings.Cut(req.URL.Path, "/wallet/")
	if operation == "" {
		_, operation, _ = strings.Cut(req.URL.Path, "/walletsolidity/")
	}
	return operation
}
