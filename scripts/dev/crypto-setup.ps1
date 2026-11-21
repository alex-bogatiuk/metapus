param(
    [string]$Mnemonic = "",
    [string]$TronApiKey = "",
    [string]$WithdrawalAddress = "",
    [string]$TenantSlug = "demo",
    [string]$TenantName = "Demo Tenant",
    [string]$VaultAddr = "http://127.0.0.1:8200",
    [string]$VaultToken = "dev-root-token",
    [int]$PoolCount = 6,
    [int]$PoolAccount = -1,
    [switch]$KeepVault,
    [switch]$NoPersistMnemonic
)

$ErrorActionPreference = "Stop"

. (Join-Path $PSScriptRoot "_crypto-env.ps1")

Set-Location $script:CryptoRepoRoot
$existingEnv = Import-CryptoEnv

$defaultWithdrawalTestAddress = "TQXx1eKQtVFjoDvyiGc3cRmJkSWE9KeXni"
$defaultFundingAddress1 = "TBmnfGQYXAM7yuAsbQnPwdWkz9Sm7afD2W"
$defaultFundingAddress2 = "TXdWeqExgQPYFroRsYgJkwNr2JfSh4dP7E"

if ([string]::IsNullOrWhiteSpace($Mnemonic)) {
    $Mnemonic = $env:TRON_DEV_MNEMONIC
}
if ([string]::IsNullOrWhiteSpace($TronApiKey)) {
    $TronApiKey = $env:TRON_TEST_API_KEY
}
if ([string]::IsNullOrWhiteSpace($WithdrawalAddress)) {
    $WithdrawalAddress = $env:TRON_WITHDRAWAL_TEST_ADDRESS
}
if ([string]::IsNullOrWhiteSpace($WithdrawalAddress)) {
    $WithdrawalAddress = $defaultWithdrawalTestAddress
}
if ([string]::IsNullOrWhiteSpace($Mnemonic)) {
    throw "Mnemonic is required. Pass -Mnemonic or set TRON_DEV_MNEMONIC in .env.crypto.local."
}
if ($PoolAccount -lt 0) {
    $configuredPoolAccount = $env:TRON_DEV_POOL_ACCOUNT
    if ([string]::IsNullOrWhiteSpace($configuredPoolAccount) -and $existingEnv.ContainsKey("TRON_DEV_POOL_ACCOUNT")) {
        $configuredPoolAccount = $existingEnv["TRON_DEV_POOL_ACCOUNT"]
    }
    if (-not [string]::IsNullOrWhiteSpace($configuredPoolAccount)) {
        $parsedPoolAccount = 0
        if (-not [int]::TryParse($configuredPoolAccount, [ref]$parsedPoolAccount)) {
            throw "TRON_DEV_POOL_ACCOUNT must be a non-negative integer."
        }
        $PoolAccount = $parsedPoolAccount
    } else {
        $PoolAccount = 0
    }
}
if ($PoolAccount -lt 0 -or $PoolAccount -gt 2147483647) {
    throw "PoolAccount must be a non-negative BIP-44 account index."
}

Assert-CryptoDockerReady

$metaDatabaseUrl = "postgres://metapus:metapus@localhost:5432/tenants?sslmode=disable"
$postgresAdminUrl = "postgres://metapus:metapus@localhost:5432/postgres?sslmode=disable"
$tenantDb = $null
$databaseUrl = $null
$cryptoConfigDir = Join-Path $script:CryptoRepoRoot ".crypto"
New-Item -ItemType Directory -Path $cryptoConfigDir -Force | Out-Null
$chainConfigPath = Join-Path $cryptoConfigDir "chains.dev.json"
$tronAPIKeyPath = Join-Path $cryptoConfigDir "trongrid-api-key"
$primaryEndpoint = [ordered]@{
    providerId = "trongrid-primary"
    priority = 0
    url = "https://nile.trongrid.io"
}
if (-not [string]::IsNullOrWhiteSpace($TronApiKey)) {
    [System.IO.File]::WriteAllText($tronAPIKeyPath, $TronApiKey, [System.Text.UTF8Encoding]::new($false))
    $primaryEndpoint.apiKeySecretFile = $tronAPIKeyPath
}
$secondaryEndpoint = [ordered]@{
    providerId = "nileex-secondary"
    priority = 10
    url = "https://api.nileex.io"
}
$chainConfig = [ordered]@{
    networks = [ordered]@{
        "TRON-NILE" = [ordered]@{
            roles = [ordered]@{
                indexer = @($primaryEndpoint)
                fullnode = @($primaryEndpoint, $secondaryEndpoint)
                solidity = @($primaryEndpoint, $secondaryEndpoint)
            }
        }
    }
}
[System.IO.File]::WriteAllText(
    $chainConfigPath,
    ($chainConfig | ConvertTo-Json -Depth 8),
    [System.Text.UTF8Encoding]::new($false)
)

$env:APP_ENV = "development"
$env:META_DATABASE_URL = $metaDatabaseUrl
$env:POSTGRES_ADMIN_URL = $postgresAdminUrl
$env:TENANT_DB_USER = "metapus"
$env:TENANT_DB_PASSWORD = "metapus"
$env:TENANT_DB_HOST = "localhost"
$env:TENANT_DB_PORT = "5432"
$env:DATABASE_URL = $databaseUrl
$env:SEED_DEMO_DATA = "true"
$env:SEED_CRYPTO_HISTORY = "false"
$env:TENANT_SLUG = $TenantSlug
$env:CRYPTO_PROCESSING_ENABLED = "true"
$env:CRYPTO_CHAIN_CONFIG_FILE = $chainConfigPath
$env:TRON_TEST_RPC_URL = "https://nile.trongrid.io"
$env:TRON_TEST_API_KEY = $TronApiKey
$env:TRON_DEV_POOL_ACCOUNT = $PoolAccount.ToString()
$env:AUTOMATION_ENCRYPTION_KEY = "test-encryption-key-32chars!!!!!"
$env:JWT_SECRET = "dev-secret-32-chars-change-me-123456"
$env:AUTH_MFA_ENCRYPTION_KEY = "dGVzdC1tZmEtZW5jcnlwdGlvbi1rZXktMzItYnl0ZXM="
$env:ADMIN_PASSWORD = "Admin123!"
$env:ADMIN_MFA_MODE = "required"
$env:ADMIN_MFA_GRACE_PERIOD_HOURS = "0"
$env:ADMIN_STEP_UP_TTL_SECONDS = "300"
$env:ADMIN_SECURITY_MODE = "enforce"
$env:ADMIN_ALLOWED_CIDRS = "127.0.0.1/32,::1/128"
$env:TRUSTED_PROXIES = ""

function Wait-Postgres {
    for ($i = 0; $i -lt 45; $i++) {
        & docker exec metapus-db pg_isready -U metapus -d mt_default *> $null
        if ($LASTEXITCODE -eq 0) {
            return
        }
        Start-Sleep -Seconds 1
    }
    throw "PostgreSQL did not become ready"
}

function Wait-Vault {
    for ($i = 0; $i -lt 45; $i++) {
        & docker exec -e "VAULT_ADDR=http://127.0.0.1:8200" metapus-vault vault status *> $null
        if ($LASTEXITCODE -eq 0) {
            return
        }
        Start-Sleep -Seconds 1
    }
    throw "Vault did not become ready"
}

function New-AuthorizationKeyPair {
    $tmp = Join-Path $env:TEMP "metapus_auth_key_$PID.go"
    @'
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
)

func main() {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]string{
		"public":  base64.RawURLEncoding.EncodeToString(pub),
		"private": base64.RawURLEncoding.EncodeToString(priv),
	})
}
'@ | Set-Content -Path $tmp -Encoding ASCII
    try {
        $json = & go run $tmp
        if ($LASTEXITCODE -ne 0) {
            throw "generate authorization key failed"
        }
        return ($json | ConvertFrom-Json)
    } finally {
        Remove-Item $tmp -Force -ErrorAction SilentlyContinue
    }
}

function Resolve-Ed25519KeyPair {
    param(
        [string]$PublicEnvName,
        [string]$PrivateEnvName
    )

    $public = [Environment]::GetEnvironmentVariable($PublicEnvName, "Process")
    $private = [Environment]::GetEnvironmentVariable($PrivateEnvName, "Process")
    if (-not [string]::IsNullOrWhiteSpace($public) -and -not [string]::IsNullOrWhiteSpace($private)) {
        return [pscustomobject]@{ public = $public; private = $private }
    }
    if ($existingEnv.ContainsKey($PublicEnvName) -and $existingEnv.ContainsKey($PrivateEnvName)) {
        return [pscustomobject]@{ public = $existingEnv[$PublicEnvName]; private = $existingEnv[$PrivateEnvName] }
    }
    return New-AuthorizationKeyPair
}

function New-ApprovalPolicyBundleJWS {
    param(
        [string]$TenantID,
        [string]$PrivateKey
    )

    $tmp = Join-Path $env:TEMP "metapus_policy_jws_$PID.go"
    @'
package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type policy struct {
	SchemaVersion             string `json:"schema_version"`
	PolicyVersion             string `json:"policy_version"`
	TenantID                  string `json:"tenant_id"`
	DefaultRequiredPrincipals []string `json:"default_required_principals"`
	Rules                     []rule `json:"rules"`
	EffectiveFrom             time.Time `json:"effective_from"`
	ExpiresAt                 time.Time `json:"expires_at"`
}

type rule struct {
	ID                 string   `json:"id"`
	Purpose            string   `json:"purpose,omitempty"`
	AmountMinorLTE     *string  `json:"amount_minor_lte,omitempty"`
	AmountMinorGTE     *string  `json:"amount_minor_gte,omitempty"`
	RiskLevelIn        []string `json:"risk_level_in,omitempty"`
	RequiredPrincipals []string `json:"required_principals"`
}

func main() {
	if len(os.Args) != 3 {
		panic("usage: policy-jws tenant-id private-key")
	}
	privateKey, err := decodePrivateKey(os.Args[2])
	if err != nil {
		panic(err)
	}
	lowWithdrawal := "100000000"
	smallTreasury := "500000000"
	highValue := "5000000000"
	p := policy{
		SchemaVersion: "approval_policy.v1",
		PolicyVersion: "money-movement-v1.0",
		TenantID: os.Args[1],
		DefaultRequiredPrincipals: []string{"admin", "risk"},
		Rules: []rule{
			{ID: "small-low-risk-withdrawal", Purpose: "withdrawal", AmountMinorLTE: &lowWithdrawal, RiskLevelIn: []string{"low"}, RequiredPrincipals: []string{"risk"}},
			{ID: "manual-treasury-small", Purpose: "treasury_withdrawal", AmountMinorLTE: &smallTreasury, RequiredPrincipals: []string{"admin"}},
			{ID: "high-value", AmountMinorGTE: &highValue, RequiredPrincipals: []string{"admin", "risk"}},
		},
		EffectiveFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		ExpiresAt: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	header, _ := json.Marshal(map[string]string{"alg": "EdDSA", "typ": "JWT"})
	payload, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}
	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	signature := ed25519.Sign(privateKey, []byte(signingInput))
	fmt.Print(signingInput + "." + base64.RawURLEncoding.EncodeToString(signature))
}

func decodePrivateKey(encoded string) (ed25519.PrivateKey, error) {
	if raw, err := base64.RawURLEncoding.DecodeString(encoded); err == nil && len(raw) == ed25519.PrivateKeySize {
		return ed25519.PrivateKey(raw), nil
	}
	if raw, err := base64.StdEncoding.DecodeString(encoded); err == nil && len(raw) == ed25519.PrivateKeySize {
		return ed25519.PrivateKey(raw), nil
	}
	if raw, err := hex.DecodeString(encoded); err == nil && len(raw) == ed25519.PrivateKeySize {
		return ed25519.PrivateKey(raw), nil
	}
	sum := sha256.Sum256([]byte(encoded))
	return nil, fmt.Errorf("invalid private key encoding or length: %x", sum[:4])
}
'@ | Set-Content -Path $tmp -Encoding ASCII
    try {
        $jws = & go run $tmp $TenantID $PrivateKey
        if ($LASTEXITCODE -ne 0) {
            throw "generate approval policy JWS failed"
        }
        return (($jws | Out-String).Trim())
    } finally {
        Remove-Item $tmp -Force -ErrorAction SilentlyContinue
    }
}

function Convert-MnemonicToHDSeed {
    param([string]$Phrase)

    $tmp = Join-Path $env:TEMP "metapus_hd_seed_$PID.go"
    @'
package main

import (
	"encoding/base64"
	"fmt"
	"os"

	bip39 "github.com/fbsobreira/go-bip39"
)

func main() {
	if len(os.Args) != 2 {
		panic("mnemonic is required")
	}
	fmt.Print(base64.RawURLEncoding.EncodeToString(bip39.NewSeed(os.Args[1], "")))
}
'@ | Set-Content -Path $tmp -Encoding ASCII
    try {
        Push-Location (Join-Path $script:CryptoRepoRoot "vault-sign-plugins\tron")
        $seed = & go run $tmp $Phrase
        if ($LASTEXITCODE -ne 0) {
            throw "convert mnemonic to HD seed failed"
        }
        return (($seed | Out-String).Trim())
    } finally {
        Pop-Location
        Remove-Item $tmp -Force -ErrorAction SilentlyContinue
    }
}

function Get-TenantInfo {
    $slugSql = ConvertTo-CryptoSqlLiteral $TenantSlug
    $row = Invoke-CryptoPsql -Database "tenants" -Sql "SELECT id || '|' || db_name FROM tenants WHERE lower(slug) = lower('$slugSql') LIMIT 1;"
    if ([string]::IsNullOrWhiteSpace($row)) {
        return $null
    }
    $parts = $row -split "\|"
    return [pscustomobject]@{ ID = $parts[0]; DBName = $parts[1] }
}

Invoke-CryptoNative "Start PostgreSQL" { docker compose up -d metapus-db }
Wait-Postgres

$pluginDir = Join-Path $script:CryptoRepoRoot "vault-plugins"
New-Item -ItemType Directory -Force -Path $pluginDir | Out-Null
$pluginDirDocker = $pluginDir.Replace("\", "/")
$vaultLocalConfig = '{"plugin_directory":"/vault/plugins"}'

Invoke-CryptoNative "Build TRON Vault plugin for Linux" {
    Push-Location (Join-Path $script:CryptoRepoRoot "vault-sign-plugins\tron")
    try {
        $oldGoos = $env:GOOS
        $oldGoarch = $env:GOARCH
        $oldCgo = $env:CGO_ENABLED
        $env:GOOS = "linux"
        $env:GOARCH = "amd64"
        $env:CGO_ENABLED = "0"
        go build -o ..\..\vault-plugins\vault-plugin-secrets-tron ./cmd/vault-plugin-secrets-tron
    } finally {
        $env:GOOS = $oldGoos
        $env:GOARCH = $oldGoarch
        $env:CGO_ENABLED = $oldCgo
        Pop-Location
    }
}

if (-not $KeepVault) {
    Write-Host "==> Recreate dev Vault container"
    $existingVault = (& docker ps -a --filter "name=^/metapus-vault$" --format "{{.Names}}")
    if ($existingVault -eq "metapus-vault") {
        Invoke-CryptoNative "Remove existing dev Vault container" { docker rm -f metapus-vault }
    }
}

$vaultRunning = (& docker ps --format "{{.Names}}" | Where-Object { $_ -eq "metapus-vault" })
$vaultExists = (& docker ps -a --format "{{.Names}}" | Where-Object { $_ -eq "metapus-vault" })
if (-not $vaultExists) {
    Invoke-CryptoNative "Start Vault dev container with plugin dir" {
        $oldVaultLocalConfig = $env:VAULT_LOCAL_CONFIG
        try {
            $env:VAULT_LOCAL_CONFIG = $vaultLocalConfig
            docker run -d --name metapus-vault --cap-add IPC_LOCK `
                -p "8200:8200" `
                -v "${pluginDirDocker}:/vault/plugins" `
                -e VAULT_LOCAL_CONFIG `
                hashicorp/vault:2.0.3 server -dev "-dev-root-token-id=$VaultToken" "-dev-listen-address=0.0.0.0:8200"
        } finally {
            $env:VAULT_LOCAL_CONFIG = $oldVaultLocalConfig
        }
    }
} elseif (-not $vaultRunning) {
    Invoke-CryptoNative "Start existing Vault container" { docker start metapus-vault }
}
Wait-Vault

$shaLine = & docker exec metapus-vault sha256sum /vault/plugins/vault-plugin-secrets-tron
if ($LASTEXITCODE -ne 0) {
    throw "failed to calculate Vault plugin checksum"
}
$pluginSha = (($shaLine -join " ") -split "\s+")[0]

Invoke-CryptoNative "Register TRON Vault plugin" {
    docker exec -e "VAULT_ADDR=http://127.0.0.1:8200" -e "VAULT_TOKEN=$VaultToken" metapus-vault `
        vault plugin register "-sha256=$pluginSha" secret vault-plugin-secrets-tron
}

$enableOutput = & docker exec -e "VAULT_ADDR=http://127.0.0.1:8200" -e "VAULT_TOKEN=$VaultToken" metapus-vault `
    vault secrets enable -path=tron -plugin-name=vault-plugin-secrets-tron plugin 2>&1
if ($LASTEXITCODE -ne 0 -and (($enableOutput -join "`n") -notmatch "path is already in use")) {
    throw "enable TRON Vault plugin failed: $($enableOutput -join "`n")"
}
$enableTransitOutput = & docker exec -e "VAULT_ADDR=http://127.0.0.1:8200" -e "VAULT_TOKEN=$VaultToken" metapus-vault `
    vault secrets enable -path=transit transit 2>&1
if ($LASTEXITCODE -ne 0 -and (($enableTransitOutput -join "`n") -notmatch "path is already in use")) {
    throw "enable Vault Transit failed: $($enableTransitOutput -join "`n")"
}

Invoke-CryptoNative "Initialize tenant meta database" { go run ./cmd/crypto-tenant init-meta }

$tenant = Get-TenantInfo
if ($null -eq $tenant) {
    Invoke-CryptoNative "Create tenant '$TenantSlug'" {
        go run ./cmd/crypto-tenant create --slug $TenantSlug --name $TenantName
    }
    $tenant = Get-TenantInfo
}
if ($null -eq $tenant) {
    throw "tenant '$TenantSlug' was not found after setup"
}

Invoke-CryptoNative "Run tenant migrations" {
    go run ./cmd/crypto-tenant migrate --id $tenant.ID
}

$tenantDb = $tenant.DBName
$databaseUrl = "postgres://metapus:metapus@localhost:5432/$tenantDb`?sslmode=disable"
$env:DATABASE_URL = $databaseUrl
$env:TENANT_ID = $tenant.ID
$env:TENANT_DB = $tenantDb

Invoke-CryptoNative "Seed tenant demo data" { go run ./cmd/crypto-seed }

$networkCodeSql = ConvertTo-CryptoSqlLiteral "TRON-NILE"
$networkID = Invoke-CryptoPsql -Database $tenantDb -Sql "SELECT id FROM cat_blockchain_networks WHERE code='$networkCodeSql' AND deletion_mark=FALSE LIMIT 1;"
if ([string]::IsNullOrWhiteSpace($networkID)) {
    throw "TRON-NILE network was not seeded in tenant database '$tenantDb'."
}
$networkIDSql = ConvertTo-CryptoSqlLiteral $networkID

$authKey = Resolve-Ed25519KeyPair -PublicEnvName "CRYPTO_SIGNING_AUTH_PUBLIC_KEY" -PrivateEnvName "CRYPTO_SIGNING_AUTH_PRIVATE_KEY"
$authPublic = $authKey.public
$authPrivate = $authKey.private

$policyKey = New-AuthorizationKeyPair
$approvalPolicyPublic = $policyKey.public
$approvalPolicyBundleJWS = New-ApprovalPolicyBundleJWS -TenantID $tenant.ID -PrivateKey $policyKey.private

$vaultHeaders = @{ "X-Vault-Token" = $VaultToken }
function New-ApprovalTransitPublicKey {
    param([Parameter(Mandatory = $true)][string]$Role)
    $keyName = "metapus-approval-$Role-$($tenant.ID)"
    $createBody = @{ type = "ed25519"; exportable = $false; allow_plaintext_backup = $false; auto_rotate_period = 0 } | ConvertTo-Json -Compress
    Invoke-RestMethod -Method Post -Uri "$VaultAddr/v1/transit/keys/$keyName" -Headers $vaultHeaders -ContentType "application/json" -Body $createBody | Out-Null
    $configBody = @{ deletion_allowed = $false } | ConvertTo-Json -Compress
    Invoke-RestMethod -Method Post -Uri "$VaultAddr/v1/transit/keys/$keyName/config" -Headers $vaultHeaders -ContentType "application/json" -Body $configBody | Out-Null
    $metadata = Invoke-RestMethod -Method Get -Uri "$VaultAddr/v1/transit/keys/$keyName" -Headers $vaultHeaders
    $version = [string]$metadata.data.latest_version
    $publicKey = $metadata.data.keys.$version.public_key
    if ($metadata.data.type -ne "ed25519" -or $metadata.data.exportable -or $metadata.data.deletion_allowed -or [string]::IsNullOrWhiteSpace($publicKey)) {
        throw "approval Transit key '$keyName' violates key custody policy"
    }
    return $publicKey
}
$intentPublic = New-ApprovalTransitPublicKey -Role "intent"
$riskAssessmentPublic = New-ApprovalTransitPublicKey -Role "risk-assessment"
$policyDecisionPublic = New-ApprovalTransitPublicKey -Role "policy-decision"
$adminApprovalPublic = New-ApprovalTransitPublicKey -Role "admin-approval"
$riskApprovalPublic = New-ApprovalTransitPublicKey -Role "risk-approval"

$env:CRYPTO_SIGNING_AUTH_PUBLIC_KEY = $authPublic
$env:CRYPTO_SIGNING_AUTH_PRIVATE_KEY = $authPrivate
$authorizationKID = "transit/metapus-signing-auth-$($tenant.ID)/v1"
$env:CRYPTO_AUTH_SIGNER = "local"
$env:CRYPTO_LOCAL_AUTH_KID = $authorizationKID
$env:CRYPTO_APPROVAL_POLICY_PUBLIC_KEY = $approvalPolicyPublic
$env:CRYPTO_APPROVAL_POLICY_BUNDLE_JWS = $approvalPolicyBundleJWS
$env:CRYPTO_INTENT_SIGNING_PUBLIC_KEY = $intentPublic
$env:CRYPTO_RISK_ASSESSMENT_SIGNING_PUBLIC_KEY = $riskAssessmentPublic
$env:CRYPTO_POLICY_DECISION_SIGNING_PUBLIC_KEY = $policyDecisionPublic
$env:CRYPTO_ADMIN_APPROVAL_SIGNING_PUBLIC_KEY = $adminApprovalPublic
$env:CRYPTO_RISK_APPROVAL_SIGNING_PUBLIC_KEY = $riskApprovalPublic

$hdSeed = Convert-MnemonicToHDSeed -Phrase $Mnemonic
$configBody = @{
    schema_version = "tron_tenant_signing_config.v3"
    authorization_keyring = @{ $authorizationKID = $authPublic }
    policy_public_key = $approvalPolicyPublic
    intent_public_key = $intentPublic
    risk_assessment_public_key = $riskAssessmentPublic
    policy_decision_public_key = $policyDecisionPublic
    admin_approval_public_key = $adminApprovalPublic
    risk_approval_public_key = $riskApprovalPublic
    hd_seed = $hdSeed
} | ConvertTo-Json -Compress
Invoke-RestMethod -Method Post `
    -Uri "$VaultAddr/v1/tron/tenants/$($tenant.ID)/config/authorization" `
    -Headers $vaultHeaders -ContentType "application/json" -Body $configBody | Out-Null

$wallets = @()
$poolWalletCodes = @()
for ($i = $PoolCount; $i -ge 1; $i--) {
    if ($PoolAccount -eq 0) {
        $poolCode = "POOL-TRON-{0:D3}" -f $i
    } else {
        $poolCode = "POOL-TRON-A{0:D3}-{1:D3}" -f $PoolAccount, $i
    }
    $poolWalletCodes += $poolCode
    $wallets += @{ Code = $poolCode; Path = "m/44'/195'/$PoolAccount'/0/$i"; IsHot = $false; IsFee = $false; Role = "merchant_pool" }
}
$wallets += @{ Code = "HOT-TRON-001"; Path = "m/44'/195'/$PoolAccount'/0/0"; IsHot = $true; IsFee = $false; Role = "liquidity_hot" }
$wallets += @{ Code = "FEE-TRON-001"; Path = "m/44'/195'/$PoolAccount'/0/2147483647"; IsHot = $false; IsFee = $true; Role = "network_fee" }
$hotWalletAddress = ""
$feeWalletAddress = ""

foreach ($wallet in $wallets) {
    $deriveBody = @{ network = "tron_nile"; derivation_path = $wallet.Path } | ConvertTo-Json -Compress
    $derived = Invoke-RestMethod -Method Post `
        -Uri "$VaultAddr/v1/tron/tenants/$($tenant.ID)/wallets/derive" `
        -Headers $vaultHeaders -ContentType "application/json" -Body $deriveBody

    $codeSql = ConvertTo-CryptoSqlLiteral $wallet.Code
    $pathSql = ConvertTo-CryptoSqlLiteral $wallet.Path
    $addressSql = ConvertTo-CryptoSqlLiteral $derived.data.address
    $vaultKeySql = ConvertTo-CryptoSqlLiteral $derived.data.vault_key_id
    $roleSql = ConvertTo-CryptoSqlLiteral $wallet.Role
    if ($wallet.IsHot) {
        $hotWalletAddress = $derived.data.address
    }
    if ($wallet.IsFee) {
        $feeWalletAddress = $derived.data.address
    }

    $existingWallet = Invoke-CryptoPsql -Database $tenantDb -Sql "SELECT id || '|' || address || '|' || derivation_path FROM cat_wallets WHERE code='$codeSql' AND deletion_mark=FALSE LIMIT 1;"
    if (-not [string]::IsNullOrWhiteSpace($existingWallet)) {
        $existingParts = $existingWallet -split "\|", 3
        $walletIDSql = ConvertTo-CryptoSqlLiteral $existingParts[0]
        $identityChanged = $existingParts[1] -ne $derived.data.address -or $existingParts[2] -ne $wallet.Path
        if ($identityChanged) {
            $historyCount = Invoke-CryptoPsql -Database $tenantDb -Sql "SELECT CASE WHEN EXISTS (SELECT 1 FROM reg_crypto_balance_movements WHERE wallet_id='$walletIDSql') OR EXISTS (SELECT 1 FROM reg_crypto_balance_balances WHERE wallet_id='$walletIDSql' AND amount <> 0) OR EXISTS (SELECT 1 FROM doc_crypto_invoices WHERE wallet_id='$walletIDSql') OR EXISTS (SELECT 1 FROM reg_crypto_sweep_attempts WHERE wallet_id='$walletIDSql' OR hot_wallet_id='$walletIDSql') OR EXISTS (SELECT 1 FROM doc_crypto_withdrawals WHERE source_wallet_id='$walletIDSql') OR EXISTS (SELECT 1 FROM doc_treasury_withdrawals WHERE source_wallet_id='$walletIDSql') OR EXISTS (SELECT 1 FROM reg_crypto_account_activations WHERE wallet_id='$walletIDSql') OR EXISTS (SELECT 1 FROM doc_crypto_network_operations WHERE target_wallet_id='$walletIDSql' OR source_wallet_id='$walletIDSql') OR EXISTS (SELECT 1 FROM reg_crypto_fee_reservations WHERE payer_wallet_id='$walletIDSql') OR EXISTS (SELECT 1 FROM doc_crypto_native_fundings WHERE source_wallet_id='$walletIDSql' OR destination_wallet_id='$walletIDSql') THEN 1 ELSE 0 END;"
            if ([int]$historyCount -gt 0) {
                throw "Wallet '$($wallet.Code)' has financial history and cannot be rebound from '$($existingParts[1])' to '$($derived.data.address)'. Select a new -PoolAccount generation."
            }
        }

        if ($wallet.IsHot) {
            Invoke-CryptoPsql -Database $tenantDb -Sql "UPDATE cat_wallets SET network_id='$networkIDSql', address='$addressSql', vault_key_id='$vaultKeySql', derivation_path='$pathSql', tier='hot', wallet_role='$roleSql', status='free', merchant_id=NULL, leased_for_id=NULL, leased_until=NULL, chain_activation_status='active', updated_at=NOW(), version=version+1 WHERE id='$walletIDSql'; UPDATE reg_crypto_wallet_allocation_holds SET status='resolved',resolved_at=NOW(),resolved_by='crypto-setup',resolution='wallet_reconfigured_as_hot' WHERE wallet_id='$walletIDSql' AND owner_type='wallet_reconciliation' AND owner_id='$walletIDSql' AND status='open';" | Out-Null
        } elseif ($wallet.IsFee) {
            Invoke-CryptoPsql -Database $tenantDb -Sql "UPDATE cat_wallets SET network_id='$networkIDSql', address='$addressSql', vault_key_id='$vaultKeySql', derivation_path='$pathSql', tier='hot', wallet_role='$roleSql', status='free', merchant_id=NULL, leased_for_id=NULL, leased_until=NULL, chain_activation_status=CASE WHEN address IS DISTINCT FROM '$addressSql' THEN 'unknown' ELSE chain_activation_status END, updated_at=NOW(), version=version+1 WHERE id='$walletIDSql'; UPDATE reg_crypto_wallet_allocation_holds SET status='resolved',resolved_at=NOW(),resolved_by='crypto-setup',resolution='wallet_reconfigured_as_fee' WHERE wallet_id='$walletIDSql' AND owner_type='wallet_reconciliation' AND owner_id='$walletIDSql' AND status='open';" | Out-Null
        } else {
            Invoke-CryptoPsql -Database $tenantDb -Sql "UPDATE cat_wallets SET network_id='$networkIDSql', address='$addressSql', vault_key_id='$vaultKeySql', derivation_path='$pathSql', tier='pool', wallet_role='$roleSql', chain_activation_status=CASE WHEN address IS DISTINCT FROM '$addressSql' THEN 'unknown' ELSE chain_activation_status END, updated_at=NOW(), version=version+1 WHERE id='$walletIDSql'; INSERT INTO reg_crypto_wallet_allocation_holds(wallet_id,reason,owner_type,owner_id,details,held_by) VALUES('$walletIDSql','chain_balance_check_pending','wallet_reconciliation','$walletIDSql',jsonb_build_object('poolAccount',$PoolAccount,'derivationPath','$pathSql'),'crypto-setup') ON CONFLICT (wallet_id,owner_type,owner_id) WHERE status='open' DO UPDATE SET details=EXCLUDED.details;" | Out-Null
        }
    } else {
        if ($wallet.IsHot) {
            Invoke-CryptoPsql -Database $tenantDb -Sql "INSERT INTO cat_wallets (code,name,network_id,address,derivation_path,vault_key_id,tier,wallet_role,status,allocation_mode,is_active,chain_activation_status) VALUES ('$codeSql','TRON Liquidity Hot Wallet','$networkIDSql','$addressSql','$pathSql','$vaultKeySql','hot','$roleSql','free','transient',TRUE,'active');" | Out-Null
        } elseif ($wallet.IsFee) {
            Invoke-CryptoPsql -Database $tenantDb -Sql "INSERT INTO cat_wallets (code,name,network_id,address,derivation_path,vault_key_id,tier,wallet_role,status,allocation_mode,is_active,chain_activation_status) VALUES ('$codeSql','TRON Network Fee Wallet','$networkIDSql','$addressSql','$pathSql','$vaultKeySql','hot','$roleSql','free','transient',TRUE,'unknown');" | Out-Null
        } else {
            Invoke-CryptoPsql -Database $tenantDb -Sql "WITH inserted AS (INSERT INTO cat_wallets (code,name,network_id,address,derivation_path,vault_key_id,tier,wallet_role,status,allocation_mode,is_active,chain_activation_status) VALUES ('$codeSql','TRON Pool Wallet (account $PoolAccount)','$networkIDSql','$addressSql','$pathSql','$vaultKeySql','pool','$roleSql','free','transient',TRUE,'unknown') RETURNING id) INSERT INTO reg_crypto_wallet_allocation_holds(wallet_id,reason,owner_type,owner_id,details,held_by) SELECT id,'chain_balance_check_pending','wallet_reconciliation',id::text,jsonb_build_object('poolAccount',$PoolAccount,'derivationPath','$pathSql'),'crypto-setup' FROM inserted;" | Out-Null
        }
    }
}

if ([string]::IsNullOrWhiteSpace($hotWalletAddress)) {
    throw "Derived hot wallet address is missing."
}
if ([string]::IsNullOrWhiteSpace($feeWalletAddress)) {
    throw "Derived network fee wallet address is missing."
}
$sweepPolicyBody = @{
    schema_version = "tron_sweep_destinations.v1"
    version = "local-dev-v1"
    destinations = @{ tron_nile = @($hotWalletAddress) }
} | ConvertTo-Json -Depth 5 -Compress
Invoke-RestMethod -Method Post `
    -Uri "$VaultAddr/v1/tron/tenants/$($tenant.ID)/config/sweep-destinations" `
    -Headers $vaultHeaders -ContentType "application/json" -Body $sweepPolicyBody | Out-Null

$usdtNetworkCode = Invoke-CryptoPsql -Database $tenantDb -Sql "SELECT n.code FROM cat_tokens t JOIN cat_blockchain_networks n ON n.id=t.network_id WHERE t.code='USDT-TRC20' AND t.deletion_mark=FALSE LIMIT 1;"
if ($usdtNetworkCode -ne "TRON-NILE") {
    throw "USDT-TRC20 is linked to '$usdtNetworkCode', expected TRON-NILE."
}

$nativeTokenCount = Invoke-CryptoPsql -Database $tenantDb -Sql "SELECT COUNT(*) FROM cat_tokens WHERE network_id='$networkIDSql' AND token_standard='native' AND is_active=TRUE AND deletion_mark=FALSE;"
if ([int]$nativeTokenCount -lt 1) {
    throw "TRON-NILE active native token was not seeded."
}

$wrongWalletNetworks = Invoke-CryptoPsql -Database $tenantDb -Sql "SELECT w.code || ':' || n.code FROM cat_wallets w JOIN cat_blockchain_networks n ON n.id=w.network_id WHERE (w.code IN ('HOT-TRON-001','FEE-TRON-001') OR w.code LIKE 'POOL-TRON-%') AND w.deletion_mark=FALSE AND n.code <> 'TRON-NILE' ORDER BY w.code;"
if (-not [string]::IsNullOrWhiteSpace($wrongWalletNetworks)) {
    throw "TRON dev wallets are linked to a non-Nile network: $wrongWalletNetworks"
}

# Fresh tenant seeds may contain demonstrational pool placeholders without a
# Vault key. They must never coexist as allocatable wallets with the selected
# derivation generation. Retire only placeholders that have no financial
# history; anything else is an operator-visible hard stop.
$unsafePoolPlaceholders = Invoke-CryptoPsql -Database $tenantDb -Sql "SELECT w.code FROM cat_wallets w WHERE w.network_id='$networkIDSql' AND w.tier='pool' AND w.vault_key_id='' AND w.is_active=TRUE AND w.deletion_mark=FALSE AND (EXISTS (SELECT 1 FROM reg_crypto_balance_movements m WHERE m.wallet_id=w.id) OR EXISTS (SELECT 1 FROM reg_crypto_balance_balances b WHERE b.wallet_id=w.id AND b.amount<>0) OR EXISTS (SELECT 1 FROM doc_crypto_invoices i WHERE i.wallet_id=w.id) OR EXISTS (SELECT 1 FROM reg_crypto_sweep_attempts s WHERE s.wallet_id=w.id OR s.hot_wallet_id=w.id) OR EXISTS (SELECT 1 FROM reg_crypto_account_activations a WHERE a.wallet_id=w.id) OR EXISTS (SELECT 1 FROM doc_crypto_network_operations o WHERE o.target_wallet_id=w.id OR o.source_wallet_id=w.id)) ORDER BY w.code;"
if (-not [string]::IsNullOrWhiteSpace($unsafePoolPlaceholders)) {
    throw "Unconfigured pool placeholders have financial history and cannot be retired: $unsafePoolPlaceholders"
}
Invoke-CryptoPsql -Database $tenantDb -Sql "UPDATE reg_crypto_reconciliation_items i SET status='resolved',resolved_at=NOW(),resolution_reason='retired_unconfigured_seed_placeholder',updated_at=NOW() FROM cat_wallets w WHERE i.wallet_id=w.id AND w.network_id='$networkIDSql' AND w.tier='pool' AND w.vault_key_id='' AND w.is_active=TRUE AND w.deletion_mark=FALSE AND i.status IN ('open','acknowledged');" | Out-Null
Invoke-CryptoPsql -Database $tenantDb -Sql "UPDATE cat_wallets SET is_active=FALSE,deletion_mark=TRUE,updated_at=NOW(),version=version+1 WHERE network_id='$networkIDSql' AND tier='pool' AND vault_key_id='' AND is_active=TRUE AND deletion_mark=FALSE;" | Out-Null

Invoke-CryptoNative "Run crypto pool on-chain preflight" {
    go run ./cmd/crypto-preflight/main.go
}

$poolCodeSqlList = ($poolWalletCodes | ForEach-Object { "'$(ConvertTo-CryptoSqlLiteral $_)'" }) -join ","
$heldGeneration = Invoke-CryptoPsql -Database $tenantDb -Sql "SELECT w.code || ':' || string_agg(h.reason,',' ORDER BY h.reason) FROM cat_wallets w JOIN reg_crypto_wallet_allocation_holds h ON h.wallet_id=w.id AND h.status='open' WHERE w.code IN ($poolCodeSqlList) GROUP BY w.id,w.code ORDER BY w.code;"
if (-not [string]::IsNullOrWhiteSpace($heldGeneration)) {
    throw "Pool account generation $PoolAccount is not clean and cannot be allocated: $heldGeneration. Re-run setup with a fresh -PoolAccount value."
}

$poolNamespace = "m/44'/195'/$PoolAccount'/0"
$poolNamespaceSql = ConvertTo-CryptoSqlLiteral $poolNamespace
$conflictingGeneration = Invoke-CryptoPsql -Database $tenantDb -Sql "SELECT code || ':' || derivation_path FROM cat_wallets WHERE network_id='$networkIDSql' AND tier='pool' AND is_active=TRUE AND deletion_mark=FALSE AND derivation_path NOT LIKE '$poolNamespaceSql/%' ORDER BY code;"
if (-not [string]::IsNullOrWhiteSpace($conflictingGeneration)) {
    throw "Active pool wallets span multiple derivation generations: $conflictingGeneration"
}
$nextPoolIndex = Invoke-CryptoPsql -Database $tenantDb -Sql "SELECT COALESCE(MAX(substring(derivation_path FROM '/([0-9]+)$')::BIGINT)+1,0) FROM cat_wallets WHERE network_id='$networkIDSql' AND tier='pool' AND is_active=TRUE AND deletion_mark=FALSE AND derivation_path LIKE '$poolNamespaceSql/%';"
Invoke-CryptoPsql -Database $tenantDb -Sql "INSERT INTO sys_crypto_wallet_derivation_cursors(network_id,derivation_namespace,next_index,needs_review) VALUES('$networkIDSql','$poolNamespaceSql',$nextPoolIndex,FALSE) ON CONFLICT(network_id) DO UPDATE SET derivation_namespace=EXCLUDED.derivation_namespace,next_index=GREATEST(sys_crypto_wallet_derivation_cursors.next_index,EXCLUDED.next_index),needs_review=FALSE,updated_at=NOW(),version=sys_crypto_wallet_derivation_cursors.version+1;" | Out-Null
Invoke-CryptoPsql -Database $tenantDb -Sql "UPDATE sys_crypto_wallet_pool_policies SET derivation_namespace='$poolNamespaceSql',needs_review=FALSE,last_error_code='',last_error_message='',updated_by=CASE WHEN updated_by='' THEN 'crypto-setup' ELSE updated_by END,updated_at=NOW(),version=version+1 WHERE network_id='$networkIDSql';" | Out-Null

$withdrawalSql = ConvertTo-CryptoSqlLiteral $WithdrawalAddress
$managedWithdrawalCode = Invoke-CryptoPsql -Database $tenantDb -Sql "SELECT code FROM cat_wallets WHERE address='$withdrawalSql' AND deletion_mark=FALSE LIMIT 1;"
if (-not [string]::IsNullOrWhiteSpace($managedWithdrawalCode)) {
    Write-Host "Withdrawal test address belongs to managed wallet '$managedWithdrawalCode'; using default external Nile destination."
    $WithdrawalAddress = $defaultWithdrawalTestAddress
}

$fundingAddress1 = $env:TRON_TEST_FUNDING_ADDRESS_1
if ([string]::IsNullOrWhiteSpace($fundingAddress1)) {
    $fundingAddress1 = $defaultFundingAddress1
}
$fundingAddress2 = $env:TRON_TEST_FUNDING_ADDRESS_2
if ([string]::IsNullOrWhiteSpace($fundingAddress2)) {
    $fundingAddress2 = $defaultFundingAddress2
}

$merchantID = Invoke-CryptoPsql -Database $tenantDb -Sql "SELECT id FROM cat_merchants WHERE code='MERCHANT-001' AND deletion_mark=FALSE LIMIT 1;"
if ([string]::IsNullOrWhiteSpace($merchantID)) {
    throw "MERCHANT-001 was not seeded in tenant database '$tenantDb'."
}
$merchantOwnerLinkCount = Invoke-CryptoPsql -Database $tenantDb -Sql "WITH linked AS (INSERT INTO sys_merchant_users(user_id,merchant_id,role) SELECT u.id,'$(ConvertTo-CryptoSqlLiteral $merchantID)',1 FROM auth_users u WHERE u.email='admin@metapus.io' AND u.deletion_mark=FALSE ON CONFLICT(user_id,merchant_id) DO UPDATE SET role=EXCLUDED.role,updated_at=NOW() RETURNING 1) SELECT COUNT(*) FROM linked;"
if ([int]$merchantOwnerLinkCount -ne 1) {
    throw "admin@metapus.io was not seeded or could not be linked to MERCHANT-001."
}
$tokenID = Invoke-CryptoPsql -Database $tenantDb -Sql "SELECT id FROM cat_tokens WHERE code='USDT-TRC20' AND deletion_mark=FALSE LIMIT 1;"
if ([string]::IsNullOrWhiteSpace($tokenID)) {
    throw "USDT-TRC20 was not seeded in tenant database '$tenantDb'."
}
$tokenIDSql = ConvertTo-CryptoSqlLiteral $tokenID
# Keep the Nile acceptance cycle deterministic: one 1 USDT invoice must be
# eligible for sweep immediately. Transient pool wallets are shared and use
# the token-level policy rather than a merchant-specific wallet override.
Invoke-CryptoPsql -Database $tenantDb -Sql "UPDATE cat_tokens SET sweep_threshold=1000000,sweep_max_age_hours=1,sweep_retain_amount=1,updated_at=NOW(),version=version+1 WHERE id='$tokenIDSql';" | Out-Null

$envValues = @{
    APP_ENV = "development"
    APP_PORT = "8080"
    PUBLIC_CHECKOUT_BASE_URL = "http://127.0.0.1:8080"
    LOG_LEVEL = "debug"
    JWT_SECRET = "dev-secret-32-chars-change-me-123456"
    AUTH_MFA_ENCRYPTION_KEY = "dGVzdC1tZmEtZW5jcnlwdGlvbi1rZXktMzItYnl0ZXM="
    ADMIN_PASSWORD = "Admin123!"
    ADMIN_MFA_MODE = "required"
    ADMIN_MFA_GRACE_PERIOD_HOURS = "0"
    ADMIN_STEP_UP_TTL_SECONDS = "300"
    ADMIN_SECURITY_MODE = "enforce"
    ADMIN_ALLOWED_CIDRS = "127.0.0.1/32,::1/128"
    TRUSTED_PROXIES = ""
    AUTOMATION_ENCRYPTION_KEY = "test-encryption-key-32chars!!!!!"
    AUTOMATION_TG_BOT_TOKEN = if ($env:AUTOMATION_TG_BOT_TOKEN) { $env:AUTOMATION_TG_BOT_TOKEN } else { "" }
    AUTOMATION_TG_CHAT_ID = if ($env:AUTOMATION_TG_CHAT_ID) { $env:AUTOMATION_TG_CHAT_ID } else { "" }
    META_DATABASE_URL = $metaDatabaseUrl
    POSTGRES_ADMIN_URL = $postgresAdminUrl
    TENANT_DB_USER = "metapus"
    TENANT_DB_PASSWORD = "metapus"
    TENANT_DB_HOST = "localhost"
    TENANT_DB_PORT = "5432"
    TENANT_SLUG = $TenantSlug
    TENANT_ID = $tenant.ID
    TENANT_DB = $tenantDb
    DATABASE_URL = $databaseUrl
    SEED_DEMO_DATA = "true"
    SEED_CRYPTO_HISTORY = "false"
    CRYPTO_PROCESSING_ENABLED = "true"
    CRYPTO_CHAIN_CONFIG_FILE = $chainConfigPath
    TRON_TEST_RPC_URL = "https://nile.trongrid.io"
    TRON_TEST_API_KEY = $TronApiKey
    TRON_DEV_POOL_ACCOUNT = $PoolAccount.ToString()
    TRON_WITHDRAWAL_TEST_ADDRESS = $WithdrawalAddress
    TRON_TEST_FUNDING_ADDRESS_1 = $fundingAddress1
    TRON_TEST_FUNDING_ADDRESS_2 = $fundingAddress2
    VAULT_ADDR = $VaultAddr
    VAULT_TOKEN = $VaultToken
    CRYPTO_AUTH_SIGNER = "local"
    CRYPTO_LOCAL_AUTH_KID = $authorizationKID
    CRYPTO_SIGNING_AUTH_PUBLIC_KEY = $authPublic
    CRYPTO_SIGNING_AUTH_PRIVATE_KEY = $authPrivate
    CRYPTO_APPROVAL_POLICY_PUBLIC_KEY = $approvalPolicyPublic
    CRYPTO_APPROVAL_POLICY_BUNDLE_JWS = $approvalPolicyBundleJWS
    CRYPTO_INTENT_SIGNING_PUBLIC_KEY = $intentPublic
    CRYPTO_RISK_ASSESSMENT_SIGNING_PUBLIC_KEY = $riskAssessmentPublic
    CRYPTO_POLICY_DECISION_SIGNING_PUBLIC_KEY = $policyDecisionPublic
    CRYPTO_ADMIN_APPROVAL_SIGNING_PUBLIC_KEY = $adminApprovalPublic
    CRYPTO_RISK_APPROVAL_SIGNING_PUBLIC_KEY = $riskApprovalPublic
    MERCHANT_ID = $merchantID
    TOKEN_ID = $tokenID
    NETWORK_ID = $networkID
    CRYPTO_RESOURCE_LEASE_TARGET_ENERGY = "65000"
    CRYPTO_RESOURCE_SAFETY_MARGIN_PCT = "10"
    CRYPTO_RESOURCE_RESERVATION_CUTOFF_SECONDS = "120"
    MERCHANT_API_KEY = ""
}
if (-not $NoPersistMnemonic) {
    $envValues["TRON_DEV_MNEMONIC"] = $Mnemonic
}
Write-CryptoEnvFile -Values $envValues -RemoveKeys @("TRON_RPC_URL", "TRON_API_KEY")

$frontendEnvPath = Join-Path $script:CryptoRepoRoot "frontend\.env.local"
$frontendEnvLines = if (Test-Path $frontendEnvPath) { @(Get-Content $frontendEnvPath) } else { @() }
$tenantEnvLine = "NEXT_PUBLIC_TENANT_ID=$($tenant.ID)"
$tenantEnvUpdated = $false
$frontendEnvLines = @($frontendEnvLines | ForEach-Object {
    if ($_ -match '^NEXT_PUBLIC_TENANT_ID=') {
        $tenantEnvUpdated = $true
        $tenantEnvLine
    } else {
        $_
    }
})
if (-not $tenantEnvUpdated) {
    $frontendEnvLines += $tenantEnvLine
}
Set-Content -Path $frontendEnvPath -Value $frontendEnvLines -Encoding UTF8

Write-Host ""
Write-Host "Crypto dev environment is ready."
Write-Host "Tenant ID: $($tenant.ID)"
Write-Host "Tenant DB: $tenantDb"
Write-Host "Env file: $script:CryptoEnvPath"
Write-Host "After the first real iTRX balance observation, post the audited opening with:"
Write-Host "  .\scripts\dev\crypto-post-provider-opening.ps1"
Write-Host ""
Write-Host "Wallets:"
& docker exec metapus-db psql -U metapus -d $tenantDb -c "SELECT code,tier,wallet_role,status,chain_activation_status,address,vault_key_id FROM cat_wallets WHERE code LIKE '%TRON%' ORDER BY code;"
