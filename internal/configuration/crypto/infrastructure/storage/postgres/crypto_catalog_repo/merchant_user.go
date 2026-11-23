package crypto_catalog_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/catalogs/merchant"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
)

const _merchantUserTable = "sys_merchant_users"

// MerchantUserRepo implements merchant.MerchantUserRepository.
// Uses TxManager from context — fully tenant-aware.
type MerchantUserRepo struct{}

// NewMerchantUserRepo creates a new merchant user repository.
func NewMerchantUserRepo() *MerchantUserRepo {
	return &MerchantUserRepo{}
}

func (r *MerchantUserRepo) builder() squirrel.StatementBuilderType {
	return squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar)
}

func (r *MerchantUserRepo) querier(ctx context.Context) postgres.Querier {
	return postgres.MustGetTxManager(ctx).GetQuerier(ctx)
}

// Add grants a user access to a merchant with the given role.
// Uses UPSERT so duplicate calls are idempotent.
func (r *MerchantUserRepo) Add(ctx context.Context, userID, merchantID id.ID, role merchant.MerchantRole) error {
	const q = `
		INSERT INTO sys_merchant_users (user_id, merchant_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, merchant_id) DO UPDATE SET role = EXCLUDED.role, updated_at = now()`

	if _, err := r.querier(ctx).Exec(ctx, q, userID, merchantID, int(role)); err != nil {
		return fmt.Errorf("merchant_user add: %w", err)
	}
	return nil
}

// Remove revokes a user's access to a merchant.
func (r *MerchantUserRepo) Remove(ctx context.Context, userID, merchantID id.ID) error {
	sql, args, err := r.builder().
		Delete(_merchantUserTable).
		Where(squirrel.Eq{"user_id": userID, "merchant_id": merchantID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("merchant_user remove build: %w", err)
	}

	ct, err := r.querier(ctx).Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("merchant_user remove: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return apperror.NewNotFound(_merchantUserTable, userID.String())
	}
	return nil
}

// UpdateRole changes a user's role within a merchant.
func (r *MerchantUserRepo) UpdateRole(ctx context.Context, userID, merchantID id.ID, role merchant.MerchantRole) error {
	sql, args, err := r.builder().
		Update(_merchantUserTable).
		Set("role", int(role)).
		Set("updated_at", squirrel.Expr("now()")).
		Where(squirrel.Eq{"user_id": userID, "merchant_id": merchantID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("merchant_user update_role build: %w", err)
	}

	ct, err := r.querier(ctx).Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("merchant_user update_role: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return apperror.NewNotFound(_merchantUserTable, userID.String())
	}
	return nil
}

// ListByMerchant returns all user associations for a merchant with the
// users' email and name from the platform.
func (r *MerchantUserRepo) ListByMerchant(ctx context.Context, merchantID id.ID) ([]merchant.MerchantUser, error) {
	const q = `
		SELECT user_id, merchant_id, role, created_at
		FROM sys_merchant_users
		WHERE merchant_id = $1
		ORDER BY created_at ASC`

	users, err := r.scanListSimple(ctx, q, merchantID)
	if err != nil {
		return nil, err
	}
	userIDs := make([]id.ID, len(users))
	for i, mu := range users {
		userIDs[i] = mu.UserID
	}
	cards, err := postgres.UserCards(ctx, userIDs)
	if err != nil {
		return nil, fmt.Errorf("merchant_user list users: %w", err)
	}
	for i := range users {
		card := cards[users[i].UserID]
		users[i].UserEmail, users[i].UserFullName = card.Email, card.DisplayName
	}
	return users, nil
}

// ListByUser returns all merchant associations for a user, ordered by created_at.
func (r *MerchantUserRepo) ListByUser(ctx context.Context, userID id.ID) ([]merchant.MerchantUser, error) {
	const q = `
		SELECT user_id, merchant_id, role, created_at
		FROM sys_merchant_users
		WHERE user_id = $1
		ORDER BY created_at ASC`

	return r.scanListSimple(ctx, q, userID)
}

// GetRole returns the role a user has for a specific merchant.
func (r *MerchantUserRepo) GetRole(ctx context.Context, userID, merchantID id.ID) (merchant.MerchantRole, error) {
	const q = `SELECT role FROM sys_merchant_users WHERE user_id = $1 AND merchant_id = $2`

	var roleInt int
	if err := r.querier(ctx).QueryRow(ctx, q, userID, merchantID).Scan(&roleInt); err != nil {
		return 0, apperror.NewNotFound(_merchantUserTable, userID.String())
	}
	return merchant.MerchantRole(roleInt), nil
}

// Ensure compile-time interface satisfaction.
var _ merchant.MerchantUserRepository = (*MerchantUserRepo)(nil)

// MerchantUserRepo also satisfies middleware.MerchantAccessChecker (ISP).
// Verified at compile time to ensure RequireMerchantAccess middleware
// can accept the repo directly without an adapter.
var _ interface {
	GetRole(ctx context.Context, userID, merchantID id.ID) (merchant.MerchantRole, error)
} = (*MerchantUserRepo)(nil)

// scanListSimple scans rows with 4 base columns (no user enrichment).
// Used by ListByUser where the JOIN is not needed.
func (r *MerchantUserRepo) scanListSimple(ctx context.Context, q string, arg id.ID) ([]merchant.MerchantUser, error) {
	rows, err := r.querier(ctx).Query(ctx, q, arg)
	if err != nil {
		return nil, fmt.Errorf("merchant_user list: %w", err)
	}
	defer rows.Close()

	var result []merchant.MerchantUser
	for rows.Next() {
		var mu merchant.MerchantUser
		var roleInt int
		if err := rows.Scan(&mu.UserID, &mu.MerchantID, &roleInt, &mu.CreatedAt); err != nil {
			return nil, fmt.Errorf("merchant_user list scan: %w", err)
		}
		mu.Role = merchant.MerchantRole(roleInt)
		result = append(result, mu)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("merchant_user list rows: %w", err)
	}
	out := make([]merchant.MerchantUser, len(result))
	copy(out, result)
	return out, nil
}
