[CmdletBinding()]
param(
    [string]$StateDir = "",
    [string]$PublicIP = "176.215.182.82",
    [string]$AdminPassword = "Admin123!",
    [string]$TenantSlug = "dual-e2e",
    [string]$TenantName = "Dual E2E Stand"
)

$ErrorActionPreference = "Stop"
$PSNativeCommandUseErrorActionPreference = $false

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
Set-Location $RepoRoot

if (-not $StateDir) {
    $StateDir = Join-Path $env:USERPROFILE (".metapus\public-test-" + (Get-Date -Format "yyyyMMdd-HHmmss"))
}
if ((Test-Path $StateDir) -and @(Get-ChildItem -Force $StateDir).Count -gt 0) {
    throw "State directory is not empty: $StateDir. Use a fresh directory or the existing-stand procedure in docs/operations/public-nile-deployment.md."
}

$Version = "checkout-" + (Get-Date -Format "yyyyMMdd")
$GitVersion = (& git rev-parse --short=12 HEAD 2>$null)
if ($LASTEXITCODE -eq 0 -and $GitVersion) { $Version = $GitVersion.Trim() }

& go run ./cmd/public-test-bootstrap init `
    --state-dir $StateDir `
    --public-ip $PublicIP `
    --version $Version `
    --network both `
    --admin-password $AdminPassword
if ($LASTEXITCODE -ne 0) { throw "Public test state initialization failed" }

$EnvFile = Join-Path $StateDir "public-test.env"
$Compose = @(
    '--project-name', 'metapus-public-test',
    '--env-file', $EnvFile,
    '-f', 'single-tenant-deployment\docker-compose.yml',
    '-f', 'single-tenant-deployment\docker-compose.public-test.yml'
)

function Invoke-Compose {
    param([Parameter(ValueFromRemainingArguments = $true)][string[]]$Arguments)
    & docker compose @Compose @Arguments
    if ($LASTEXITCODE -ne 0) { throw "docker compose failed: $($Arguments -join ' ')" }
}

Invoke-Compose config --quiet
Invoke-Compose --profile init --profile operator build init seed public-test-bootstrap crypto-preflight
Invoke-Compose up -d postgres
Invoke-Compose --profile init run --rm init init-meta

$Created = (Invoke-Compose --profile init run --rm init create --slug $TenantSlug --name $TenantName) | Out-String
if ($Created -notmatch 'Tenant ID:\s+([a-f0-9-]+)') { throw "Tenant ID was not returned" }
$TenantID = $Matches[1]
if ($Created -notmatch 'Database:\s+(mt_[a-f0-9]+)') { throw "Tenant database was not returned" }
$TenantDB = $Matches[1]

Invoke-Compose --profile operator run --rm --no-deps public-test-bootstrap finalize --state-dir /state --tenant-id $TenantID --tenant-db $TenantDB
Invoke-Compose --profile operator run --rm runtime-secret-permissions
Invoke-Compose --profile init run --rm seed

Invoke-Compose --profile vault-test up -d --build vault-test
Invoke-Compose --profile operator run --rm --no-deps public-test-bootstrap vault-init --state-dir /state --vault-addr https://vault-test:8200

$TronHashOutput = (Invoke-Compose --profile vault-test exec -T vault-test sha256sum /vault/plugins/vault-plugin-secrets-tron) | Out-String
$EVMHashOutput = (Invoke-Compose --profile vault-test exec -T vault-test sha256sum /vault/plugins/vault-plugin-secrets-evm) | Out-String
$TronHash = ($TronHashOutput -split '\s+')[0]
$EVMHash = ($EVMHashOutput -split '\s+')[0]
$PoolAccount = ((Get-Content $EnvFile | Select-String '^POOL_ACCOUNT=(\d+)$').Matches.Groups[1].Value)
if (-not $PoolAccount -or $TronHash.Length -ne 64 -or $EVMHash.Length -ne 64) {
    throw "Pool account or Vault plugin checksums are missing"
}

Invoke-Compose --profile operator run --rm --no-deps public-test-bootstrap vault-bootstrap `
    --state-dir /state `
    --tenant-id $TenantID `
    --pool-account $PoolAccount `
    --family both `
    --plugin-sha256 $TronHash `
    --evm-plugin-sha256 $EVMHash `
    --proxy-cidr 172.30.251.10/32 `
    --vault-addr https://vault-test:8200

# The wrapped SecretID expires after 120 seconds. Start Proxy immediately.
Invoke-Compose --profile operator run --rm vault-bootstrap-permissions
Invoke-Compose --profile vault-test up -d vault-proxy
Invoke-Compose --profile operator run --rm --no-deps public-test-bootstrap configure-db --state-dir /state

# Compose assigns a fixed IP to preflight; run the networks one at a time.
Invoke-Compose --profile operator run --rm --no-deps crypto-preflight
Invoke-Compose --profile operator run --rm --no-deps -e CRYPTO_NETWORK_CODE=ETHEREUM-SEPOLIA -e CRYPTO_PREFLIGHT_NETWORK_CODE=ETHEREUM-SEPOLIA crypto-preflight

Invoke-Compose build metapus-app metapus-worker frontend
Invoke-Compose up -d metapus-app frontend
Invoke-Compose --profile vault-test up -d metapus-worker

Write-Host "Both custody families and RPC topologies are configured for tenant $TenantID."
Write-Host "Wallet addresses: $StateDir\wallets-tron.json and $StateDir\wallets-evm.json"
Write-Host "Admin email: admin@metapus.io. Password is in $StateDir\secrets\admin_password."
Write-Host "Fund native hot and fee wallets, record confirmed funding receipts, then enable network policies through Admin UI with MFA step-up."
Write-Host "Complete the public edge and HTTPS steps in docs/operations/public-nile-deployment.md before remote login."
