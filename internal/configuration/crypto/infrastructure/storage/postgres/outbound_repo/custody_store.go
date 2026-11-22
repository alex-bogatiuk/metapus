package outbound_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
)

type CustodyStore struct{}

func (CustodyStore) LoadWallet(ctx context.Context, id id.ID) (CustodyWallet, error) {
	return loadCustodyWallet(ctx, id)
}

func (CustodyStore) FindWallet(ctx context.Context, network id.ID, address string) (CustodyWallet, error) {
	return findCustodyWallet(ctx, network, address)
}

func (CustodyStore) IsEnforced(ctx context.Context, network id.ID) (bool, error) {
	return merchantCustodyEnforced(ctx, network)
}

func (CustodyStore) IsManagedDestination(ctx context.Context, network id.ID, address, family string) (bool, error) {
	var managed bool
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cat_wallets WHERE network_id=$1 AND address_key=CASE WHEN $3='evm' THEN LOWER($2) ELSE $2 END)`, network, address, family).Scan(&managed)
	return managed, err
}

func (CustodyStore) RequireTransferPlan(ctx context.Context, intent crypto.TransferIntent) error {
	return crypto_repo.RequireAMLTransferPlan(ctx, intent)
}

func (CustodyStore) HasAML(ctx context.Context, network id.ID) (bool, error) {
	var enabled bool
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes WHERE network_id=$1 AND enabled)`, network).Scan(&enabled)
	return enabled, err
}

func (CustodyStore) BeginDispatch(ctx context.Context, id, stage string) error {
	return crypto_repo.BeginAMLDispatch(ctx, id, stage)
}

func (CustodyStore) FindFeeWalletForOwner(ctx context.Context, network id.ID, merchant *id.ID) (id.ID, error) {
	var fee id.ID
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT id FROM cat_wallets WHERE network_id=$1 AND custody_merchant_id IS NOT DISTINCT FROM $2::uuid AND wallet_role='network_fee' AND is_active AND NOT deletion_mark ORDER BY id LIMIT 1`, network, merchant).Scan(&fee)
	if errors.Is(err, pgx.ErrNoRows) {
		return fee, fmt.Errorf("%w: custody owner network fee wallet is missing", ErrMerchantCustody)
	}
	return fee, err
}

func (CustodyStore) LoadFundingWallet(ctx context.Context, wallet, network id.ID, role string) (HotWallet, error) {
	return loadNativeFundingWallet(ctx, wallet, network, role)
}
