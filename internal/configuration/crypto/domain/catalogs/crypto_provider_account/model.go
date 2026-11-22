// Package crypto_provider_account provides external crypto infrastructure provider accounts.
package crypto_provider_account

import (
	"context"
	"strings"
	"time"

	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/domain"
	"metapus/pkg/sdk/entity"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/numerator"
)

// ProviderCredentials holds decrypted API credentials for a resource provider.
type ProviderCredentials struct {
	BaseURL     string `json:"baseUrl,omitempty"`
	APIKey      string `json:"apiKey,omitempty"`
	APISecret   string `json:"apiSecret,omitempty"`
	CallbackURL string `json:"callbackUrl,omitempty"`
}

func (c ProviderCredentials) HasKeys() bool {
	return strings.TrimSpace(c.APIKey) != "" && strings.TrimSpace(c.APISecret) != ""
}

func (c ProviderCredentials) MaskedKey() string {
	key := strings.TrimSpace(c.APIKey)
	if len(key) == 0 {
		return ""
	}
	if len(key) <= 8 {
		return "••••••••"
	}
	return key[:2] + "••••••••" + key[len(key)-4:]
}

// CryptoProviderAccount identifies one provider balance account per blockchain network.
// Credentials are encrypted using AES-256-GCM and never returned via public GET APIs.
type CryptoProviderAccount struct {
	entity.Catalog

	Provider             string     `db:"provider" json:"provider" meta:"label:Провайдер"`
	NetworkID            id.ID      `db:"network_id" json:"networkId" meta:"label:Сеть,ref:blockchain_network"`
	BalanceTokenID       id.ID      `db:"balance_token_id" json:"balanceTokenId" meta:"label:Токен баланса,ref:token"`
	ExternalAccountRef   string     `db:"external_account_ref" json:"externalAccountRef,omitempty" meta:"label:Внешний аккаунт"`
	CredentialsEnc       []byte     `db:"credentials_enc" json:"-"`
	CredentialsVersion   int        `db:"credentials_version" json:"credentialsVersion"`
	CredentialsUpdatedAt *time.Time `db:"credentials_updated_at" json:"credentialsUpdatedAt,omitempty"`
	IsActive             bool       `db:"is_active" json:"isActive" meta:"label:Активен"`
	// SubmittedBaseURL is the base URL of credentials submitted with this
	// write. Credentials are stored encrypted, so it is validated here.
	SubmittedBaseURL string `db:"-" json:"-"`
	// CredentialsErr is why credentials submitted with this write could not
	// be sealed. The HTTP mapper cannot fail, so it records the error here and
	// Validate rejects the write instead of saving the account without them.
	CredentialsErr error `db:"-" json:"-"`
}

func New(code, name, provider string, networkID, balanceTokenID id.ID) *CryptoProviderAccount {
	return &CryptoProviderAccount{
		Catalog:            entity.NewCatalog(code, name),
		Provider:           provider,
		NetworkID:          networkID,
		BalanceTokenID:     balanceTokenID,
		CredentialsVersion: 1,
		IsActive:           true,
	}
}

func (a *CryptoProviderAccount) Validate(ctx context.Context) error {
	if err := a.Catalog.Validate(ctx); err != nil {
		return err
	}
	if a.CredentialsErr != nil {
		return a.CredentialsErr
	}
	if a.Provider == "" {
		return apperror.NewValidation("provider is required").WithDetail("field", "provider")
	}
	if id.IsNil(a.NetworkID) {
		return apperror.NewValidation("network is required").WithDetail("field", "networkId")
	}
	if id.IsNil(a.BalanceTokenID) {
		return apperror.NewValidation("balance token is required").WithDetail("field", "balanceTokenId")
	}
	if a.SubmittedBaseURL != "" {
		if err := ValidateBaseURL(a.SubmittedBaseURL); err != nil {
			return apperror.NewValidation(err.Error()).WithDetail("field", "baseUrl")
		}
	}
	return nil
}

func (CryptoProviderAccount) TableName() string { return "cat_crypto_provider_accounts" }

type Repository interface {
	domain.CatalogRepository[*CryptoProviderAccount]
	SetCredentials(ctx context.Context, accountID id.ID, creds ProviderCredentials) error
	GetCredentials(ctx context.Context, accountID id.ID) (*ProviderCredentials, error)
}

type Service struct {
	*domain.CatalogService[*CryptoProviderAccount]
	repo Repository
}

func NewService(repo Repository, num numerator.Generator) *Service {
	return &Service{
		CatalogService: domain.NewCatalogService(domain.CatalogServiceConfig[*CryptoProviderAccount]{
			Repo: repo, Numerator: num, Definition: &Definition,
		}),
		repo: repo,
	}
}

func (s *Service) SetCredentials(ctx context.Context, accountID id.ID, creds ProviderCredentials) error {
	return s.repo.SetCredentials(ctx, accountID, creds)
}

func (s *Service) GetCredentials(ctx context.Context, accountID id.ID) (*ProviderCredentials, error) {
	return s.repo.GetCredentials(ctx, accountID)
}
