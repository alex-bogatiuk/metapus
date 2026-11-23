// Package integrations owns tenant-local external accounts and account pools.
package integrations

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/numerator"
	"github.com/alex-bogatiuk/metapus/internal/domain"
)

// Provider identifies a ProviderDefinition of the application's Catalog.
type Provider string

// Credentials is write-only at the HTTP boundary. Named keys share the account quota.
type Credentials struct {
	Keys      []NamedKey `json:"keys"`
	APISecret string     `json:"apiSecret,omitempty"`
	Username  string     `json:"username,omitempty"`
}
type NamedKey struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func (c Credentials) Validate() error {
	if len(c.Keys) > 32 {
		return apperror.NewValidation("Допускается не более 32 ключей аккаунта")
	}
	seen := make(map[string]bool, len(c.Keys))
	for _, k := range c.Keys {
		if k.Name == "" || len(k.Name) > 64 || seen[k.Name] || strings.ContainsAny(k.Name, "\r\n\t ") || strings.TrimSpace(k.Value) == "" || len(k.Value) > 4096 || strings.ContainsAny(k.Value, "\r\n") {
			return apperror.NewValidation("Укажите уникальное имя и непустое значение ключа без переводов строк")
		}
		seen[k.Name] = true
	}
	if len(c.APISecret) > 4096 || strings.ContainsAny(c.APISecret, "\r\n") || len(c.Username) > 320 {
		return apperror.NewValidation("Некорректные реквизиты доступа")
	}
	return nil
}
func (c Credentials) Key(slot string) (string, bool) {
	if slot == "" {
		slot = "default"
	}
	for _, k := range c.Keys {
		if k.Name == slot {
			return k.Value, true
		}
	}
	return "", false
}

type Account struct {
	entity.Catalog
	Provider              Provider     `db:"provider" json:"provider" meta:"label:Провайдер"`
	ExternalAccountRef    string       `db:"external_account_ref" json:"externalAccountRef" meta:"label:Внешний аккаунт"`
	IsActive              bool         `db:"is_active" json:"isActive" meta:"label:Активен"`
	ResetDay              int          `db:"reset_day" json:"resetDay" meta:"label:День обновления квоты"`
	ResetTimezone         string       `db:"reset_timezone" json:"resetTimezone" meta:"label:Часовой пояс квоты"`
	CredentialsEnc        []byte       `db:"credentials_enc" json:"-" meta:"skip"`
	CredentialsVersion    int          `db:"credentials_version" json:"credentialsVersion" meta:"label:Версия ключей,readOnly:true"`
	CredentialsUpdatedAt  *time.Time   `db:"credentials_updated_at" json:"credentialsUpdatedAt,omitempty" meta:"list:false,readOnly:true"`
	CredentialsExpiresAt  *time.Time   `db:"credentials_expires_at" json:"credentialsExpiresAt" meta:"label:Действует до"`
	ManualPeriodStartedAt *time.Time   `db:"manual_period_started_at" json:"manualPeriodStartedAt" meta:"label:Начало ручного периода,readOnly:true"`
	ManualPeriodVersion   int64        `db:"manual_period_version" json:"manualPeriodVersion" meta:"label:Версия ручного периода,readOnly:true"`
	PendingCredentials    *Credentials `db:"-" json:"-" meta:"skip"`
}

func NewAccount(code, name string) *Account {
	return &Account{Catalog: entity.NewCatalog(code, name), IsActive: true, ResetDay: 1, ResetTimezone: "UTC"}
}
func (Account) TableName() string { return "cat_integration_accounts" }
func (a *Account) Validate(ctx context.Context) error {
	if err := a.Catalog.Validate(ctx); err != nil {
		return err
	}
	if a.IsFolder || a.ParentID != nil {
		return apperror.NewValidation("Аккаунты интеграций не поддерживают группы")
	}
	if a.ResetDay < 1 || a.ResetDay > 31 {
		return apperror.NewValidation("День обновления квоты должен быть от 1 до 31")
	}
	if strings.TrimSpace(a.ResetTimezone) == "" || len(a.ResetTimezone) > 128 {
		return apperror.NewValidation("Неизвестный часовой пояс квоты")
	}
	if len(a.ExternalAccountRef) > 255 {
		return apperror.NewValidation("Слишком длинный идентификатор аккаунта")
	}
	return nil
}

// ValidateFor checks the rules of the account's provider: its key period and
// the shape of replacement credentials.
func (a *Account) ValidateFor(definition ProviderDefinition) error {
	if definition.ManualPeriod() && a.CredentialsExpiresAt == nil {
		return apperror.NewValidation("Укажите срок действия ключа").WithDetail("field", "credentialsExpiresAt")
	}
	if !definition.ManualPeriod() && (a.CredentialsExpiresAt != nil || a.ManualPeriodStartedAt != nil || a.ManualPeriodVersion != 0) {
		return apperror.NewValidation("Этот профиль не использует ручной срок ключа").WithDetail("field", "credentialsExpiresAt")
	}
	if a.PendingCredentials != nil {
		return definition.Credentials.Validate(*a.PendingCredentials)
	}
	return nil
}

// Period clamps billing anchors to the last day of each local calendar month.
func (a *Account) Period(definition ProviderDefinition, now time.Time, loc *time.Location) (time.Time, time.Time, error) {
	if definition.ManualPeriod() {
		if a.ManualPeriodStartedAt == nil || a.CredentialsExpiresAt == nil || a.ManualPeriodVersion < 1 || !a.CredentialsExpiresAt.After(*a.ManualPeriodStartedAt) {
			return time.Time{}, time.Time{}, apperror.NewValidation("Ручной период ключа не настроен")
		}
		return a.ManualPeriodStartedAt.UTC(), a.CredentialsExpiresAt.UTC(), nil
	}
	if definition.QuotaPolicy == QuotaPackage {
		return time.Time{}, time.Time{}, apperror.NewValidation("Пакет проверок не имеет календарного сброса")
	}
	if definition.QuotaPolicy == QuotaDailyUTC {
		start := now.UTC().Truncate(24 * time.Hour)
		return start, start.Add(24 * time.Hour), nil
	}
	if loc == nil || loc.String() != a.ResetTimezone {
		return time.Time{}, time.Time{}, fmt.Errorf("quota timezone has not been resolved")
	}
	if a.ResetDay < 1 || a.ResetDay > 31 {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid quota reset day")
	}
	n := now.In(loc)
	anchor := func(y int, m time.Month) time.Time {
		first := time.Date(y, m, 1, 0, 0, 0, 0, loc)
		last := first.AddDate(0, 1, -1).Day()
		return time.Date(first.Year(), first.Month(), min(a.ResetDay, last), 0, 0, 0, 0, loc)
	}
	start := anchor(n.Year(), n.Month())
	if now.Before(start) {
		start = anchor(n.Year(), n.Month()-1)
	}
	end := anchor(start.Year(), start.Month()+1)
	return start.UTC(), end.UTC(), nil
}

type Pool struct {
	entity.Catalog
	Provider   Provider `db:"provider" json:"provider" meta:"label:Провайдер"`
	IsActive   bool     `db:"is_active" json:"isActive" meta:"label:Активен"`
	AccountIDs []id.ID  `db:"-" json:"accountIds" meta:"skip"`
}

func NewPool(code, name string) *Pool {
	return &Pool{Catalog: entity.NewCatalog(code, name), IsActive: true, AccountIDs: []id.ID{}}
}
func (Pool) TableName() string { return "cat_integration_pools" }
func (p *Pool) Validate(ctx context.Context) error {
	if err := p.Catalog.Validate(ctx); err != nil {
		return err
	}
	if p.IsFolder || p.ParentID != nil {
		return apperror.NewValidation("Пулы не поддерживают группы")
	}
	if len(p.AccountIDs) > 64 {
		return apperror.NewValidation("Допускается не более 64 аккаунтов в пуле")
	}
	if p.IsActive && len(p.AccountIDs) == 0 {
		return apperror.NewValidation("Добавьте аккаунты или отключите пул")
	}
	seen := make(map[id.ID]bool, len(p.AccountIDs))
	for _, uid := range p.AccountIDs {
		if id.IsNil(uid) || seen[uid] {
			return apperror.NewValidation("Аккаунты пула должны быть уникальны")
		}
		seen[uid] = true
	}
	return nil
}

type AccountRepository interface {
	domain.CatalogRepository[*Account]
	// Prepare encrypts pending credentials and plans the key period under
	// the rules of the account's provider.
	Prepare(context.Context, *Account, ProviderDefinition) error
}
type PoolRepository interface {
	domain.CatalogRepository[*Pool]
	ValidateMembers(context.Context, *Pool) error
}

// NewAccountService validates accounts against the catalog's provider rules.
func NewAccountService(repo AccountRepository, num numerator.Generator, catalog *Catalog) *domain.CatalogService[*Account] {
	if catalog == nil {
		panic("integrations.NewAccountService: catalog is required")
	}
	s := domain.NewCatalogService(domain.CatalogServiceConfig[*Account]{Repo: repo, Numerator: num, Definition: &AccountDefinition})
	prepare := func(ctx context.Context, a *Account) error {
		definition, err := catalog.Require(a.Provider)
		if err != nil {
			return err
		}
		if err := a.ValidateFor(definition); err != nil {
			return err
		}
		return repo.Prepare(ctx, a, definition)
	}
	s.Hooks().OnBeforeCreate(func(ctx context.Context, a *Account) error {
		if err := catalog.ValidateCreation(a.Provider); err != nil {
			return err
		}
		return prepare(ctx, a)
	})
	s.Hooks().OnBeforeUpdate(prepare)
	return s
}

// NewPoolService validates pools against the catalog's providers.
func NewPoolService(repo PoolRepository, num numerator.Generator, catalog *Catalog) *domain.CatalogService[*Pool] {
	if catalog == nil {
		panic("integrations.NewPoolService: catalog is required")
	}
	s := domain.NewCatalogService(domain.CatalogServiceConfig[*Pool]{Repo: repo, Numerator: num, Definition: &PoolDefinition})
	s.Hooks().OnBeforeCreate(func(ctx context.Context, p *Pool) error {
		if err := catalog.ValidateCreation(p.Provider); err != nil {
			return err
		}
		return repo.ValidateMembers(ctx, p)
	})
	s.Hooks().OnBeforeUpdate(func(ctx context.Context, p *Pool) error {
		if _, ok := catalog.Definition(p.Provider); !ok {
			return apperror.NewValidation("Неизвестный провайдер пула").WithDetail("field", "provider")
		}
		return repo.ValidateMembers(ctx, p)
	})
	return s
}

type Selection struct {
	Recovery             bool       `json:"-"`
	CredentialsExpiresAt *time.Time `json:"credentialsExpiresAt"`
	ManualPeriodVersion  int64      `json:"manualPeriodVersion"`
	PoolID               id.ID      `json:"poolId"`
	AccountID            id.ID      `json:"accountId"`
	AccountName          string     `json:"accountName"`
	Provider             Provider   `json:"provider"`
	PoolVersion          int        `json:"poolVersion"`
	AccountVersion       int        `json:"accountVersion"`
	CredentialsVersion   int        `json:"credentialsVersion"`
	SelectionVersion     int64      `json:"selectionVersion"`
	PeriodStart          time.Time  `json:"periodStart"`
	PeriodEnd            time.Time  `json:"periodEnd"`
	Slot                 string     `json:"slot"`
	Key                  string     `json:"-"`

	// ExhaustionRecheck is set for automatic requests to providers with rolling
	// quotas. An operator's manual exhaustion lasts until the end of the period.
	ExhaustionRecheck time.Duration `json:"-"`
}
type Failure string

const (
	Exhausted    Failure = "quota_exhausted"
	Throttled    Failure = "rate_limited"
	Unauthorized Failure = "invalid_credentials"
	Expired      Failure = "credentials_expired"
	// KeyMissing means pool accounts lack the key name the endpoint reads.
	KeyMissing Failure = "key_missing"
)

type RuntimeRepository interface {
	Select(context.Context, id.ID, string, Provider) (Selection, error)
	Report(context.Context, Selection, Failure, time.Time) error
}
