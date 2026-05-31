---
trigger: always_on
---

```powershell
netstat -ano | findstr ":8080"  # Go backend
netstat -ano | findstr ":3000"  # Next.js
```
## Environment

# Backend
```powershell
$env:JWT_SECRET="your-secret-key-change-in-production"; $env:META_DATABASE_URL="postgres://metapus:metapus@localhost:5432/tenants?sslmode=disable"; $env:TENANT_DB_USER="metapus"; $env:TENANT_DB_PASSWORD="metapus";
$env:AUTOMATION_ENCRYPTION_KEY="test-encryption-key-32chars!!!!!"; $env:TRON_RPC_URL="https://api.shasta.trongrid.io";
$env:TRON_API_KEY="c9c9646e-0626-4035-857b-911c6aba25cc";$env:DATABASE_URL="postgres://metapus:metapus@localhost:5432/mt_default?sslmode=disable"; $env:APP_PORT="8080"; $env:APP_ENV="development"; $env:LOG_LEVEL="info"; Start-Job { go run ./cmd/server }; Start-Job { go run ./cmd/worker }
```

```bash
# Frontend
NEXT_PUBLIC_API_URL=http://localhost:8080/api/v1
NEXT_PUBLIC_TENANT_ID=5cfe45cb-9035-4e57-93e2-127e960370b8
# Test: admin@metapus.io / Admin123!
```