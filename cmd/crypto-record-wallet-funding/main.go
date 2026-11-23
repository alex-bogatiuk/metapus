package main

import (
	"context"
	"flag"
	"fmt"
	"math/big"
	"os"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/blockchain/evm"
	evmfunding "github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/blockchain/evm/funding"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/blockchain/tron"
	tronfunding "github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/blockchain/tron/funding"
	"github.com/alex-bogatiuk/metapus/internal/core/json"
	"github.com/alex-bogatiuk/metapus/internal/core/tenant"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/tenantdb"
	"github.com/alex-bogatiuk/metapus/internal/platform/secretfile"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	var walletCode string
	var tokenCode string
	var txHash string
	flag.StringVar(&walletCode, "wallet-code", "", "managed wallet code receiving the transfer")
	flag.StringVar(&tokenCode, "token-code", "", "managed token code receiving the transfer")
	flag.StringVar(&txHash, "tx-hash", "", "financially final native or supported token transaction hash")
	flag.Parse()

	dsn, err := secretfile.Get("DATABASE_URL", "")
	if err != nil {
		return fmt.Errorf("load DATABASE_URL: %w", err)
	}
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	configPath := os.Getenv("CRYPTO_CHAIN_CONFIG_FILE")
	if configPath == "" {
		return fmt.Errorf("CRYPTO_CHAIN_CONFIG_FILE is required")
	}
	chainConfig, err := chainconfig.Load(configPath)
	if err != nil {
		return fmt.Errorf("load chain config: %w", err)
	}
	networkCode := os.Getenv("CRYPTO_NETWORK_CODE")
	runtime, ok := chainConfig.Networks[networkCode]
	if networkCode == "" || !ok {
		return fmt.Errorf("CRYPTO_NETWORK_CODE must select a configured network")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolCfg := postgres.DefaultPoolConfig(dsn)
	poolCfg.MaxConns = 2
	poolCfg.MinConns = 1
	pool, err := postgres.NewPool(ctx, poolCfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	txm := postgres.NewTxManager(pool)
	ctx = tenantdb.WithPool(ctx, pool.Unwrap())
	ctx = tenant.WithTxManager(ctx, txm)

	var family string
	var networkIdentifier string
	if err := txm.GetQuerier(ctx).QueryRow(ctx, `
		SELECT chain_family, network_identifier
		FROM cat_blockchain_networks
		WHERE code = $1 AND is_active = TRUE AND deletion_mark = FALSE
	`, networkCode).Scan(&family, &networkIdentifier); err != nil {
		return fmt.Errorf("load active funding network %s: %w", networkCode, err)
	}

	var result any
	switch crypto.ChainFamily(family) {
	case crypto.ChainFamilyTRON:
		settings := chainconfig.TRONSettings{}
		if runtime.TRON != nil {
			settings = *runtime.TRON
		}
		roles := []chainconfig.Role{chainconfig.RoleFullNode, chainconfig.RoleSolidity}
		if err := runtime.RequireRoles(roles...); err != nil {
			return fmt.Errorf("network %s runtime endpoints: %w", networkCode, err)
		}
		var decoderVersion int
		if err := txm.GetQuerier(ctx).QueryRow(ctx, `SELECT i.decoder_version FROM sys_crypto_block_ingestion i JOIN cat_blockchain_networks n ON n.id=i.network_id WHERE n.code=$1`, networkCode).Scan(&decoderVersion); err != nil {
			return err
		}
		if decoderVersion != crypto.TRONTransferEvidenceSchema {
			return crypto.ErrDecoderVersionConflict
		}
		rpcPool, err := tron.NewRPCPool(tron.RPCPoolConfig{
			Settings:    settings,
			NetworkCode: networkCode, FailureThreshold: 3, OpenDuration: 30 * time.Second,
			FullNode: runtime.Endpoints(chainconfig.RoleFullNode),
			Solidity: runtime.Endpoints(chainconfig.RoleSolidity),
		})
		if err != nil {
			return fmt.Errorf("build TRON RPC pool: %w", err)
		}
		recorded, err := tronfunding.RecordConfirmedWalletFunding(ctx, rpcPool, walletCode, tokenCode, txHash)
		if err != nil {
			return err
		}
		result = recorded
	case crypto.ChainFamilyEVM:
		if err := runtime.RequireRoles(chainconfig.RoleRPC); err != nil {
			return fmt.Errorf("network %s runtime endpoints: %w", networkCode, err)
		}
		chainID, ok := new(big.Int).SetString(networkIdentifier, 10)
		if !ok || chainID.Sign() <= 0 {
			return fmt.Errorf("network %s has invalid EVM chain ID %q", networkCode, networkIdentifier)
		}
		rpcPool, err := evm.NewRPCPool(ctx, evm.RPCPoolConfig{
			NetworkCode: networkCode, ExpectedChainID: chainID,
			FailureThreshold: 3, OpenDuration: 30 * time.Second,
			Endpoints: runtime.Endpoints(chainconfig.RoleRPC),
		})
		if err != nil {
			return fmt.Errorf("build EVM RPC pool: %w", err)
		}
		defer rpcPool.Close()
		recorded, err := evmfunding.RecordConfirmedWalletFunding(ctx, rpcPool, chainID, walletCode, tokenCode, txHash)
		if err != nil {
			return err
		}
		result = recorded
	default:
		return fmt.Errorf("network %s has unsupported chain family %q", networkCode, family)
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
