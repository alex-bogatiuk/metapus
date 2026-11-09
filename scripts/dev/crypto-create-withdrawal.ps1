param(
    [string]$ToAddress = "",
    [string]$Amount = "1.000000",
    [string]$TokenCode = "USDT-TRC20",
    [string]$ApiBaseUrl = "http://127.0.0.1:8080",
    [string]$AdminPassword = "",
    [switch]$NoApprove,
    [switch]$AllowManagedDestination
)

. (Join-Path $PSScriptRoot "_crypto-env.ps1")

Set-Location $script:CryptoRepoRoot
Import-CryptoEnv | Out-Null
Assert-CryptoDockerReady

$defaultWithdrawalTestAddress = "TQXx1eKQtVFjoDvyiGc3cRmJkSWE9KeXni"
$explicitDestination = -not [string]::IsNullOrWhiteSpace($ToAddress)
$tenantID = Get-CryptoRequiredEnv "TENANT_ID"
$tenantDb = Get-CryptoRequiredEnv "TENANT_DB"
if ([string]::IsNullOrWhiteSpace($AdminPassword)) {
    $AdminPassword = Get-CryptoRequiredEnv "ADMIN_PASSWORD"
}

if ([string]::IsNullOrWhiteSpace($ToAddress)) {
    $ToAddress = $env:TRON_WITHDRAWAL_TEST_ADDRESS
}
if ([string]::IsNullOrWhiteSpace($ToAddress)) {
    $ToAddress = $defaultWithdrawalTestAddress
}

$toSql = ConvertTo-CryptoSqlLiteral $ToAddress
$managedCode = Invoke-CryptoPsql -Database $tenantDb -Sql "SELECT code FROM cat_wallets WHERE address='$toSql' AND deletion_mark=FALSE LIMIT 1;"
if (-not [string]::IsNullOrWhiteSpace($managedCode) -and -not $explicitDestination -and -not $AllowManagedDestination) {
    Write-Host "Configured withdrawal test address belongs to managed wallet '$managedCode'; using default external Nile destination."
    $ToAddress = $defaultWithdrawalTestAddress
    $toSql = ConvertTo-CryptoSqlLiteral $ToAddress
    $managedCode = Invoke-CryptoPsql -Database $tenantDb -Sql "SELECT code FROM cat_wallets WHERE address='$toSql' AND deletion_mark=FALSE LIMIT 1;"
}
if (-not [string]::IsNullOrWhiteSpace($managedCode) -and -not $AllowManagedDestination) {
    throw "Destination address belongs to managed wallet '$managedCode'. Pass -AllowManagedDestination only if this is intentional."
}

$merchantID = $env:MERCHANT_ID
if ([string]::IsNullOrWhiteSpace($merchantID)) {
    $merchantID = Invoke-CryptoPsql -Database $tenantDb -Sql "SELECT id FROM cat_merchants WHERE code='MERCHANT-001' AND deletion_mark=FALSE LIMIT 1;"
}
if ([string]::IsNullOrWhiteSpace($merchantID)) {
    throw "MERCHANT-001 not found. Run scripts/dev/crypto-setup.ps1 first."
}

$tokenSql = ConvertTo-CryptoSqlLiteral $TokenCode
$tokenRow = Invoke-CryptoPsql -Database $tenantDb -Sql "SELECT id || '|' || network_id FROM cat_tokens WHERE code='$tokenSql' AND deletion_mark=FALSE LIMIT 1;"
if ([string]::IsNullOrWhiteSpace($tokenRow)) {
    throw "Token '$TokenCode' not found."
}
$tokenParts = $tokenRow -split "\|"
$tokenID = $tokenParts[0]
$networkID = $tokenParts[1]

$adminUserID = Invoke-CryptoPsql -Database $tenantDb -Sql "SELECT id FROM auth_users WHERE email='admin@metapus.io' AND deletion_mark=FALSE LIMIT 1;"
Invoke-CryptoPsql -Database $tenantDb -Sql "INSERT INTO sys_merchant_users(user_id, merchant_id, role) VALUES ('$adminUserID', '$merchantID', 1) ON CONFLICT (user_id, merchant_id) DO UPDATE SET role=1;" | Out-Null

$login = Invoke-RestMethod -Method Post `
    -Uri "$ApiBaseUrl/api/v1/auth/login" `
    -Headers @{ "X-Tenant-ID" = $tenantID } -ContentType "application/json" `
    -Body (@{ email = "admin@metapus.io"; password = $AdminPassword } | ConvertTo-Json -Compress)

$adminToken = $login.tokens.accessToken
$authHeaders = @{ "Authorization" = "Bearer $adminToken"; "X-Tenant-ID" = $tenantID }

$merchantSql = ConvertTo-CryptoSqlLiteral $merchantID
$networkSql = ConvertTo-CryptoSqlLiteral $networkID
$addressID = Invoke-CryptoPsql -Database $tenantDb -Sql "SELECT id FROM reg_withdrawal_addresses WHERE merchant_id='$merchantSql' AND network_id='$networkSql' AND address='$toSql' AND _deleted_at IS NULL LIMIT 1;"
if ([string]::IsNullOrWhiteSpace($addressID)) {
    $addr = Invoke-RestMethod -Method Post `
        -Uri "$ApiBaseUrl/portal/v1/withdrawal-addresses?merchant_id=$merchantID" `
        -Headers $authHeaders -ContentType "application/json" `
        -Body (@{ networkId = $networkID; address = $ToAddress; label = "Manual dev test" } | ConvertTo-Json -Compress)
    $addressID = $addr.id
}

$wr = Invoke-RestMethod -Method Post `
    -Uri "$ApiBaseUrl/portal/v1/withdrawal-requests?merchant_id=$merchantID" `
    -Headers $authHeaders -ContentType "application/json" `
    -Body (@{ tokenId = $tokenID; amount = $Amount; addressId = $addressID } | ConvertTo-Json -Compress)

if (-not $NoApprove) {
    Invoke-RestMethod -Method Post `
        -Uri "$ApiBaseUrl/api/v1/document/withdrawal-request/$($wr.id)/approve" `
        -Headers $authHeaders | Out-Null
}

Write-CryptoEnvFile -Values @{ MERCHANT_ID = $merchantID; TOKEN_ID = $tokenID; NETWORK_ID = $networkID }

[pscustomobject]@{
    requestId = $wr.id
    status = if ($NoApprove) { "pending_approval" } else { "approved" }
    destination = $ToAddress
    amount = $Amount
} | ConvertTo-Json -Depth 5
