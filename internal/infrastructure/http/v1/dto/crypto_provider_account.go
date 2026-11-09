package dto

import (
	"time"

	corecrypto "metapus/internal/core/crypto"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/domain/catalogs/crypto_provider_account"
	"metapus/internal/platform/secretfile"
)

type CreateCryptoProviderAccountRequest struct {
	Code               string `json:"code"`
	Name               string `json:"name" binding:"required"`
	Provider           string `json:"provider" binding:"required"`
	NetworkID          string `json:"networkId" binding:"required"`
	BalanceTokenID     string `json:"balanceTokenId" binding:"required"`
	ExternalAccountRef string `json:"externalAccountRef"`
	BaseURL            string `json:"baseUrl"`
	APIKey             string `json:"apiKey"`
	APISecret          string `json:"apiSecret"`
	CallbackURL        string `json:"callbackUrl"`
	IsActive           bool   `json:"isActive"`
}

func getCredentialsEncryptionKey() ([]byte, error) {
	keyStr, err := secretfile.Get("AUTOMATION_ENCRYPTION_KEY", "")
	if err != nil || len(keyStr) != 32 {
		return nil, err
	}
	return []byte(keyStr), nil
}

func (r CreateCryptoProviderAccountRequest) ToEntity() *crypto_provider_account.CryptoProviderAccount {
	networkID, _ := id.Parse(r.NetworkID)
	tokenID, _ := id.Parse(r.BalanceTokenID)
	account := crypto_provider_account.New(r.Code, r.Name, r.Provider, networkID, tokenID)
	account.ExternalAccountRef = r.ExternalAccountRef
	account.IsActive = r.IsActive

	if r.APIKey != "" || r.APISecret != "" || r.BaseURL != "" || r.CallbackURL != "" {
		creds := crypto_provider_account.ProviderCredentials{
			BaseURL:     r.BaseURL,
			APIKey:      r.APIKey,
			APISecret:   r.APISecret,
			CallbackURL: r.CallbackURL,
		}
		if payload, err := json.Marshal(creds); err == nil {
			if key, err := getCredentialsEncryptionKey(); err == nil && len(key) == 32 {
				if enc, err := corecrypto.Encrypt(payload, key); err == nil {
					account.CredentialsEnc = enc
					now := time.Now()
					account.CredentialsUpdatedAt = &now
				}
			}
		}
	}
	return account
}

type UpdateCryptoProviderAccountRequest struct {
	CreateCryptoProviderAccountRequest
	Version int `json:"version" binding:"required"`
}

func (r UpdateCryptoProviderAccountRequest) ApplyTo(account *crypto_provider_account.CryptoProviderAccount) {
	updated := r.ToEntity()
	account.Code, account.Name, account.Provider = updated.Code, updated.Name, updated.Provider
	account.NetworkID, account.BalanceTokenID = updated.NetworkID, updated.BalanceTokenID
	account.ExternalAccountRef, account.IsActive = updated.ExternalAccountRef, updated.IsActive
	account.Version = r.Version

	if r.APIKey != "" || r.APISecret != "" || r.BaseURL != "" || r.CallbackURL != "" {
		creds := crypto_provider_account.ProviderCredentials{}
		if len(account.CredentialsEnc) > 0 {
			if key, err := getCredentialsEncryptionKey(); err == nil && len(key) == 32 {
				if dec, err := corecrypto.Decrypt(account.CredentialsEnc, key); err == nil {
					_ = json.Unmarshal(dec, &creds)
				}
			}
		}
		if r.BaseURL != "" {
			creds.BaseURL = r.BaseURL
		}
		if r.APIKey != "" {
			creds.APIKey = r.APIKey
		}
		if r.APISecret != "" {
			creds.APISecret = r.APISecret
		}
		if r.CallbackURL != "" {
			creds.CallbackURL = r.CallbackURL
		}

		if payload, err := json.Marshal(creds); err == nil {
			if key, err := getCredentialsEncryptionKey(); err == nil && len(key) == 32 {
				if enc, err := corecrypto.Encrypt(payload, key); err == nil {
					account.CredentialsEnc = enc
					account.CredentialsVersion++
					now := time.Now()
					account.CredentialsUpdatedAt = &now
				}
			}
		}
	}
}

type CryptoProviderAccountResponse struct {
	ID                   string            `json:"id"`
	Code                 string            `json:"code"`
	Name                 string            `json:"name"`
	Provider             string            `json:"provider"`
	NetworkID            string            `json:"networkId"`
	BalanceTokenID       string            `json:"balanceTokenId"`
	ExternalAccountRef   string            `json:"externalAccountRef"`
	HasCredentials       bool              `json:"hasCredentials"`
	MaskedAPIKey         string            `json:"maskedApiKey,omitempty"`
	CredentialsVersion   int               `json:"credentialsVersion"`
	CredentialsUpdatedAt *time.Time        `json:"credentialsUpdatedAt,omitempty"`
	IsActive             bool              `json:"isActive"`
	ParentID             *string           `json:"parentId,omitempty"`
	IsFolder             bool              `json:"isFolder"`
	DeletionMark         bool              `json:"deletionMark"`
	Version              int               `json:"version"`
	Attributes           entity.Attributes `json:"attributes,omitempty"`
}

func FromCryptoProviderAccount(account *crypto_provider_account.CryptoProviderAccount) any {
	resp := CryptoProviderAccountResponse{
		ID:                   account.ID.String(),
		Code:                 account.Code,
		Name:                 account.Name,
		Provider:             account.Provider,
		NetworkID:            account.NetworkID.String(),
		BalanceTokenID:       account.BalanceTokenID.String(),
		ExternalAccountRef:   account.ExternalAccountRef,
		CredentialsVersion:   account.CredentialsVersion,
		CredentialsUpdatedAt: account.CredentialsUpdatedAt,
		IsActive:             account.IsActive,
		ParentID:             idToStringPtr(account.ParentID),
		IsFolder:             account.IsFolder,
		DeletionMark:         account.DeletionMark,
		Version:              account.Version,
		Attributes:           account.Attributes,
	}
	if len(account.CredentialsEnc) > 0 {
		resp.HasCredentials = true
		if key, err := getCredentialsEncryptionKey(); err == nil && len(key) == 32 {
			if dec, err := corecrypto.Decrypt(account.CredentialsEnc, key); err == nil {
				var creds crypto_provider_account.ProviderCredentials
				if json.Unmarshal(dec, &creds) == nil {
					resp.MaskedAPIKey = creds.MaskedKey()
				}
			}
		}
	}
	return resp
}
