package catalog_repo

import (
	"context"
	"crypto/subtle"
	"fmt"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/json"
	"github.com/alex-bogatiuk/metapus/internal/domain/integrations"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/secretbox"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
)

func (r *IntegrationAccountRepo) Create(ctx context.Context, a *integrations.Account) error {
	return r.writeWithUniqueKeys(ctx, a, r.BaseCatalogRepo.Create)
}

func (r *IntegrationAccountRepo) Update(ctx context.Context, a *integrations.Account) error {
	return r.writeWithUniqueKeys(ctx, a, r.BaseCatalogRepo.Update)
}

func (r *IntegrationAccountRepo) writeWithUniqueKeys(ctx context.Context, a *integrations.Account, write func(context.Context, *integrations.Account) error) error {
	return postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		if err := LockIntegrationConfiguration(ctx); err != nil {
			return err
		}
		if err := r.validateUniqueKeys(ctx, a); err != nil {
			return err
		}
		if !a.DeletionMark && len(a.CredentialsEnc) > 0 {
			for _, consumer := range r.consumers {
				if err := consumer.ValidateAccount(ctx, a); err != nil {
					return err
				}
			}
		}
		return write(ctx, a)
	})
}

// Compare plaintext only in memory: AES-GCM uses a random nonce, so equal keys
// have different ciphertexts. The configuration lock covers both this check and
// the write, including repository callers outside the catalog service.
// Existing accounts need no fingerprint backfill or credential rewrite.
func (r *IntegrationAccountRepo) validateUniqueKeys(ctx context.Context, a *integrations.Account) error {
	if a.DeletionMark || len(a.CredentialsEnc) == 0 {
		return nil
	}
	credentials, err := OpenCredentials(a.CredentialsEnc)
	if err != nil {
		return err
	}
	if len(credentials.Keys) == 0 {
		return nil
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	rows, err := q.Query(ctx, `SELECT code,name,credentials_enc FROM cat_integration_accounts
		WHERE provider=$1 AND id<>$2 AND NOT deletion_mark AND _deleted_at IS NULL
		AND credentials_enc IS NOT NULL ORDER BY code,id`, a.Provider, a.ID)
	if err != nil {
		return fmt.Errorf("check integration credential uniqueness: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var code, name string
		var encrypted []byte
		if err := rows.Scan(&code, &name, &encrypted); err != nil {
			return fmt.Errorf("scan integration credentials: %w", err)
		}
		if len(encrypted) == 0 {
			continue
		}
		stored, err := OpenCredentials(encrypted)
		if err != nil {
			return err
		}
		for _, incoming := range credentials.Keys {
			for _, existing := range stored.Keys {
				// Slot names describe endpoints, not ownership of the API key.
				if subtle.ConstantTimeCompare([]byte(incoming.Value), []byte(existing.Value)) == 1 {
					return apperror.NewConflict(fmt.Sprintf("Ключи уже используются в аккаунте «%s» (%s).", name, code)).
						WithDetail("field", "credentials").
						WithDetail("accountName", name).
						WithDetail("accountCode", code)
				}
			}
		}
	}
	return rows.Err()
}

// OpenCredentials decodes an integration account's sealed credentials. A
// missing or invalid platform key keeps its validation error; a value that
// does not open is an internal error that names no secret.
func OpenCredentials(sealed []byte) (integrations.Credentials, error) {
	var credentials integrations.Credentials
	if err := secretbox.Ready(); err != nil {
		return credentials, err
	}
	raw, err := secretbox.Open(sealed)
	if err != nil {
		return credentials, apperror.NewInternal(fmt.Errorf("decrypt integration credentials"))
	}
	if err := json.Unmarshal(raw, &credentials); err != nil {
		return credentials, apperror.NewInternal(fmt.Errorf("decode integration credentials"))
	}
	return credentials, nil
}
