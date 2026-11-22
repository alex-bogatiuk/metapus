package cryptodto

import (
	"fmt"
	"time"

	"metapus/internal/configuration/crypto/domain/catalogs/crypto_provider_account"
	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/entity"
	"metapus/pkg/sdk/http/dto"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/json"
	"metapus/pkg/sdk/secretbox"
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

func (r CreateCryptoProviderAccountRequest) ToEntity() *crypto_provider_account.CryptoProviderAccount {
	networkID, _ := id.Parse(r.NetworkID)
	tokenID, _ := id.Parse(r.BalanceTokenID)
	account := crypto_provider_account.New(r.Code, r.Name, r.Provider, networkID, tokenID)
	account.ExternalAccountRef = r.ExternalAccountRef
	account.IsActive = r.IsActive
	account.SubmittedBaseURL = r.BaseURL

	if r.APIKey != "" || r.APISecret != "" || r.BaseURL != "" || r.CallbackURL != "" {
		creds := crypto_provider_account.ProviderCredentials{
			BaseURL:     r.BaseURL,
			APIKey:      r.APIKey,
			APISecret:   r.APISecret,
			CallbackURL: r.CallbackURL,
		}
		enc, err := sealCredentials(creds)
		if err != nil {
			account.CredentialsErr = err
			return account
		}
		account.CredentialsEnc = enc
		now := time.Now()
		account.CredentialsUpdatedAt = &now
	}
	return account
}

// sealCredentials keeps the key error (a validation error asking for a
// configured key) and reports any other failure as internal.
func sealCredentials(creds crypto_provider_account.ProviderCredentials) ([]byte, error) {
	payload, err := json.Marshal(creds)
	if err != nil {
		return nil, apperror.NewInternal(fmt.Errorf("encode provider credentials: %w", err))
	}
	if err := secretbox.Ready(); err != nil {
		return nil, err
	}
	enc, err := secretbox.Seal(payload)
	if err != nil {
		return nil, apperror.NewInternal(fmt.Errorf("encrypt provider credentials: %w", err))
	}
	return enc, nil
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
	account.SubmittedBaseURL = r.BaseURL

	if r.APIKey != "" || r.APISecret != "" || r.BaseURL != "" || r.CallbackURL != "" {
		// Fields left empty keep their stored values, so stored credentials
		// that cannot be read fail the write rather than being replaced by
		// a partial set.
		creds := crypto_provider_account.ProviderCredentials{}
		if len(account.CredentialsEnc) > 0 {
			if err := secretbox.Ready(); err != nil {
				account.CredentialsErr = err
				return
			}
			dec, err := secretbox.Open(account.CredentialsEnc)
			if err == nil {
				err = json.Unmarshal(dec, &creds)
			}
			if err != nil {
				account.CredentialsErr = apperror.NewInternal(fmt.Errorf("open stored provider credentials: %w", err))
				return
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

		enc, err := sealCredentials(creds)
		if err != nil {
			account.CredentialsErr = err
			return
		}
		account.CredentialsEnc = enc
		account.CredentialsVersion++
		now := time.Now()
		account.CredentialsUpdatedAt = &now
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
		ParentID:             dto.IDToStringPtr(account.ParentID),
		IsFolder:             account.IsFolder,
		DeletionMark:         account.DeletionMark,
		Version:              account.Version,
		Attributes:           account.Attributes,
	}
	if len(account.CredentialsEnc) > 0 {
		resp.HasCredentials = true
		if dec, err := secretbox.Open(account.CredentialsEnc); err == nil {
			var creds crypto_provider_account.ProviderCredentials
			if json.Unmarshal(dec, &creds) == nil {
				resp.MaskedAPIKey = creds.MaskedKey()
			}
		}
	}
	return resp
}
