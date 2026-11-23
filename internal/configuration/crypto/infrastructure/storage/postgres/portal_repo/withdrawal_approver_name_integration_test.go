//go:build integration

package portal_repo

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/application/portalapp"
	"metapus/internal/configuration/crypto/domain/crypto/aml"
	"metapus/internal/configuration/crypto/domain/documents/withdrawal_request"
	"metapus/pkg/sdk/domain"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/sdktest"
	"metapus/pkg/sdk/tenant"
	"metapus/pkg/sdk/types"
)

// The admin withdrawal reads name the approver through the platform's user
// cards: the full name, else the email, and only the ID once the user is
// marked for deletion.
func TestAdminWithdrawalReadsNameTheApprover(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("METAPUS_TEST_DATABASE_DSN is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := sdktest.NewTxManager(pool)
	ctx = tenant.WithTxManager(ctx, txm)
	rollback := errors.New("rollback withdrawal approver fixture")
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := txm.GetQuerier(ctx)
		var tokenID, networkID id.ID
		err := q.QueryRow(ctx, `SELECT id, network_id FROM cat_tokens WHERE _deleted_at IS NULL AND deletion_mark = FALSE ORDER BY created_at LIMIT 1`).Scan(&tokenID, &networkID)
		if errors.Is(err, pgx.ErrNoRows) {
			t.Skip("test database has no crypto token")
		}
		require.NoError(t, err)
		merchantID, addressID, approver := id.New(), id.New(), id.New()
		_, err = q.Exec(ctx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$2,'Withdrawal approver merchant')`, merchantID, "wa-"+id.Compact(merchantID))
		require.NoError(t, err)
		_, err = q.Exec(ctx, `INSERT INTO reg_withdrawal_addresses(id,merchant_id,network_id,address,address_key) VALUES($1,$2,$3,'external-destination','external-destination')`, addressID, merchantID, networkID)
		require.NoError(t, err)
		email := "approver-" + id.Compact(approver) + "@example.test"
		_, err = q.Exec(ctx, `INSERT INTO auth_users(id,email,password_hash,first_name,last_name) VALUES($1,$2,'x','Анна','')`, approver, email)
		require.NoError(t, err)

		doc := withdrawal_request.NewWithdrawalRequest(merchantID, tokenID, addressID, "external-destination", types.MustRawAmount("100"))
		doc.Number = "WR-" + id.Compact(doc.GetID())
		doc.Date = time.Now().UTC().Truncate(time.Microsecond)
		doc.Posted, doc.PostedVersion = true, 1
		_, err = q.Exec(ctx, `INSERT INTO reg_crypto_merchant_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,amount)
			VALUES(gen_random_uuid_v7(),$1,'WithdrawalRequest',1,$2,'expense',$3,$4,100)`, doc.GetID(), doc.Date, merchantID, tokenID)
		require.NoError(t, err)
		repo := NewDashboardRepo()
		require.NoError(t, repo.CreateWithdrawalRequest(ctx, doc, aml.Class("")))
		_, err = q.Exec(ctx, `UPDATE doc_withdrawal_requests SET approved_by=$1 WHERE id=$2`, approver, doc.GetID())
		require.NoError(t, err)

		names := func() []*string {
			detail, err := repo.GetAdminWithdrawalRequest(ctx, doc.GetID())
			require.NoError(t, err)
			items, _, err := repo.ListAdminWithdrawalRequests(ctx, "all", 500)
			require.NoError(t, err)
			var listed *portalapp.AdminWithdrawalRequestItem
			for i := range items {
				if items[i].ID == doc.GetID().String() {
					listed = &items[i]
				}
			}
			require.NotNil(t, listed, "the request is among the latest 500")
			page, err := repo.ListAdminWithdrawalRequestsPage(ctx, domain.ListFilter{Search: doc.Number, Limit: 10}, "all")
			require.NoError(t, err)
			require.Len(t, page.Items, 1)
			for _, approvedBy := range []*string{detail.ApprovedByID, listed.ApprovedByID, page.Items[0].ApprovedByID} {
				require.NotNil(t, approvedBy)
				require.Equal(t, approver.String(), *approvedBy)
			}
			return []*string{detail.ApprovedByName, listed.ApprovedByName, page.Items[0].ApprovedByName}
		}
		for _, name := range names() {
			require.NotNil(t, name)
			require.Equal(t, "Анна", *name)
		}
		_, err = q.Exec(ctx, `UPDATE auth_users SET first_name=' ',last_name=NULL WHERE id=$1`, approver)
		require.NoError(t, err)
		for _, name := range names() {
			require.NotNil(t, name)
			require.Equal(t, email, *name, "a blank name falls back to the email")
		}
		_, err = q.Exec(ctx, `UPDATE auth_users SET deletion_mark=TRUE WHERE id=$1`, approver)
		require.NoError(t, err)
		for _, name := range names() {
			require.Nil(t, name, "a user marked for deletion has no name")
		}
		return rollback
	})
	require.ErrorIs(t, err, rollback)
}
