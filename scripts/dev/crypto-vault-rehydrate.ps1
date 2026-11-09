param(
    [int]$PoolCount = 6,
    [int]$PoolAccount = -1
)

$ErrorActionPreference = "Stop"

. (Join-Path $PSScriptRoot "_crypto-env.ps1")

Set-Location $script:CryptoRepoRoot
Import-CryptoEnv | Out-Null
Assert-CryptoDockerReady

$vaultAddr = Get-CryptoRequiredEnv "VAULT_ADDR"
$vaultToken = Get-CryptoRequiredEnv "VAULT_TOKEN"

$running = & docker ps --filter "name=^/metapus-vault$" --format "{{.Names}}"
if (($running | Out-String).Trim() -ne "metapus-vault") {
    Invoke-CryptoNative "Start existing Vault container" { docker start metapus-vault }
}

for ($attempt = 0; $attempt -lt 30; $attempt++) {
    try {
        $health = Invoke-RestMethod -Uri "$vaultAddr/v1/sys/health" -TimeoutSec 2
        if ($health.initialized -and -not $health.sealed) {
            break
        }
    } catch {
        if ($attempt -eq 29) { throw }
    }
    Start-Sleep -Seconds 1
}

$shaLine = & docker exec metapus-vault sha256sum /vault/plugins/vault-plugin-secrets-tron
if ($LASTEXITCODE -ne 0) {
    throw "failed to calculate Vault plugin checksum"
}
$pluginSha = (($shaLine -join " ") -split "\s+")[0]

Invoke-CryptoNative "Register TRON Vault plugin" {
    docker exec -e "VAULT_ADDR=http://127.0.0.1:8200" -e "VAULT_TOKEN=$vaultToken" metapus-vault `
        vault plugin register "-sha256=$pluginSha" secret vault-plugin-secrets-tron
}

$previousErrorAction = $ErrorActionPreference
$ErrorActionPreference = "Continue"
$enableOutput = & docker exec -e "VAULT_ADDR=http://127.0.0.1:8200" -e "VAULT_TOKEN=$vaultToken" metapus-vault `
    vault secrets enable -path=tron -plugin-name=vault-plugin-secrets-tron plugin 2>&1
$enableExitCode = $LASTEXITCODE
$ErrorActionPreference = $previousErrorAction
if ($enableExitCode -ne 0 -and (($enableOutput -join "`n") -notmatch "path is already in use")) {
    throw "enable TRON Vault plugin failed: $($enableOutput -join "`n")"
}

& (Join-Path $PSScriptRoot "_vault-tenant-config.ps1") | Out-Null
& (Join-Path $PSScriptRoot "_vault-wallets-derive.ps1") -PoolCount $PoolCount -PoolAccount $PoolAccount | Out-Null

Write-Host "Vault TRON plugin and tenant wallets rehydrated."
