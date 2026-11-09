param(
    [string]$AmountMinorUnits = "11000000",
    [string]$Currency = "USDT-TRC20",
    [int]$TTLMinutes = 60,
    [string]$OrderId = ""
)

$ErrorActionPreference = "Stop"

. (Join-Path $PSScriptRoot "_crypto-env.ps1")

Set-Location $script:CryptoRepoRoot
Import-CryptoEnv | Out-Null
Assert-CryptoDockerReady

if ([string]::IsNullOrWhiteSpace($OrderId)) {
    $OrderId = "manual-$([guid]::NewGuid())"
}

if ($AmountMinorUnits -notmatch '^(0|[1-9][0-9]*)$' -or $AmountMinorUnits -eq "0") {
    throw "AmountMinorUnits must be a canonical positive integer string."
}

$apiKey = $env:MERCHANT_API_KEY
if ([string]::IsNullOrWhiteSpace($apiKey)) {
    throw "MERCHANT_API_KEY is required. Create a scoped key through Merchant Portal with MFA and set it in .env.crypto.local before running this script."
}

$headers = @{
    "X-Api-Key" = $apiKey
    "Idempotency-Key" = "invoice-$OrderId"
}
$invoice = Invoke-RestMethod -Method Post `
    -Uri "http://127.0.0.1:8080/merchant/v1/invoices" `
    -Headers $headers -ContentType "application/json" `
    -Body (@{
        amount = $AmountMinorUnits
        currency = $Currency
        ttlMinutes = $TTLMinutes
        orderId = $OrderId
    } | ConvertTo-Json -Compress)

$invoice | ConvertTo-Json -Depth 8
Write-Host ""
Write-Host "Open checkout page: $($invoice.payUrl)"
Write-Host "Send testnet funds to walletAddress: $($invoice.walletAddress)"
