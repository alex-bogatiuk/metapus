. (Join-Path $PSScriptRoot "_crypto-env.ps1")

Set-Location $script:CryptoRepoRoot
Import-CryptoEnv | Out-Null

$env:APP_ENV = if ($env:APP_ENV) { $env:APP_ENV } else { "development" }

Get-CryptoRequiredEnv "META_DATABASE_URL" | Out-Null
Get-CryptoRequiredEnv "TENANT_DB_USER" | Out-Null
Get-CryptoRequiredEnv "TENANT_DB_PASSWORD" | Out-Null
Get-CryptoRequiredEnv "VAULT_ADDR" | Out-Null
Get-CryptoRequiredEnv "VAULT_TOKEN" | Out-Null
Get-CryptoRequiredEnv "CRYPTO_AUTH_SIGNER" | Out-Null
Get-CryptoRequiredEnv "CRYPTO_LOCAL_AUTH_KID" | Out-Null
Get-CryptoRequiredEnv "CRYPTO_SIGNING_AUTH_PRIVATE_KEY" | Out-Null
Get-CryptoRequiredEnv "CRYPTO_APPROVAL_POLICY_PUBLIC_KEY" | Out-Null
Get-CryptoRequiredEnv "CRYPTO_INTENT_SIGNING_PUBLIC_KEY" | Out-Null
Get-CryptoRequiredEnv "CRYPTO_RISK_ASSESSMENT_SIGNING_PUBLIC_KEY" | Out-Null
Get-CryptoRequiredEnv "CRYPTO_POLICY_DECISION_SIGNING_PUBLIC_KEY" | Out-Null
Get-CryptoRequiredEnv "CRYPTO_ADMIN_APPROVAL_SIGNING_PUBLIC_KEY" | Out-Null
Get-CryptoRequiredEnv "CRYPTO_RISK_APPROVAL_SIGNING_PUBLIC_KEY" | Out-Null
Get-CryptoRequiredEnv "CRYPTO_PROCESSING_ENABLED" | Out-Null
Get-CryptoRequiredEnv "CRYPTO_CHAIN_CONFIG_FILE" | Out-Null

go run ./cmd/crypto-worker
