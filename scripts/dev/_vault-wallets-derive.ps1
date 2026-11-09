param(
    [int]$PoolCount = 6,
    [int]$PoolAccount = -1
)

. (Join-Path $PSScriptRoot "_crypto-env.ps1")

Set-Location $script:CryptoRepoRoot
Import-CryptoEnv | Out-Null

$tenantID = Get-CryptoRequiredEnv "TENANT_ID"
$vaultAddr = Get-CryptoRequiredEnv "VAULT_ADDR"
$vaultToken = Get-CryptoRequiredEnv "VAULT_TOKEN"

if ($PoolAccount -lt 0) {
    $configuredPoolAccount = Get-CryptoRequiredEnv "TRON_DEV_POOL_ACCOUNT"
    if (-not [int]::TryParse($configuredPoolAccount, [ref]$PoolAccount) -or $PoolAccount -lt 0) {
        throw "TRON_DEV_POOL_ACCOUNT must be a non-negative integer."
    }
}
if ($PoolCount -le 0) {
    throw "PoolCount must be positive."
}

$wallets = @()
for ($i = $PoolCount; $i -ge 1; $i--) {
    if ($PoolAccount -eq 0) {
        $code = "POOL-TRON-{0:D3}" -f $i
    } else {
        $code = "POOL-TRON-A{0:D3}-{1:D3}" -f $PoolAccount, $i
    }
    $wallets += @{ Code = $code; Path = "m/44'/195'/$PoolAccount'/0/$i"; Role = "merchant_pool" }
}
$wallets += @{ Code = "HOT-TRON-001"; Path = "m/44'/195'/$PoolAccount'/0/0"; Role = "liquidity_hot" }
$wallets += @{ Code = "FEE-TRON-001"; Path = "m/44'/195'/$PoolAccount'/0/2147483647"; Role = "network_fee" }

$vaultHeaders = @{ "X-Vault-Token" = $vaultToken }
$hotWalletAddress = ""
foreach ($wallet in $wallets) {
    $deriveBody = @{ network = "tron_nile"; derivation_path = $wallet.Path } | ConvertTo-Json -Compress
    $derived = Invoke-RestMethod -Method Post `
        -Uri "$vaultAddr/v1/tron/tenants/$tenantID/wallets/derive" `
        -Headers $vaultHeaders -ContentType "application/json" -Body $deriveBody
    if ($wallet.Role -eq "liquidity_hot") {
        $hotWalletAddress = $derived.data.address
    }
    Write-Host "Derived $($wallet.Code) -> $($derived.data.address)"
}

if ([string]::IsNullOrWhiteSpace($hotWalletAddress)) {
    throw "Derived hot wallet address is missing."
}
$sweepPolicyBody = @{
    schema_version = "tron_sweep_destinations.v1"
    version = "local-dev-v1"
    destinations = @{ tron_nile = @($hotWalletAddress) }
} | ConvertTo-Json -Depth 5 -Compress
Invoke-RestMethod -Method Post `
    -Uri "$vaultAddr/v1/tron/tenants/$tenantID/config/sweep-destinations" `
    -Headers $vaultHeaders -ContentType "application/json" -Body $sweepPolicyBody | Out-Null

$readiness = Invoke-RestMethod -Method Get `
    -Uri "$vaultAddr/v1/tron/tenants/$tenantID/config/readiness" `
    -Headers $vaultHeaders
if (-not $readiness.data.hd_seed_configured `
    -or -not $readiness.data.authorization_keyring_configured `
    -or -not $readiness.data.approval_public_keys_configured `
    -or $readiness.data.sweep_policy_networks -notcontains "tron_nile") {
    throw "Vault TRON signer is not ready after wallet rehydration."
}
Write-Host "Vault TRON signer rehydrated for pool account $PoolAccount."
