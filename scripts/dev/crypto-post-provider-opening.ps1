param(
    [string]$IncidentId = "",
    [string]$ApiBaseUrl = "http://127.0.0.1:8080",
    [string]$AdminPassword = ""
)

$ErrorActionPreference = "Stop"

. (Join-Path $PSScriptRoot "_crypto-env.ps1")

Set-Location $script:CryptoRepoRoot
Import-CryptoEnv | Out-Null
Assert-CryptoDockerReady

$tenantID = Get-CryptoRequiredEnv "TENANT_ID"
if ([string]::IsNullOrWhiteSpace($AdminPassword)) {
    $AdminPassword = Get-CryptoRequiredEnv "ADMIN_PASSWORD"
}
$login = Invoke-RestMethod -Method Post `
    -Uri "$ApiBaseUrl/api/v1/auth/login" `
    -Headers @{ "X-Tenant-ID" = $tenantID } `
    -ContentType "application/json" `
    -Body (@{ email = "admin@metapus.io"; password = $AdminPassword } | ConvertTo-Json -Compress)

$headers = @{
    "Authorization" = "Bearer $($login.tokens.accessToken)"
    "X-Tenant-ID" = $tenantID
}

if ([string]::IsNullOrWhiteSpace($IncidentId)) {
    $incidents = Invoke-RestMethod -Method Get `
        -Uri "$ApiBaseUrl/api/v1/document/crypto-resource-rental/provider-balance-incidents" `
        -Headers $headers
    $positive = @($incidents.items | Where-Object { [int64]$_.deltaAmount -gt 0 })
    if ($positive.Count -eq 0) {
        throw "No active positive provider balance incident was found. Run the worker until it records an iTRX balance observation."
    }
    if ($positive.Count -gt 1) {
        $ids = ($positive | ForEach-Object { $_.id }) -join ", "
        throw "Multiple positive incidents found. Pass -IncidentId explicitly. IDs: $ids"
    }
    $IncidentId = $positive[0].id
}

$prepared = Invoke-RestMethod -Method Post `
    -Uri "$ApiBaseUrl/api/v1/document/crypto-provider-balance-operation/provider-balance-incidents/$IncidentId/prepare-opening" `
    -Headers $headers -ContentType "application/json" -Body "{}"

Invoke-RestMethod -Method Post `
    -Uri "$ApiBaseUrl/api/v1/document/crypto-provider-balance-operation/$($prepared.id)/approve" `
    -Headers $headers -ContentType "application/json" -Body "{}" | Out-Null

$operation = Invoke-RestMethod -Method Get `
    -Uri "$ApiBaseUrl/api/v1/document/crypto-provider-balance-operation/$($prepared.id)" `
    -Headers $headers

$operation | ConvertTo-Json -Depth 8
