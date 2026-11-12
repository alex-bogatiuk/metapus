package catalog_repo

import (
	"context"
	"fmt"
	"net/url"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/storage/postgres"
)

type CryptoRPCEndpointRepo struct {
	*BaseCatalogRepo[*integrations.RPCEndpoint]
}

func NewCryptoRPCEndpointRepo() *CryptoRPCEndpointRepo {
	return &CryptoRPCEndpointRepo{NewCatalogRepoForDefinition[*integrations.RPCEndpoint](integrations.RPCEndpointDefinition, postgres.ExtractDBColumns[integrations.RPCEndpoint](), func() *integrations.RPCEndpoint { return &integrations.RPCEndpoint{} })}
}
func (r *CryptoRPCEndpointRepo) ValidateBinding(ctx context.Context, e *integrations.RPCEndpoint) error {
	if err := LockIntegrationConfiguration(ctx); err != nil {
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
		if provider, ok := integrations.RPCProviderForHost(u.Hostname()); ok {
			definition, _ := provider.Definition()
			if definition.ManagedRPCOnly {
				return apperror.NewValidation("Для этого RPC выберите пул аккаунтов; адрес сохраняется без ключа")
			}
		}
		return nil
	}
	var provider integrations.Provider
	if err := q.QueryRow(ctx, `SELECT provider FROM cat_integration_pools WHERE id=$1 AND NOT deletion_mark`, e.PoolID).Scan(&provider); err != nil {
		return apperror.NewValidation("Выберите существующий пул аккаунтов")
	}
	if !provider.Supports(integrations.KindRPC) {
		return apperror.NewValidation("Выберите пул провайдера с назначением «Доступ к блокчейну — RPC»")
	}
	// The provider profile fixes the key name for most bindings. An omitted
	// name (or the generic default) takes that name; another one is rejected.
	if name := provider.RPCKeyName(binding); name != "" && (e.Slot == "" || e.Slot == "default") {
		e.Slot = name
	}
	if e.Slot == "" {
		e.Slot = "default"
	}
	binding.Slot = e.Slot
	if err := provider.ValidateRPCBinding(binding); err != nil {
		return err
	}
	members, err := poolMemberIDs(ctx, *e.PoolID)
	if err != nil {
		return err
	}
	holders, err := loadRPCKeyHolders(ctx, members)
	if err != nil {
		return err
	}
	if err := requireRPCKeys([]rpcKeyUser{{name: e.Name, slot: e.Slot}}, holders); err != nil {
		return err
	}
	definition, _ := provider.Definition()
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
func (r *CryptoRPCEndpointRepo) Create(ctx context.Context, e *integrations.RPCEndpoint) error {
	return r.mutate(ctx, e.NetworkID, func(ctx context.Context) error {
		if err := r.ValidateBinding(ctx, e); err != nil {
			return err
		}
		return r.BaseCatalogRepo.Create(ctx, e)
	})
}
func (r *CryptoRPCEndpointRepo) Update(ctx context.Context, e *integrations.RPCEndpoint) error {
	return postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		if err := LockIntegrationConfiguration(ctx); err != nil {
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
				return validateRPCNetworkMutation(ctx, e.NetworkID, apply)
			}
			return apply(ctx)
		})
	})
}

func (r *CryptoRPCEndpointRepo) mutate(ctx context.Context, network id.ID, mutation func(context.Context) error) error {
	return postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		if err := LockIntegrationConfiguration(ctx); err != nil {
			return err
		}
		return validateRPCNetworkMutation(ctx, network, mutation)
	})
}

func (r *CryptoRPCEndpointRepo) SetDeletionMark(ctx context.Context, uid id.ID, marked bool) error {
	return postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		if err := LockIntegrationConfiguration(ctx); err != nil {
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
		if err := LockIntegrationConfiguration(ctx); err != nil {
			return err
		}
		e, err := r.GetByID(ctx, uid)
		if err != nil {
			return err
		}
		return r.mutate(ctx, e.NetworkID, func(ctx context.Context) error { return r.BaseCatalogRepo.Delete(ctx, uid) })
	})
}
