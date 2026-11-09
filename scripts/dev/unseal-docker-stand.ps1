[CmdletBinding()]
param(
    [string]$StateDir = ""
)

$ErrorActionPreference = "Continue"
$PSNativeCommandUseErrorActionPreference = $false

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
Set-Location $RepoRoot

Write-Host "=== Восстановление тестового стенда Metapus (Unseal & Recovery) ===" -ForegroundColor Cyan

# 1. Поиск активной папки состояния
if (-not $StateDir) {
    $candidates = Get-ChildItem -Path (Join-Path $env:USERPROFILE ".metapus") -Directory -Filter "public-test-*-dual" | Sort-Object LastWriteTime -Descending
    if ($candidates.Count -gt 0) {
        $StateDir = $candidates[0].FullName
    }
}

if (-not $StateDir -or -not (Test-Path $StateDir)) {
    throw "Не найдена директория состояния стенда. Укажите параметр -StateDir <путь>."
}

$EnvFile = Join-Path $StateDir "public-test.env"
if (-not (Test-Path $EnvFile)) {
    throw "Файл окружения не найден: $EnvFile"
}

Write-Host "Используется состояние: $StateDir" -ForegroundColor DarkGray

$Compose = @(
  '--project-name', 'metapus-public-test',
  '--env-file', $EnvFile,
  '-f', 'single-tenant-deployment\docker-compose.yml',
  '-f', 'single-tenant-deployment\docker-compose.public-test.yml'
)

# 2. Убеждаемся, что базовые сервисы запущены
Write-Host "1/4. Проверка работы контейнеров..." -ForegroundColor Cyan
docker compose @Compose up -d postgres vault-test

# Ожидание готовности Vault
Write-Host "Ожидание ответа Vault..." -ForegroundColor DarkGray
Start-Sleep -Seconds 2

# 3. Разблокировка Vault (Unseal)
Write-Host "2/4. Разблокировка хранилища HashiCorp Vault (Unseal)..." -ForegroundColor Cyan
$unsealRes = docker compose @Compose --profile operator run --rm --no-deps public-test-bootstrap vault-unseal --state-dir /state --vault-addr https://vault-test:8200 2>&1
if ($LASTEXITCODE -ne 0) {
    throw "Ошибка разблокировки Vault: $($unsealRes | Out-String)"
}
Write-Host "Vault успешно распечатан." -ForegroundColor Green

# 4. Выпуск нового SecretID для Vault Proxy
Write-Host "3/4. Обновление одноразового SecretID для Tokenless Vault Proxy..." -ForegroundColor Cyan
$wrapRes = docker compose @Compose --profile operator run --rm --no-deps public-test-bootstrap vault-wrap-secret-id --state-dir /state --vault-addr https://vault-test:8200 2>&1
if ($LASTEXITCODE -ne 0) {
    throw "Ошибка выпуска SecretID: $($wrapRes | Out-String)"
}
docker compose @Compose --profile operator run --rm vault-bootstrap-permissions | Out-Null

# 5. Перезапуск прокси, бэкенда и воркера
Write-Host "4/4. Перезапуск Vault Proxy, API-сервера и воркера..." -ForegroundColor Cyan
docker compose @Compose restart vault-proxy metapus-app metapus-worker | Out-Null

# 6. Проверка здоровья API
Write-Host "Ожидание готовности API-сервера..." -ForegroundColor DarkGray
$isHealthy = $false
for ($i = 1; $i -le 15; $i++) {
    Start-Sleep -Seconds 2
    $appStatus = docker inspect --format '{{.State.Health.Status}}' metapus-public-test-metapus-app-1 2>$null
    if ($appStatus -eq "healthy") {
        $isHealthy = $true
        break
    }
}

if ($isHealthy) {
    Write-Host "`n========================================================" -ForegroundColor Green
    Write-Host " Тестовый стенд успешно восстановлен!" -ForegroundColor Green
    Write-Host "========================================================" -ForegroundColor Green
    Write-Host " URL:       http://localhost:3000/login" -ForegroundColor White
    Write-Host " Email:     admin@metapus.io" -ForegroundColor Yellow
    Write-Host " Пароль:    Admin123!" -ForegroundColor Yellow
    Write-Host "========================================================`n" -ForegroundColor Green
} else {
    Write-Host "`nКонтейнеры перезапущены. Проверьте логи командой: docker logs metapus-public-test-metapus-app-1" -ForegroundColor Yellow
}
