package catalog_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/catalogs/merchant"
	"metapus/internal/infrastructure/storage/postgres"
)

const (
	_merchantTable      = "cat_merchants"
	_merchantUsersTable = "sys_merchant_users"
)

// MerchantRepo implements merchant.Repository.
type MerchantRepo struct {
	*BaseCatalogRepo[*merchant.Merchant]
}

// NewMerchantRepo creates a new merchant repository.
func NewMerchantRepo() *MerchantRepo {
	return &MerchantRepo{
		BaseCatalogRepo: NewBaseCatalogRepo[*merchant.Merchant](
			_merchantTable,
			postgres.ExtractDBColumns[merchant.Merchant](),
			func() *merchant.Merchant { return &merchant.Merchant{} },
			false, // flat catalog
		),
	}
}

// GetMerchantIDsByUserID returns all merchant IDs accessible by a user.
func (r *MerchantRepo) GetMerchantIDsByUserID(ctx context.Context, userID id.ID) ([]id.ID, error) {
	q := r.Builder().Select("merchant_id").
		From(_merchantUsersTable).
		Where(squirrel.Eq{"user_id": userID})

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	rows, err := querier.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("get merchant ids by user: %w", err)
	}
	defer rows.Close()

	var ids []id.ID
	for rows.Next() {
		var mid id.ID
		if err := rows.Scan(&mid); err != nil {
			return nil, fmt.Errorf("scan merchant id: %w", err)
		}
		ids = append(ids, mid)
	}

	return ids, rows.Err()
}

// GetUsersByMerchantID returns all users associated with a merchant.
func (r *MerchantRepo) GetUsersByMerchantID(ctx context.Context, merchantID id.ID) ([]merchant.MerchantUser, error) {
	q := r.Builder().Select("user_id", "merchant_id", "role").
		From(_merchantUsersTable).
		Where(squirrel.Eq{"merchant_id": merchantID}).
		OrderBy("role ASC")

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	var users []merchant.MerchantUser
	if err := pgxscan.Select(ctx, querier, &users, sql, args...); err != nil {
		return nil, fmt.Errorf("get users by merchant: %w", err)
	}

	return users, nil
}

// AddUser creates a user-merchant association.
func (r *MerchantRepo) AddUser(ctx context.Context, merchantID, userID id.ID, role merchant.MerchantRole) error {
	q := r.Builder().Insert(_merchantUsersTable).
		Columns("user_id", "merchant_id", "role").
		Values(userID, merchantID, role)

	sql, args, err := q.ToSql()
	if err != nil {
		return fmt.Errorf("build query: %w", err)
	}

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	_, err = querier.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("add user to merchant: %w", err)
	}

	return nil
}

// RemoveUser deletes a user-merchant association.
func (r *MerchantRepo) RemoveUser(ctx context.Context, merchantID, userID id.ID) error {
	q := r.Builder().Delete(_merchantUsersTable).
		Where(squirrel.Eq{"user_id": userID, "merchant_id": merchantID})

	sql, args, err := q.ToSql()
	if err != nil {
		return fmt.Errorf("build query: %w", err)
	}

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	ct, err := querier.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("remove user from merchant: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return apperror.NewNotFound("merchant_user", fmt.Sprintf("%s/%s", merchantID, userID))
	}

	return nil
}
