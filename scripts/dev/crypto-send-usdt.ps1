param(
    [string]$FromWalletCode = "HOT-TRON-001",
    [string]$ToAddress,
    [string]$Amount = "1.000000",
    [string]$TokenCode = "USDT-TRC20"
)

$ErrorActionPreference = "Stop"

. (Join-Path $PSScriptRoot "_crypto-env.ps1")

function Invoke-CryptoTransferParameter {
    param(
        [string]$ToAddress,
        [int64]$AmountMinor
    )

    $tmp = Join-Path $env:TEMP "metapus_tron_param_$PID.go"
    @'
package main

import (
	"fmt"
	"math/big"
	"os"
	"strings"

	"github.com/fbsobreira/gotron-sdk/pkg/address"
)

func main() {
	if len(os.Args) != 3 {
		panic("usage: helper <to-address> <amount-minor>")
	}
	to, err := address.Base58ToAddress(os.Args[1])
	if err != nil {
		panic(err)
	}
	amount, ok := new(big.Int).SetString(os.Args[2], 10)
	if !ok {
		panic("invalid amount")
	}
	toHex := strings.TrimPrefix(strings.ToLower(to.Hex()), "0x")
	toHex = strings.TrimPrefix(toHex, "41")
	amountHex := amount.Text(16)
	if len(amountHex) > 64 {
		panic("amount is too large")
	}
	fmt.Print(strings.Repeat("0", 24) + toHex + strings.Repeat("0", 64-len(amountHex)) + amountHex)
}
'@ | Set-Content -Path $tmp -Encoding ASCII
    try {
        Push-Location (Join-Path $script:CryptoRepoRoot "vault-sign-plugins\tron")
        $result = & go run $tmp $ToAddress $AmountMinor 2>$null
        $value = (($result | Out-String).Trim())
        if ($LASTEXITCODE -ne 0 -and [string]::IsNullOrWhiteSpace($value)) {
            throw "encode transfer parameter failed"
        }
        return $value
    } finally {
        Pop-Location
        Remove-Item $tmp -Force -ErrorAction SilentlyContinue
    }
}

function Invoke-CryptoSignJWS {
    param(
        [string]$PrivateKey,
        [string]$PayloadJson
    )

    $tmp = Join-Path $env:TEMP "metapus_jws_$PID.go"
    @'
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) != 3 {
		panic("usage: helper <private-key> <payload-base64url>")
	}
	key, err := base64.RawURLEncoding.DecodeString(os.Args[1])
	if err != nil {
		key, err = base64.StdEncoding.DecodeString(os.Args[1])
	}
	if err != nil {
		panic(err)
	}
	var private ed25519.PrivateKey
	switch len(key) {
	case ed25519.SeedSize:
		private = ed25519.NewKeyFromSeed(key)
	case ed25519.PrivateKeySize:
		private = ed25519.PrivateKey(key)
	default:
		panic("invalid ed25519 private key length")
	}
	header, err := json.Marshal(map[string]string{"alg": "EdDSA", "typ": "JWT"})
	if err != nil {
		panic(err)
	}
	encodedHeader := base64.RawURLEncoding.EncodeToString(header)
	encodedPayload := os.Args[2]
	signingInput := encodedHeader + "." + encodedPayload
	signature := ed25519.Sign(private, []byte(signingInput))
	fmt.Print(signingInput + "." + base64.RawURLEncoding.EncodeToString(signature))
}
'@ | Set-Content -Path $tmp -Encoding ASCII
    try {
        $payloadBase64 = [Convert]::ToBase64String([System.Text.Encoding]::UTF8.GetBytes($PayloadJson)).TrimEnd("=").Replace("+", "-").Replace("/", "_")
        $result = & go run $tmp $PrivateKey $payloadBase64 2>$null
        $value = (($result | Out-String).Trim())
        if ($LASTEXITCODE -ne 0 -and [string]::IsNullOrWhiteSpace($value)) {
            throw "sign JWS failed"
        }
        return $value
    } finally {
        Remove-Item $tmp -Force -ErrorAction SilentlyContinue
    }
}

function New-CryptoUUID {
    return [guid]::NewGuid().ToString()
}

Set-Location $script:CryptoRepoRoot
Import-CryptoEnv | Out-Null
Assert-CryptoDockerReady

$tenantID = Get-CryptoRequiredEnv "TENANT_ID"
$tenantDb = Get-CryptoRequiredEnv "TENANT_DB"
$vaultAddr = Get-CryptoRequiredEnv "VAULT_ADDR"
$vaultToken = Get-CryptoRequiredEnv "VAULT_TOKEN"
$authPrivate = Get-CryptoRequiredEnv "CRYPTO_SIGNING_AUTH_PRIVATE_KEY"
$tronRpcUrl = Get-CryptoRequiredEnv "TRON_TEST_RPC_URL"

if ([string]::IsNullOrWhiteSpace($ToAddress)) {
    throw "ToAddress is required."
}

$walletCodeSql = ConvertTo-CryptoSqlLiteral $FromWalletCode
$tokenCodeSql = ConvertTo-CryptoSqlLiteral $TokenCode
$walletRow = Invoke-CryptoPsql -Database $tenantDb -Sql "SELECT id || '|' || address || '|' || vault_key_id FROM cat_wallets WHERE code='$walletCodeSql' AND deletion_mark=FALSE LIMIT 1;"
if ([string]::IsNullOrWhiteSpace($walletRow)) {
    throw "Wallet '$FromWalletCode' not found."
}
$walletParts = $walletRow -split "\|"
$sourceAddress = $walletParts[1]
$vaultKeyID = $walletParts[2]
if ([string]::IsNullOrWhiteSpace($vaultKeyID)) {
    throw "Wallet '$FromWalletCode' has empty vault_key_id. Run crypto-setup.ps1 first."
}

$tokenRow = Invoke-CryptoPsql -Database $tenantDb -Sql "SELECT contract_address || '|' || decimal_places || '|' || token_standard FROM cat_tokens WHERE code='$tokenCodeSql' AND deletion_mark=FALSE LIMIT 1;"
if ([string]::IsNullOrWhiteSpace($tokenRow)) {
    throw "Token '$TokenCode' not found."
}
$tokenParts = $tokenRow -split "\|"
$contractAddress = $tokenParts[0]
$decimals = [int]$tokenParts[1]
$tokenStandard = $tokenParts[2]
if ($tokenStandard -ne "TRC-20" -and $tokenStandard -ne "native") {
    throw "Only TRC-20 and native token transfers are supported by this helper."
}

$culture = [System.Globalization.CultureInfo]::InvariantCulture
$amountDecimal = [decimal]::Parse($Amount, $culture)
$scale = [decimal][Math]::Pow(10, $decimals)
$amountMinor = [int64][decimal]::Round($amountDecimal * $scale, 0)
if ($amountMinor -le 0) {
    throw "Amount must be positive."
}

$headers = @{}
if (-not [string]::IsNullOrWhiteSpace($env:TRON_TEST_API_KEY)) {
    $headers["TRON-PRO-API-KEY"] = $env:TRON_TEST_API_KEY
}

if ($tokenStandard -eq "TRC-20") {
    $parameter = Invoke-CryptoTransferParameter -ToAddress $ToAddress -AmountMinor $amountMinor
    $triggerBody = @{
        owner_address = $sourceAddress
        contract_address = $contractAddress
        function_selector = "transfer(address,uint256)"
        parameter = $parameter
        fee_limit = 100000000
        visible = $true
    } | ConvertTo-Json -Compress

    $unsignedResp = Invoke-RestMethod -Method Post `
        -Uri "$tronRpcUrl/wallet/triggersmartcontract" `
        -Headers $headers -ContentType "application/json" -Body $triggerBody
    $unsignedTx = $unsignedResp.transaction
} else {
    $transferBody = @{
        owner_address = $sourceAddress
        to_address = $ToAddress
        amount = $amountMinor
        visible = $true
    } | ConvertTo-Json -Compress
    $unsignedTx = Invoke-RestMethod -Method Post `
        -Uri "$tronRpcUrl/wallet/createtransaction" `
        -Headers $headers -ContentType "application/json" -Body $transferBody
}

if ($null -eq $unsignedTx -or [string]::IsNullOrWhiteSpace($unsignedTx.txID)) {
    throw "TRON node did not return a transaction: $($unsignedTx | ConvertTo-Json -Depth 10)"
}

$basisID = New-CryptoUUID
$authID = New-CryptoUUID
$expires = (Get-Date).ToUniversalTime().AddMinutes(10).ToString("yyyy-MM-ddTHH:mm:ss.fffffffZ", $culture)
$auth = [ordered]@{
    authorization_id = $authID
    tenant_id = $tenantID
    purpose = "sweep"
    network_code = "tron_nile"
    source_address = $sourceAddress
    vault_key_id = $vaultKeyID
    destination_address = $ToAddress
    token_standard = $tokenStandard
    contract_address = $contractAddress
    amount_minor_units = $amountMinor
    fee_limit_sun = if ($tokenStandard -eq "TRC-20") { 100000000 } else { 0 }
    basis_type = "ManualDevTransfer"
    basis_id = $basisID
    attempt = 1
    expires_at = $expires
    policy_version = "tron-v1"
}
$authJson = $auth | ConvertTo-Json -Compress
$authorizationJWS = Invoke-CryptoSignJWS -PrivateKey $authPrivate -PayloadJson $authJson

$vaultBody = @{
    authorization_jws = $authorizationJWS
    unsigned_tx = $unsignedTx
} | ConvertTo-Json -Depth 20 -Compress

$signedResp = Invoke-RestMethod -Method Post `
    -Uri "$vaultAddr/v1/tron/tenants/$tenantID/tx/sign" `
    -Headers @{ "X-Vault-Token" = $vaultToken } `
    -ContentType "application/json" -Body $vaultBody

$signedTx = $signedResp.data.signed_tx
$broadcast = Invoke-RestMethod -Method Post `
    -Uri "$tronRpcUrl/wallet/broadcasttransaction" `
    -Headers $headers -ContentType "application/json" `
    -Body ($signedTx | ConvertTo-Json -Depth 20 -Compress)

[pscustomobject]@{
    result = $broadcast.result
    code = $broadcast.code
    message = $broadcast.message
    txID = if ($broadcast.txid) { $broadcast.txid } else { $signedTx.txID }
    from = $sourceAddress
    to = $ToAddress
    amount = $Amount
    amountMinor = $amountMinor
} | ConvertTo-Json -Depth 5
