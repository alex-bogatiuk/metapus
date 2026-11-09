[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$EnvFile,

    [string]$PublicIP = "176.215.182.82"
)

$ErrorActionPreference = "Stop"
$deployDir = Split-Path -Parent $PSScriptRoot
$envPath = [IO.Path]::GetFullPath($EnvFile)
if (-not (Test-Path -LiteralPath $envPath -PathType Leaf)) {
    throw "Public test env file does not exist: $envPath"
}

$composeArgs = @(
    "compose",
    "--env-file", $envPath,
    "--project-name", "metapus-public-test",
    "-f", (Join-Path $deployDir "docker-compose.yml"),
    "-f", (Join-Path $deployDir "docker-compose.public-test.yml")
)

function Invoke-Compose {
    & docker @composeArgs @args
    if ($LASTEXITCODE -ne 0) {
        throw "docker compose failed with exit code $LASTEXITCODE"
    }
}

function Set-EnvFileValue {
    param([string]$Name, [string]$Value)

    $lines = [Collections.Generic.List[string]]::new()
    $found = $false
    foreach ($line in [IO.File]::ReadAllLines($envPath)) {
        if ($line -match "^$([Regex]::Escape($Name))=") {
            $lines.Add("$Name=$Value")
            $found = $true
        } else {
            $lines.Add($line)
        }
    }
    if (-not $found) {
        $lines.Add("$Name=$Value")
    }
    [IO.File]::WriteAllLines($envPath, $lines, [Text.UTF8Encoding]::new($false))
}

$env:PUBLIC_IP = $PublicIP
$env:NGINX_MODE = "acme"

$versionOutput = (Invoke-Compose --profile tls-tools run --rm certbot --version | Out-String).Trim()
if ($versionOutput -notmatch "certbot\s+(\d+)\.(\d+)") {
    throw "Could not parse Certbot version: $versionOutput"
}
if (([int]$Matches[1] -lt 5) -or (([int]$Matches[1] -eq 5) -and ([int]$Matches[2] -lt 4))) {
    throw "Certbot 5.4 or newer is required, got $versionOutput"
}

Invoke-Compose up -d reverse-proxy

Invoke-Compose --profile tls-tools run --rm certbot certonly `
    --staging `
    --non-interactive `
    --agree-tos `
    --register-unsafely-without-email `
    --preferred-profile shortlived `
    --webroot `
    --webroot-path /var/www/certbot `
    --ip-address $PublicIP `
    --cert-name "$PublicIP-staging"

Invoke-Compose --profile tls-tools run --rm certbot certonly `
    --non-interactive `
    --agree-tos `
    --register-unsafely-without-email `
    --preferred-profile shortlived `
    --webroot `
    --webroot-path /var/www/certbot `
    --ip-address $PublicIP `
    --cert-name $PublicIP

$env:NGINX_MODE = "https"
Set-EnvFileValue -Name "NGINX_MODE" -Value "https"
Invoke-Compose up -d --force-recreate reverse-proxy

Invoke-Compose --profile tls-tools run --rm certbot renew `
    --dry-run `
    --no-random-sleep-on-renew `
    --run-deploy-hooks `
    --cert-name $PublicIP `
    --preferred-profile shortlived `
    --webroot `
    --webroot-path /var/www/certbot `
    --deploy-hook "touch /etc/letsencrypt/.metapus-reload"

Invoke-Compose --profile tls up -d certbot-renew

& curl.exe --fail --silent --show-error `
    --resolve "${PublicIP}:443:127.0.0.1" `
    "https://$PublicIP/health/live"
if ($LASTEXITCODE -ne 0) {
    throw "Local trusted HTTPS verification failed"
}

Invoke-Compose --profile tls-tools run --rm certbot certificates --cert-name $PublicIP
Write-Output "Trusted IP certificate, HTTPS proxy, renewal dry-run, and 12-hour renewal loop are ready."
