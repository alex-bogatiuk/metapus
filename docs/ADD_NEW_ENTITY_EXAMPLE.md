# Пример: Добавление нового справочника "Банковские счета"

Этот документ показывает пошаговый процесс добавления нового справочника после рефакторинга.

> Важно: проект использует **Database-per-Tenant**.  
> Поэтому запросы не содержат фильтрации по tenant — изоляция обеспечивается выбором базы.  
> Tenant/TxManager берутся из `context.Context` через middleware (см. `Manifest.md`).

---

## Задача

Добавить справочник **"Банковские счета"** (Bank Accounts) со следующими полями:
- Номер счёта (account_number)
- Название банка (bank_name)
- БИК (bic)
- Корреспондентский счёт (correspondent_account)
- Валюта (currency_id - ссылка на справочник Валюты)

---

## Шаг 1: Создать миграцию БД (5 мин)

**Файл:** `db/migrations/00030_cat_bank_accounts.sql`

```sql
-- Create bank accounts catalog
CREATE TABLE IF NOT EXISTS cat_bank_accounts (
    -- Base fields (from entity.BaseEntity / entity.Catalog)
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,
    version INT NOT NULL DEFAULT 1,
    attributes JSONB DEFAULT '{}',
    
    -- Catalog fields (from entity.Catalog)
    code VARCHAR(50) NOT NULL,
    name VARCHAR(255) NOT NULL,
    parent_id UUID REFERENCES cat_bank_accounts(id),
    is_folder BOOLEAN NOT NULL DEFAULT FALSE,
    
    -- Specific fields
    account_number VARCHAR(20) NOT NULL,
    bank_name VARCHAR(255) NOT NULL,
    bic VARCHAR(9) NOT NULL,
    correspondent_account VARCHAR(20),
    currency_id UUID NOT NULL REFERENCES cat_currencies(id),
    
    -- Constraints
    CONSTRAINT chk_bank_accounts_account_number_len CHECK (char_length(account_number) = 20),
    CONSTRAINT chk_bank_accounts_bic_len CHECK (char_length(bic) = 9)
);

-- Indexes
-- Unique code within tenant DB (excluding soft-deleted)
CREATE UNIQUE INDEX idx_cat_bank_accounts_code
    ON cat_bank_accounts (code)
    WHERE deletion_mark = FALSE;

-- Unique account number within tenant DB (excluding soft-deleted)
CREATE UNIQUE INDEX idx_cat_bank_accounts_account_number
    ON cat_bank_accounts (account_number)
    WHERE deletion_mark = FALSE;

-- Search / filters
CREATE INDEX idx_cat_bank_accounts_name ON cat_bank_accounts USING gin (name gin_trgm_ops);
CREATE INDEX idx_cat_bank_accounts_bank_name ON cat_bank_accounts USING gin (bank_name gin_trgm_ops);
CREATE INDEX idx_cat_bank_accounts_parent ON cat_bank_accounts (parent_id) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_bank_accounts_currency ON cat_bank_accounts (currency_id) WHERE deletion_mark = FALSE;

-- JSONB GIN index for custom fields search
CREATE INDEX idx_cat_bank_accounts_attrs ON cat_bank_accounts USING gin (attributes);

-- Примечание: CDC/Audit триггеры добавляйте только если они реально реализованы в проекте.
```

---

## Шаг 2: Создать модель (5 мин)

**Файл:** `internal/domain/catalogs/bank_account/model.go`

```go
// Package bank_account provides the BankAccount catalog.
package bank_account

import (
    "context"
    
    "metapus/internal/core/apperror"
    "metapus/internal/core/entity"
    "metapus/internal/core/id"
)

// BankAccount represents a bank account (Банковский счёт).
type BankAccount struct {
    entity.Catalog
    
    // Account number (20-значный номер счёта)
    AccountNumber string `db:"account_number" json:"accountNumber"`
    
    // Bank name
    BankName string `db:"bank_name" json:"bankName"`
    
    // BIC (Bank Identification Code - БИК банка)
    BIC string `db:"bic" json:"bic"`
    
    // Correspondent account
    CorrespondentAccount *string `db:"correspondent_account" json:"correspondentAccount,omitempty"`
    
    // Currency reference
    CurrencyID id.ID `db:"currency_id" json:"currencyId"`
}

// NewBankAccount creates a new bank account.
func NewBankAccount(code, name string) *BankAccount {
    return &BankAccount{
        Catalog: entity.NewCatalog(code, name),
    }
}

// Validate implements entity.Validatable interface.
func (b *BankAccount) Validate(ctx context.Context) error {
    // Base validation
    if err := b.Catalog.Validate(ctx); err != nil {
        return err
    }
    
    // Account number validation (simplified)
    if b.AccountNumber != "" && len(b.AccountNumber) != 20 {
        return apperror.NewValidation("account number must be 20 digits").
            WithDetail("field", "accountNumber")
    }
    
    // BIC validation (9 digits)
    if b.BIC == "" {
        return apperror.NewValidation("BIC is required").
            WithDetail("field", "bic")
    }
    if len(b.BIC) != 9 {
        return apperror.NewValidation("BIC must be 9 digits").
            WithDetail("field", "bic")
    }
    
    // Currency is required
    if id.IsNil(b.CurrencyID) {
        return apperror.NewValidation("currency is required").
            WithDetail("field", "currencyId")
    }
    
    return nil
}
```

---

## Шаг 3: Создать репозиторий (3 мин)

**Файл:** `internal/infrastructure/storage/postgres/catalog_repo/bank_account.go`

```go
package catalog_repo

import (
    "metapus/internal/domain/catalogs/bank_account"
    "metapus/internal/infrastructure/storage/postgres"
)

const bankAccountTable = "cat_bank_accounts"

// BankAccountRepo implements bank_account.Repository.
type BankAccountRepo struct {
    *BaseCatalogRepo[*bank_account.BankAccount]
}

// NewBankAccountRepo creates a new bank account repository.
func NewBankAccountRepo() *BankAccountRepo {
    return &BankAccountRepo{
        BaseCatalogRepo: NewBaseCatalogRepo[*bank_account.BankAccount](
            bankAccountTable,
            postgres.ExtractDBColumns[bank_account.BankAccount](), // ✨ Магия здесь!
        ),
    }
}

// Специфичные методы (если нужны):

// FindByAccountNumber retrieves bank account by account number.
// func (r *BankAccountRepo) FindByAccountNumber(ctx context.Context, accountNumber string) (*bank_account.BankAccount, error) {
//     q := r.baseSelect(ctx).
//         Where(squirrel.Eq{"account_number": accountNumber}).
//         Limit(1)
//     return r.FindOne(ctx, q)
// }
```

---

## Шаг 4: Создать сервис (5 мин)

**Файл:** `internal/domain/catalogs/bank_account/service.go`

```go
package bank_account

import (
    "context"
    
    "metapus/internal/core/id"
    "metapus/internal/domain"
    "metapus/pkg/numerator"
)

// Repository defines bank account-specific repository methods.
type Repository interface {
    domain.CatalogRepository[*BankAccount]
    // Добавьте специфичные методы если нужны:
    // FindByAccountNumber(ctx context.Context, accountNumber string) (*BankAccount, error)
}

// Service provides business logic for BankAccount catalog.
type Service struct {
    *domain.CatalogService[*BankAccount]
    repo Repository
}

// NewService creates a new bank account service.
func NewService(
    repo Repository,
    numerator *numerator.Service,
) *Service {
    base := domain.NewCatalogService(domain.CatalogServiceConfig[*BankAccount]{
        Repo:       repo,
        TxManager:  nil, // Will be obtained from context (Database-per-Tenant)
        Numerator:  numerator,
        EntityName: "bank_account",
    })
    
    svc := &Service{
        CatalogService: base,
        repo:           repo,
    }
    
    // Регистрируем hooks если нужна специфичная логика
    base.Hooks().OnBeforeCreate(svc.validateUniqueness)
    
    return svc
}

// validateUniqueness проверяет уникальность номера счёта.
func (s *Service) validateUniqueness(ctx context.Context, ba *BankAccount) error {
    // Автогенерация кода если не указан
    if ba.Code == "" && ba.AccountNumber != "" {
        ba.Code = ba.AccountNumber
    }
    
    // Здесь можно добавить проверку уникальности account_number
    // exists, _ := s.repo.FindByAccountNumber(ctx, ba.AccountNumber)
    // if exists != nil { return error }
    
    return nil
}
```

---

## Шаг 5: Создать DTO (8 мин)

**Файл:** `internal/infrastructure/http/v1/dto/bank_account.go`

```go
package dto

import (
    "metapus/internal/core/entity"
    "metapus/internal/core/id"
    "metapus/internal/domain/catalogs/bank_account"
)

// --- Request DTOs ---

// CreateBankAccountRequest is the request body for creating a bank account.
type CreateBankAccountRequest struct {
    Code                 string            `json:"code"`
    Name                 string            `json:"name" binding:"required"`
    AccountNumber        string            `json:"accountNumber" binding:"required"`
    BankName             string            `json:"bankName" binding:"required"`
    BIC                  string            `json:"bic" binding:"required"`
    CorrespondentAccount *string           `json:"correspondentAccount"`
    CurrencyID           string            `json:"currencyId" binding:"required"`
    ParentID             *string           `json:"parentId"`
    IsFolder             bool              `json:"isFolder"`
    Attributes           entity.Attributes `json:"attributes"`
}

// ToEntity converts DTO to domain entity.
func (r *CreateBankAccountRequest) ToEntity() *bank_account.BankAccount {
    currencyID, _ := id.Parse(r.CurrencyID)

    ba := bank_account.NewBankAccount(r.Code, r.Name)
    ba.AccountNumber = r.AccountNumber
    ba.BankName = r.BankName
    ba.BIC = r.BIC
    ba.CorrespondentAccount = r.CorrespondentAccount
    ba.CurrencyID = currencyID
    ba.ParentID = r.ParentID
    ba.IsFolder = r.IsFolder
    ba.Attributes = r.Attributes

    return ba
}

// UpdateBankAccountRequest is the request body for updating a bank account.
type UpdateBankAccountRequest struct {
    Code                 string            `json:"code"`
    Name                 string            `json:"name" binding:"required"`
    AccountNumber        string            `json:"accountNumber" binding:"required"`
    BankName             string            `json:"bankName" binding:"required"`
    BIC                  string            `json:"bic" binding:"required"`
    CorrespondentAccount *string           `json:"correspondentAccount"`
    CurrencyID           string            `json:"currencyId" binding:"required"`
    ParentID             *string           `json:"parentId"`
    IsFolder             bool              `json:"isFolder"`
    Attributes           entity.Attributes `json:"attributes"`
    Version              int               `json:"version" binding:"required"`
}

// ApplyTo applies update DTO to existing entity.
func (r *UpdateBankAccountRequest) ApplyTo(ba *bank_account.BankAccount) error {
    currencyID, err := id.Parse(r.CurrencyID)
    if err != nil {
        return err
    }
    
    ba.Code = r.Code
    ba.Name = r.Name
    ba.AccountNumber = r.AccountNumber
    ba.BankName = r.BankName
    ba.BIC = r.BIC
    ba.CorrespondentAccount = r.CorrespondentAccount
    ba.CurrencyID = currencyID
    ba.ParentID = r.ParentID
    ba.IsFolder = r.IsFolder
    ba.Attributes = r.Attributes
    ba.Version = r.Version
    
    return nil
}

// --- Response DTOs ---

// BankAccountResponse is the response body for a bank account.
type BankAccountResponse struct {
    ID                   string            `json:"id"`
    Code                 string            `json:"code"`
    Name                 string            `json:"name"`
    AccountNumber        string            `json:"accountNumber"`
    BankName             string            `json:"bankName"`
    BIC                  string            `json:"bic"`
    CorrespondentAccount *string           `json:"correspondentAccount,omitempty"`
    CurrencyID           string            `json:"currencyId"`
    ParentID             *string           `json:"parentId,omitempty"`
    IsFolder             bool              `json:"isFolder"`
    DeletionMark         bool              `json:"deletionMark"`
    Version              int               `json:"version"`
    Attributes           entity.Attributes `json:"attributes,omitempty"`
}

// FromBankAccount creates response DTO from domain entity.
func FromBankAccount(ba *bank_account.BankAccount) *BankAccountResponse {
    return &BankAccountResponse{
        ID:                   ba.ID.String(),
        Code:                 ba.Code,
        Name:                 ba.Name,
        AccountNumber:        ba.AccountNumber,
        BankName:             ba.BankName,
        BIC:                  ba.BIC,
        CorrespondentAccount: ba.CorrespondentAccount,
        CurrencyID:           ba.CurrencyID.String(),
        ParentID:             ba.ParentID,
        IsFolder:             ba.IsFolder,
        DeletionMark:         ba.DeletionMark,
        Version:              ba.Version,
        Attributes:           ba.Attributes,
    }
}
```

---

## Шаг 6: Создать HTTP handler (3 мин)

**Файл:** `internal/infrastructure/http/v1/handlers/bank_account.go`

```go
package handlers

import (
    "metapus/internal/domain/catalogs/bank_account"
    "metapus/internal/infrastructure/http/v1/dto"
)

// BankAccountHandler is the HTTP handler for bank accounts.
type BankAccountHandler = CatalogHandler[
    *bank_account.BankAccount,
    dto.CreateBankAccountRequest,
    dto.UpdateBankAccountRequest,
]

// NewBankAccountHandler creates a new bank account handler.
func NewBankAccountHandler(
    base *BaseHandler,
    service *bank_account.Service,
) *BankAccountHandler {
    config := CatalogHandlerConfig[
        *bank_account.BankAccount,
        dto.CreateBankAccountRequest,
        dto.UpdateBankAccountRequest,
    ]{
        Service:    service.CatalogService,
        EntityName: "bank_account",
        
        MapCreateDTO: func(req dto.CreateBankAccountRequest) *bank_account.BankAccount {
            return req.ToEntity()
        },
        
        MapUpdateDTO: func(req dto.UpdateBankAccountRequest, existing *bank_account.BankAccount) *bank_account.BankAccount {
            req.ApplyTo(existing)
            return existing
        },
        
        MapToDTO: func(entity *bank_account.BankAccount) any {
            return dto.FromBankAccount(entity)
        },
    }
    
    return NewCatalogHandler(base, config)
}
```

---

## Шаг 7: Зарегистрировать роуты (1 мин) ⚡

**Файл:** `internal/infrastructure/http/v1/router.go`

Добавить в функцию `registerCatalogRoutes()`:

```go
// --- BANK ACCOUNTS ---
{
    repo := catalog_repo.NewBankAccountRepo()
    service := bank_account.NewService(repo, cfg.Numerator)
    handler := handlers.NewBankAccountHandler(baseHandler, service)
    RegisterCatalogRoutes(catalogs.Group("/bank-accounts"), handler, "catalog:bank_account")  // ✨ Одна строка!
}
```

---

## Шаг 8: Добавить permissions в seed (2 мин)

**Файл:** `db/migrations/00021_auth_seed_permissions.sql` (или создать новую миграцию)

```sql
INSERT INTO auth_permissions (id, name, description, resource_type, resource_name, action, created_at) VALUES
    (gen_random_uuid_v7(), 'catalog:bank_account:read', 'Read bank accounts', 'catalog', 'bank_account', 'read', NOW()),
    (gen_random_uuid_v7(), 'catalog:bank_account:create', 'Create bank accounts', 'catalog', 'bank_account', 'create', NOW()),
    (gen_random_uuid_v7(), 'catalog:bank_account:update', 'Update bank accounts', 'catalog', 'bank_account', 'update', NOW()),
    (gen_random_uuid_v7(), 'catalog:bank_account:delete', 'Delete bank accounts', 'catalog', 'bank_account', 'delete', NOW());
```

---

## Готово! 🎉

**Итого времени:** ~22 минуты

**Результат:**
- ✅ Полный CRUD API для банковских счетов
- ✅ Поддержка иерархии (папки/элементы)
- ✅ Soft delete (deletion_mark)
- ✅ Optimistic locking (version)
- ✅ Multi-tenancy: Database-per-Tenant (изоляция через отдельные базы)
- ✅ Permissions

**API эндпоинты (автоматически):**
```
GET    /api/v1/catalog/bank-accounts          - List
POST   /api/v1/catalog/bank-accounts          - Create
GET    /api/v1/catalog/bank-accounts/:id      - Get
PUT    /api/v1/catalog/bank-accounts/:id      - Update
DELETE /api/v1/catalog/bank-accounts/:id      - Delete
POST   /api/v1/catalog/bank-accounts/:id/deletion-mark - Set deletion mark
GET    /api/v1/catalog/bank-accounts/tree     - Get tree
```

---

## Проверка

```bash
# Компиляция
go build ./cmd/server

# Запуск
./server

# Тест API (пример)
curl -X POST http://localhost:8080/api/v1/catalog/bank-accounts \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <token>" \
  -d '{
    "name": "Основной счёт",
    "accountNumber": "40702810100000000001",
    "bankName": "Сбербанк",
    "bic": "044525225",
    "currencyId": "<currency-uuid>"
  }'
```

---

