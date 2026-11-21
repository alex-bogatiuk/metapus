. (Join-Path $PSScriptRoot "_crypto-env.ps1")

Set-Location $script:CryptoRepoRoot
Import-CryptoEnv | Out-Null

$env:APP_ENV = if ($env:APP_ENV) { $env:APP_ENV } else { "development" }
$env:JWT_SECRET = if ($env:JWT_SECRET) { $env:JWT_SECRET } else { "dev-secret-32-chars-change-me-123456" }

Get-CryptoRequiredEnv "META_DATABASE_URL" | Out-Null
Get-CryptoRequiredEnv "PUBLIC_CHECKOUT_BASE_URL" | Out-Null
Get-CryptoRequiredEnv "TENANT_DB_USER" | Out-Null
Get-CryptoRequiredEnv "TENANT_DB_PASSWORD" | Out-Null
Get-CryptoRequiredEnv "CRYPTO_APPROVAL_POLICY_PUBLIC_KEY" | Out-Null
Get-CryptoRequiredEnv "CRYPTO_APPROVAL_POLICY_BUNDLE_JWS" | Out-Null
Get-CryptoRequiredEnv "VAULT_ADDR" | Out-Null

go run ./cmd/crypto-server
