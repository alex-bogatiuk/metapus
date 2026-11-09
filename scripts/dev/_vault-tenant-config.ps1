. (Join-Path $PSScriptRoot "_crypto-env.ps1")

Set-Location $script:CryptoRepoRoot
Import-CryptoEnv | Out-Null

$tenantID = Get-CryptoRequiredEnv "TENANT_ID"
$vaultAddr = Get-CryptoRequiredEnv "VAULT_ADDR"
$vaultToken = Get-CryptoRequiredEnv "VAULT_TOKEN"
$authPublic = Get-CryptoRequiredEnv "CRYPTO_SIGNING_AUTH_PUBLIC_KEY"
$authorizationKID = Get-CryptoRequiredEnv "CRYPTO_LOCAL_AUTH_KID"
$policyPublic = Get-CryptoRequiredEnv "CRYPTO_APPROVAL_POLICY_PUBLIC_KEY"
$intentPublic = Get-CryptoRequiredEnv "CRYPTO_INTENT_SIGNING_PUBLIC_KEY"
$riskAssessmentPublic = Get-CryptoRequiredEnv "CRYPTO_RISK_ASSESSMENT_SIGNING_PUBLIC_KEY"
$policyDecisionPublic = Get-CryptoRequiredEnv "CRYPTO_POLICY_DECISION_SIGNING_PUBLIC_KEY"
$adminApprovalPublic = Get-CryptoRequiredEnv "CRYPTO_ADMIN_APPROVAL_SIGNING_PUBLIC_KEY"
$riskApprovalPublic = Get-CryptoRequiredEnv "CRYPTO_RISK_APPROVAL_SIGNING_PUBLIC_KEY"
$mnemonic = Get-CryptoRequiredEnv "TRON_DEV_MNEMONIC"

$tmp = Join-Path $env:TEMP "metapus_hd_seed.go"
@'
package main

import (
	"encoding/base64"
	"fmt"
	"os"

	bip39 "github.com/fbsobreira/go-bip39"
)

func main() {
	fmt.Print(base64.RawURLEncoding.EncodeToString(bip39.NewSeed(os.Args[1], "")))
}
'@ | Set-Content -Path $tmp -Encoding ASCII

Push-Location (Join-Path $script:CryptoRepoRoot "vault-sign-plugins\tron")
$hdSeed = & go run $tmp $mnemonic 2>$null
if ($LASTEXITCODE -ne 0) { throw "convert mnemonic to HD seed failed" }
$hdSeed = ($hdSeed | Out-String).Trim()
Pop-Location
Remove-Item $tmp -Force -ErrorAction SilentlyContinue

Write-Host "Configuring Vault tenant $tenantID..."
$body = @{
    schema_version = "tron_tenant_signing_config.v3"
    authorization_keyring = @{ $authorizationKID = $authPublic }
    policy_public_key = $policyPublic
    intent_public_key = $intentPublic
    risk_assessment_public_key = $riskAssessmentPublic
    policy_decision_public_key = $policyDecisionPublic
    admin_approval_public_key = $adminApprovalPublic
    risk_approval_public_key = $riskApprovalPublic
    hd_seed = $hdSeed
} | ConvertTo-Json -Compress
$resp = Invoke-RestMethod -Method Post `
    -Uri "$vaultAddr/v1/tron/tenants/$tenantID/config/authorization" `
    -Headers @{ "X-Vault-Token" = $vaultToken } `
    -ContentType "application/json" -Body $body
Write-Host "Vault tenant configured successfully."

# Verify
$check = Invoke-RestMethod -Method Get `
    -Uri "$vaultAddr/v1/tron/tenants/$tenantID/config/authorization" `
    -Headers @{ "X-Vault-Token" = $vaultToken }
$check.data | ConvertTo-Json
