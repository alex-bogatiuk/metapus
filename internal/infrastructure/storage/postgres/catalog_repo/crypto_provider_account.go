package catalog_repo

import (
	"context"
	"fmt"

	"metapus/internal/core/apperror"
	corecrypto "metapus/internal/core/crypto"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/domain/catalogs/crypto_provider_account"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/platform/secretfile"
)

type CryptoProviderAccountRepo struct {
	*BaseCatalogRepo[*crypto_provider_account.CryptoProviderAccount]
}

func NewCryptoProviderAccountRepo() *CryptoProviderAccountRepo {
	return &CryptoProviderAccountRepo{BaseCatalogRepo: NewCatalogRepoForDefinition[*crypto_provider_account.CryptoProviderAccount](crypto_provider_account.Definition,
		postgres.ExtractDBColumns[crypto_provider_account.CryptoProviderAccount](),
		func() *crypto_provider_account.CryptoProviderAccount {
			return &crypto_provider_account.CryptoProviderAccount{}
		})}
}

func getCryptoProviderEncryptionKey() ([]byte, error) {
	keyStr, err := secretfile.Get("AUTOMATION_ENCRYPTION_KEY", "")
	if err != nil {
		return nil, fmt.Errorf("load encryption key: %w", err)
	}
	if keyStr == "" {
		return nil, fmt.Errorf("AUTOMATION_ENCRYPTION_KEY environment variable is not set")
	}
	key := []byte(keyStr)
	if len(key) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes, got %d", len(key))
	}
	return key, nil
}

func (r *CryptoProviderAccountRepo) SetCredentials(ctx context.Context, accountID id.ID, creds crypto_provider_account.ProviderCredentials) error {
	key, err := getCryptoProviderEncryptionKey()
	if err != nil {
		return apperror.NewInternal(err)
	}

	payload, err := json.Marshal(creds)
	if err != nil {
		return apperror.NewInternal(fmt.Errorf("marshal credentials: %w", err))
	}

	enc, err := corecrypto.Encrypt(payload, key)
	if err != nil {
		return apperror.NewInternal(fmt.Errorf("encrypt credentials: %w", err))
	}

	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	tag, err := q.Exec(ctx, `
		UPDATE cat_crypto_provider_accounts
		SET credentials_enc = $1,
		    credentials_version = credentials_version + 1,
		    credentials_updated_at = NOW(),
		    updated_at = NOW()
		WHERE id = $2 AND deletion_mark = FALSE
	`, enc, accountID)
	if err != nil {
		return fmt.Errorf("update crypto provider account credentials: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperror.NewNotFound("cat_crypto_provider_accounts", accountID.String())
	}
	return nil
}

func (r *CryptoProviderAccountRepo) GetCredentials(ctx context.Context, accountID id.ID) (*crypto_provider_account.ProviderCredentials, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var enc []byte
	err := q.QueryRow(ctx, `
		SELECT credentials_enc FROM cat_crypto_provider_accounts
		WHERE id = $1 AND deletion_mark = FALSE
	`, accountID).Scan(&enc)
	if err != nil {
		return nil, fmt.Errorf("query crypto provider account credentials: %w", err)
	}

	if len(enc) == 0 {
		return &crypto_provider_account.ProviderCredentials{}, nil
	}

	key, err := getCryptoProviderEncryptionKey()
	if err != nil {
		return nil, apperror.NewInternal(err)
	}

	dec, err := corecrypto.Decrypt(enc, key)
	if err != nil {
		return nil, apperror.NewInternal(fmt.Errorf("decrypt credentials: %w", err))
	}

	var creds crypto_provider_account.ProviderCredentials
	if err := json.Unmarshal(dec, &creds); err != nil {
		return nil, apperror.NewInternal(fmt.Errorf("unmarshal credentials: %w", err))
	}
	return &creds, nil
}
