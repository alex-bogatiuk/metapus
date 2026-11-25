package catalog_repo

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/json"
	"github.com/alex-bogatiuk/metapus/internal/domain/integrations"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/secretbox"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
)

// RenameAccountKey gives a stored key of an integration account another name.
// The value never leaves the platform: the credentials are opened, the key is
// renamed and they are sealed again. A new credentials version makes runtimes
// reload the account. It reports false when the account has no key named from.
//
// The caller holds LockIntegrationConfiguration and keeps the account's
// consumers consistent with the new name in the same transaction.
func RenameAccountKey(ctx context.Context, accountID id.ID, from, to string) (bool, error) {
	if from == to {
		return false, nil
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var sealed []byte
	err := q.QueryRow(ctx, `SELECT credentials_enc FROM cat_integration_accounts WHERE id=$1 AND _deleted_at IS NULL FOR UPDATE`, accountID).Scan(&sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, apperror.NewNotFound("integration_account", accountID.String())
	}
	if err != nil {
		return false, fmt.Errorf("lock integration account credentials: %w", err)
	}
	if len(sealed) == 0 {
		return false, nil
	}
	credentials, err := OpenCredentials(sealed)
	if err != nil {
		return false, err
	}
	i := slices.IndexFunc(credentials.Keys, func(k integrations.NamedKey) bool { return k.Name == from })
	if i < 0 {
		return false, nil
	}
	credentials.Keys[i].Name = to
	if err := credentials.Validate(); err != nil {
		return false, apperror.NewValidation(fmt.Sprintf("У аккаунта уже есть ключ «%s»", to)).WithDetail("field", "credentials")
	}
	raw, err := json.Marshal(credentials)
	if err != nil {
		return false, fmt.Errorf("encode integration credentials: %w", err)
	}
	enc, err := secretbox.Seal(raw)
	if err != nil {
		return false, fmt.Errorf("encrypt integration credentials: %w", err)
	}
	if _, err := q.Exec(ctx, `UPDATE cat_integration_accounts SET credentials_enc=$2,credentials_version=credentials_version+1,credentials_updated_at=now(),version=version+1 WHERE id=$1`, accountID, enc); err != nil {
		return false, fmt.Errorf("store renamed integration account key: %w", err)
	}
	return true, nil
}
