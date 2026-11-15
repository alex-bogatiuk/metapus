package architecture_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// configurationPackages lists the import paths (and their subpackages) that
// belong to the crypto-processing configuration rather than to the reusable
// platform. Everything else under internal/ and pkg/ is treated as platform,
// including the shared applied catalogs (currency, organization, counterparty,
// contract) and the exchange-rate subsystem.
var configurationPackages = []string{
	"metapus/internal/content",

	"metapus/internal/domain/catalogs/blockchain_network",
	"metapus/internal/domain/catalogs/crypto_provider_account",
	"metapus/internal/domain/catalogs/merchant",
	"metapus/internal/domain/catalogs/token",
	"metapus/internal/domain/catalogs/wallet",
	"metapus/internal/domain/crypto",
	"metapus/internal/domain/dashboard",
	"metapus/internal/domain/documents/crypto_custody_receipt",
	"metapus/internal/domain/documents/crypto_invoice",
	"metapus/internal/domain/documents/crypto_merchant_credit",
	"metapus/internal/domain/documents/crypto_network_operation",
	"metapus/internal/domain/documents/crypto_payment",
	"metapus/internal/domain/documents/crypto_platform_funding",
	"metapus/internal/domain/documents/crypto_provider_balance_operation",
	"metapus/internal/domain/documents/crypto_resource_rental",
	"metapus/internal/domain/documents/crypto_sweep",
	"metapus/internal/domain/documents/crypto_withdrawal",
	"metapus/internal/domain/documents/outbound_settlement",
	"metapus/internal/domain/documents/treasury_withdrawal",
	"metapus/internal/domain/documents/withdrawal_request",
	"metapus/internal/domain/registers/crypto_balance",
	"metapus/internal/domain/registers/crypto_fee",
	"metapus/internal/domain/registers/crypto_merchant_balance",
	"metapus/internal/domain/registers/crypto_platform_cost",
	"metapus/internal/domain/registers/crypto_provider_balance",
	"metapus/internal/domain/registers/crypto_receipt_suspense",
	"metapus/internal/domain/tariff",

	"metapus/internal/application/checkout",
	"metapus/internal/application/cryptoapp",
	"metapus/internal/application/dashboardapp",
	"metapus/internal/application/outboundapp",
	"metapus/internal/application/portalapp",

	"metapus/internal/infrastructure/aml",
	"metapus/internal/infrastructure/blockchain",
	"metapus/internal/infrastructure/crypto",
	"metapus/internal/infrastructure/crypto_worker",
	"metapus/internal/infrastructure/ingestionmonitor",
	"metapus/internal/infrastructure/integrations",
	"metapus/internal/infrastructure/vaultclient",
	"metapus/internal/infrastructure/storage/postgres/crypto_repo",
	"metapus/internal/infrastructure/storage/postgres/dashboard_repo",
	"metapus/internal/infrastructure/storage/postgres/money_movement_repo",
	"metapus/internal/infrastructure/storage/postgres/outbound_repo",
	"metapus/internal/infrastructure/storage/postgres/portal_repo",
}

// knownConfigurationLeaks records platform → configuration imports that
// existed when the boundary was introduced. The list may only shrink: a new
// edge fails the test, and an edge that disappears must be removed here.
var knownConfigurationLeaks = map[string][]string{
	"metapus/internal/application/exchangerateapp": {
		"metapus/internal/domain/crypto",
	},
	"metapus/internal/domain/auth": {
		"metapus/internal/domain/catalogs/merchant",
	},
	"metapus/internal/domain/posting": {
		"metapus/internal/domain/registers/crypto_balance",
		"metapus/internal/domain/registers/crypto_fee",
		"metapus/internal/domain/registers/crypto_merchant_balance",
		"metapus/internal/domain/registers/crypto_platform_cost",
		"metapus/internal/domain/registers/crypto_provider_balance",
		"metapus/internal/domain/registers/crypto_receipt_suspense",
	},
	"metapus/internal/infrastructure/http/contracts": {
		"metapus/internal/application/cryptoapp",
		"metapus/internal/content",
	},
	"metapus/internal/infrastructure/http/v1": {
		"metapus/internal/application/checkout",
		"metapus/internal/application/cryptoapp",
		"metapus/internal/application/dashboardapp",
		"metapus/internal/application/portalapp",
		"metapus/internal/domain/catalogs/merchant",
		"metapus/internal/domain/catalogs/wallet",
		"metapus/internal/domain/crypto",
		"metapus/internal/domain/documents/crypto_invoice",
		"metapus/internal/domain/registers/crypto_balance",
		"metapus/internal/domain/registers/crypto_fee",
		"metapus/internal/domain/registers/crypto_merchant_balance",
		"metapus/internal/domain/registers/crypto_platform_cost",
		"metapus/internal/domain/registers/crypto_provider_balance",
		"metapus/internal/domain/registers/crypto_receipt_suspense",
		"metapus/internal/infrastructure/integrations",
		"metapus/internal/infrastructure/storage/postgres/crypto_repo",
		"metapus/internal/infrastructure/storage/postgres/dashboard_repo",
		"metapus/internal/infrastructure/storage/postgres/money_movement_repo",
		"metapus/internal/infrastructure/storage/postgres/portal_repo",
	},
	"metapus/internal/infrastructure/http/v1/dto": {
		"metapus/internal/application/cryptoapp",
		"metapus/internal/application/portalapp",
		"metapus/internal/domain/catalogs/blockchain_network",
		"metapus/internal/domain/catalogs/crypto_provider_account",
		"metapus/internal/domain/catalogs/merchant",
		"metapus/internal/domain/catalogs/token",
		"metapus/internal/domain/catalogs/wallet",
		"metapus/internal/domain/crypto",
		"metapus/internal/domain/crypto/aml",
		"metapus/internal/domain/dashboard",
		"metapus/internal/domain/documents/crypto_custody_receipt",
		"metapus/internal/domain/documents/crypto_invoice",
		"metapus/internal/domain/documents/crypto_merchant_credit",
		"metapus/internal/domain/documents/crypto_network_operation",
		"metapus/internal/domain/documents/crypto_payment",
		"metapus/internal/domain/documents/crypto_platform_funding",
		"metapus/internal/domain/documents/crypto_provider_balance_operation",
		"metapus/internal/domain/documents/crypto_resource_rental",
		"metapus/internal/domain/documents/crypto_sweep",
		"metapus/internal/domain/documents/crypto_withdrawal",
		"metapus/internal/domain/documents/treasury_withdrawal",
	},
	"metapus/internal/infrastructure/http/v1/handlers": {
		"metapus/internal/application/checkout",
		"metapus/internal/application/cryptoapp",
		"metapus/internal/application/dashboardapp",
		"metapus/internal/application/portalapp",
		"metapus/internal/domain/catalogs/merchant",
		"metapus/internal/domain/catalogs/wallet",
		"metapus/internal/domain/crypto",
		"metapus/internal/domain/crypto/aml",
		"metapus/internal/domain/documents/crypto_invoice",
		"metapus/internal/domain/documents/crypto_network_operation",
		"metapus/internal/domain/documents/crypto_payment",
		"metapus/internal/domain/documents/crypto_provider_balance_operation",
		"metapus/internal/domain/documents/crypto_resource_rental",
		"metapus/internal/domain/documents/crypto_sweep",
		"metapus/internal/domain/documents/crypto_withdrawal",
		"metapus/internal/domain/documents/treasury_withdrawal",
		"metapus/internal/domain/documents/withdrawal_request",
		"metapus/internal/infrastructure/crypto/webhook",
		"metapus/internal/infrastructure/crypto_worker",
		"metapus/internal/infrastructure/storage/postgres/crypto_repo",
		"metapus/internal/infrastructure/storage/postgres/money_movement_repo",
		"metapus/internal/infrastructure/storage/postgres/portal_repo",
	},
	"metapus/internal/infrastructure/http/v1/middleware": {
		"metapus/internal/application/checkout",
		"metapus/internal/domain/catalogs/merchant",
	},
	"metapus/internal/infrastructure/rate_feed": {
		"metapus/internal/infrastructure/integrations",
	},
	"metapus/internal/infrastructure/storage/postgres": {
		"metapus/internal/application/checkout",
		"metapus/internal/domain/crypto",
		"metapus/internal/domain/documents/crypto_network_operation",
	},
	"metapus/internal/infrastructure/storage/postgres/auth_repo": {
		"metapus/internal/domain/catalogs/merchant",
	},
	"metapus/internal/infrastructure/storage/postgres/catalog_repo": {
		"metapus/internal/domain/catalogs/blockchain_network",
		"metapus/internal/domain/catalogs/crypto_provider_account",
		"metapus/internal/domain/catalogs/merchant",
		"metapus/internal/domain/catalogs/token",
		"metapus/internal/domain/catalogs/wallet",
		"metapus/internal/domain/crypto",
	},
	"metapus/internal/infrastructure/storage/postgres/document_repo": {
		"metapus/internal/domain/catalogs/wallet",
		"metapus/internal/domain/documents/crypto_custody_receipt",
		"metapus/internal/domain/documents/crypto_invoice",
		"metapus/internal/domain/documents/crypto_merchant_credit",
		"metapus/internal/domain/documents/crypto_network_operation",
		"metapus/internal/domain/documents/crypto_payment",
		"metapus/internal/domain/documents/crypto_platform_funding",
		"metapus/internal/domain/documents/crypto_provider_balance_operation",
		"metapus/internal/domain/documents/crypto_resource_rental",
		"metapus/internal/domain/documents/crypto_sweep",
		"metapus/internal/domain/documents/crypto_withdrawal",
		"metapus/internal/domain/documents/treasury_withdrawal",
	},
	"metapus/internal/infrastructure/storage/postgres/integration_repo": {
		"metapus/internal/domain/crypto",
		"metapus/internal/infrastructure/blockchain/chainconfig",
		"metapus/internal/infrastructure/blockchain/rpcpolicy",
		"metapus/internal/infrastructure/integrations",
		"metapus/internal/infrastructure/integrations/profiles",
	},
	"metapus/internal/infrastructure/storage/postgres/register_repo": {
		"metapus/internal/domain/registers/crypto_balance",
		"metapus/internal/domain/registers/crypto_fee",
		"metapus/internal/domain/registers/crypto_merchant_balance",
	},
}

func TestPlatformDoesNotDependOnConfiguration(t *testing.T) {
	t.Parallel()
	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(currentFile)))

	for _, pkg := range configurationPackages {
		dir := filepath.Join(repoRoot, filepath.FromSlash(strings.TrimPrefix(pkg, "metapus/")))
		info, err := os.Stat(dir)
		require.NoErrorf(t, err, "configuration package %s no longer exists; update configurationPackages", pkg)
		require.Truef(t, info.IsDir(), "configuration package %s is not a directory", pkg)
	}

	actual := map[string]map[string]bool{}
	for _, top := range []string{"internal", "pkg"} {
		err := filepath.WalkDir(filepath.Join(repoRoot, top), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, err := filepath.Rel(repoRoot, filepath.Dir(path))
			if err != nil {
				return err
			}
			pkg := "metapus/" + filepath.ToSlash(rel)
			if isConfigurationPackage(pkg) {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, imported := range file.Imports {
				value, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					return err
				}
				if !isConfigurationPackage(value) {
					continue
				}
				if actual[pkg] == nil {
					actual[pkg] = map[string]bool{}
				}
				actual[pkg][value] = true
			}
			return nil
		})
		require.NoError(t, err)
	}

	for pkg, imports := range actual {
		for imported := range imports {
			if !slices.Contains(knownConfigurationLeaks[pkg], imported) {
				t.Errorf("platform package %s imports configuration package %s; "+
					"move the dependency into the configuration or invert it through a platform interface", pkg, imported)
			}
		}
	}
	for pkg, allowed := range knownConfigurationLeaks {
		for _, imported := range allowed {
			if !actual[pkg][imported] {
				t.Errorf("leak %s -> %s is gone; remove it from knownConfigurationLeaks", pkg, imported)
			}
		}
	}
}

func isConfigurationPackage(importPath string) bool {
	for _, pkg := range configurationPackages {
		if importPath == pkg || strings.HasPrefix(importPath, pkg+"/") {
			return true
		}
	}
	return false
}
