package crypto_integration_repo

import (
	"context"
	"fmt"
	"metapus/internal/configuration/crypto/domain/crypto/cryptointegrations"
	"net/url"

	"metapus/internal/configuration/crypto/domain/crypto/rpcconfig"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
)

type CryptoRPCEndpointRepo struct {
	*catalog_repo.BaseCatalogRepo[*rpcconfig.RPCEndpoint]
}

func NewCryptoRPCEndpointRepo() *CryptoRPCEndpointRepo {
	return &CryptoRPCEndpointRepo{catalog_repo.NewCatalogRepoForDefinition[*rpcconfig.RPCEndpoint](rpcconfig.RPCEndpointDefinition, postgres.ExtractDBColumns[rpcconfig.RPCEndpoint](), func() *rpcconfig.RPCEndpoint { return &rpcconfig.RPCEndpoint{} })}
}
func (r *CryptoRPCEndpointRepo) ValidateBinding(ctx context.Context, e *rpcconfig.RPCEndpoint) error {
	if err := catalog_repo.LockIntegrationConfiguration(ctx); err != nil {
		return err
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	binding, err := rpcBindingFor(ctx, e.NetworkID, e.Role, e.URL, e.TrustGroup, e.Slot)
	if err != nil {
		return err
	}
	family := binding.Family
	if family == "evm" && e.Role != "rpc" || family == "tron" && e.Role == "rpc" {
		return apperror.NewValidation("Роль RPC не соответствует семейству сети")
	}
	if e.PoolID == nil {
		// Anonymous endpoints carry no account key.
		e.Slot = "default"
		u, err := url.Parse(e.URL)
		if err != nil {
			return err
		}
		if provider, ok := rpcconfig.RPCProviderForHost(u.Hostname()); ok {
			if rpcconfig.ManagedRPCOnly(provider) {
				return apperror.NewValidation("Для этого RPC выберите пул аккаунтов; адрес сохраняется без ключа")
			}
		}
		return nil
	}
	var provider integrations.Provider
	if err := q.QueryRow(ctx, `SELECT provider FROM cat_integration_pools WHERE id=$1 AND NOT deletion_mark`, e.PoolID).Scan(&provider); err != nil {
		return apperror.NewValidation("Выберите существующий пул аккаунтов")
	}
	if !cryptointegrations.Catalog().Supports(provider, cryptointegrations.KindRPC) {
		return apperror.NewValidation("Выберите пул провайдера с назначением «Доступ к блокчейну — RPC»")
	}
	// The provider profile fixes the key name for most bindings. An omitted
	// name (or the generic default) takes that name; another one is rejected.
	if name := rpcconfig.RPCKeyName(provider, binding); name != "" && (e.Slot == "" || e.Slot == "default") {
		e.Slot = name
	}
	if e.Slot == "" {
		e.Slot = "default"
	}
	binding.Slot = e.Slot
	if err := rpcconfig.ValidateRPCBinding(provider, binding); err != nil {
		return err
	}
	members, err := catalog_repo.PoolMemberIDs(ctx, *e.PoolID)
	if err != nil {
		return err
	}
	accounts, err := catalog_repo.AccountKeyNames(ctx, members)
	if err != nil {
		return err
	}
	if err := rpcconfig.RequireRPCKeys([]rpcconfig.RPCKeyUser{{Endpoint: e.Name, Key: e.Slot}}, accounts); err != nil {
		return err
	}
	definition, _ := cryptointegrations.Catalog().Definition(provider)
	if e.IsActive && definition.AccountMinRequestIntervalMS > 0 {
		var conflicting bool
		if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sys_crypto_rpc_endpoints other JOIN sys_integration_pool_accounts own ON own.pool_id=$1 JOIN sys_integration_pool_accounts shared ON shared.pool_id=other.pool_id AND shared.account_id=own.account_id WHERE other.id<>$2 AND other.is_active AND NOT other.deletion_mark AND other._deleted_at IS NULL AND GREATEST(other.min_request_interval_ms,$4::int)<>GREATEST($3::int,$4::int))`, e.PoolID, e.ID, e.MinRequestIntervalMS, definition.AccountMinRequestIntervalMS).Scan(&conflicting); err != nil {
			return err
		}
		if conflicting {
			return apperror.NewValidation("Для подключений одного аккаунта задайте одинаковый минимальный интервал запросов")
		}
	}
	return nil
}
func (r *CryptoRPCEndpointRepo) Create(ctx context.Context, e *rpcconfig.RPCEndpoint) error {
	return r.mutate(ctx, e.NetworkID, func(ctx context.Context) error {
		if err := r.ValidateBinding(ctx, e); err != nil {
			return err
		}
		return r.BaseCatalogRepo.Create(ctx, e)
	})
}
func (r *CryptoRPCEndpointRepo) Update(ctx context.Context, e *rpcconfig.RPCEndpoint) error {
	return postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		if err := catalog_repo.LockIntegrationConfiguration(ctx); err != nil {
			return err
		}
		old, err := r.GetByID(ctx, e.ID)
		if err != nil {
			return err
		}
		return r.mutate(ctx, old.NetworkID, func(ctx context.Context) error {
			apply := func(ctx context.Context) error {
				if err := r.ValidateBinding(ctx, e); err != nil {
					return err
				}
				if err := r.BaseCatalogRepo.Update(ctx, e); err != nil {
					return fmt.Errorf("update RPC endpoint: %w", err)
				}
				return nil
			}
			if old.NetworkID != e.NetworkID {
				return ValidateRPCNetworkMutation(ctx, e.NetworkID, apply)
			}
			return apply(ctx)
		})
	})
}

func (r *CryptoRPCEndpointRepo) mutate(ctx context.Context, network id.ID, mutation func(context.Context) error) error {
	return postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		if err := catalog_repo.LockIntegrationConfiguration(ctx); err != nil {
			return err
		}
		return ValidateRPCNetworkMutation(ctx, network, mutation)
	})
}

func (r *CryptoRPCEndpointRepo) SetDeletionMark(ctx context.Context, uid id.ID, marked bool) error {
	return postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		if err := catalog_repo.LockIntegrationConfiguration(ctx); err != nil {
			return err
		}
		e, err := r.GetByID(ctx, uid)
		if err != nil {
			return err
		}
		return r.mutate(ctx, e.NetworkID, func(ctx context.Context) error {
			if !marked {
				if err := r.ValidateBinding(ctx, e); err != nil {
					return err
				}
			}
			return r.BaseCatalogRepo.SetDeletionMark(ctx, uid, marked)
		})
	})
}

func (r *CryptoRPCEndpointRepo) Delete(ctx context.Context, uid id.ID) error {
	return postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		if err := catalog_repo.LockIntegrationConfiguration(ctx); err != nil {
			return err
		}
		e, err := r.GetByID(ctx, uid)
		if err != nil {
			return err
		}
		return r.mutate(ctx, e.NetworkID, func(ctx context.Context) error { return r.BaseCatalogRepo.Delete(ctx, uid) })
	})
}
