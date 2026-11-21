package crypto_repo

import (
	"context"
	"errors"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
)

var ErrAMLRiskContractRequired = errors.New("AML scope requires risk-class source snapshots and dispatch permits")

// amlSpendScopeEnabled reads the exact merchant/network/asset policy. SQL
// failures are propagated; disabled scope is an explicit supported policy.
func amlSpendScopeEnabled(ctx context.Context, walletID, tokenID, networkID id.ID, contract string) (bool, error) {
	var enabled bool
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT EXISTS(
 SELECT 1 FROM cat_wallets w JOIN cat_blockchain_networks n ON n.id=w.network_id
 JOIN cat_tokens t ON t.network_id=w.network_id
 JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=w.custody_merchant_id AND s.network_id=w.network_id AND s.token_id=t.id AND s.enabled
 WHERE w.id=$1 AND w.network_id=$3 AND (($2::uuid<>'00000000-0000-0000-0000-000000000000'::uuid AND t.id=$2)
 OR ($2::uuid='00000000-0000-0000-0000-000000000000'::uuid AND CASE WHEN n.chain_family='evm' THEN LOWER(COALESCE(t.contract_address,''))=LOWER($4) ELSE COALESCE(t.contract_address,'')=$4 END)))`, walletID, tokenID, networkID, contract).Scan(&enabled)
	return enabled, err
}

// RequireAMLTransferPlan permits planning only for an exact registered source
// contract. Actual signing and sending each require a durable dispatch start.
func RequireAMLTransferPlan(ctx context.Context, intent crypto.TransferIntent) error {
	enabled, err := amlSpendScopeEnabled(ctx, intent.SourceWalletID, intent.Asset.TokenID, intent.Network.ID, intent.Asset.ContractAddress)
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}
	var matches bool
	err = postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT fn_crypto_aml_spend_plan_matches($1,$2,$3,$4,t.id,$5,$6) FROM cat_tokens t JOIN cat_blockchain_networks n ON n.id=t.network_id WHERE t.network_id=$7 AND (($8::uuid<>'00000000-0000-0000-0000-000000000000'::uuid AND t.id=$8) OR ($8::uuid='00000000-0000-0000-0000-000000000000'::uuid AND CASE WHEN n.chain_family='evm' THEN LOWER(COALESCE(t.contract_address,''))=LOWER($9) ELSE COALESCE(t.contract_address,'')=$9 END))`, intent.BasisType, intent.BasisID, intent.Purpose, intent.SourceWalletID, intent.DestinationAddress, intent.Amount, intent.Network.ID, intent.Asset.TokenID, intent.Asset.ContractAddress).Scan(&matches)
	if err != nil {
		return err
	}
	if !matches {
		return ErrAMLRiskContractRequired
	}
	return nil
}

func BeginAMLDispatch(ctx context.Context, authorization string, phase string) error {
	if postgres.MustGetTxManager(ctx).GetTx(ctx) != nil {
		return apperror.NewConflict("external dispatch requires a committed SQL permit outside a caller transaction")
	}
	return postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		var start *id.ID
		return postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT fn_crypto_aml_begin_dispatch((SELECT id FROM reg_crypto_signing_authorizations WHERE authorization_id=$1),$2)`, authorization, phase).Scan(&start)
	})
}
