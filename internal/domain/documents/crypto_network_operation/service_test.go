package crypto_network_operation

import (
	"context"
	"testing"

	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/core/types"
	"metapus/internal/domain"

	"github.com/stretchr/testify/require"
)

type editingRepoStub struct {
	doc      *CryptoNetworkOperation
	setCalls int
	lock     func() error
}

func (r *editingRepoStub) LockResources(context.Context, id.ID, ...id.ID) error {
	if r.lock != nil {
		return r.lock()
	}
	return nil
}

type editingTx struct{}

func (editingTx) RunInTransaction(ctx context.Context, work func(context.Context) error) error {
	return work(ctx)
}

func (r *editingRepoStub) Create(context.Context, *CryptoNetworkOperation) error { return nil }
func (r *editingRepoStub) GetByID(context.Context, id.ID) (*CryptoNetworkOperation, error) {
	copy := *r.doc
	return &copy, nil
}
func (r *editingRepoStub) GetByIDForUpdate(ctx context.Context, docID id.ID) (*CryptoNetworkOperation, error) {
	return r.GetByID(ctx, docID)
}
func (r *editingRepoStub) GetByNumber(context.Context, string) (*CryptoNetworkOperation, error) {
	return r.doc, nil
}
func (r *editingRepoStub) Update(context.Context, *CryptoNetworkOperation) error { return nil }
func (r *editingRepoStub) Delete(context.Context, id.ID) error                   { return nil }
func (r *editingRepoStub) List(context.Context, domain.ListFilter) (domain.CursorListResult[*CryptoNetworkOperation], error) {
	return domain.CursorListResult[*CryptoNetworkOperation]{}, nil
}
func (r *editingRepoStub) ListIDs(context.Context, domain.ListFilter, int) ([]id.ID, error) {
	return nil, nil
}
func (r *editingRepoStub) SetManualEditAllowed(_ context.Context, _ id.ID, allowed bool) error {
	r.doc.ManualEditAllowed = allowed
	r.setCalls++
	return nil
}
func (r *editingRepoStub) ValidatePostingReferences(context.Context, *CryptoNetworkOperation) error {
	return nil
}

func TestAllowEditingRequiresSystemAdminAndExcludesSecurityAdmin(t *testing.T) {
	doc := finalActivation(types.NewCryptoAmountFromInt64(10))
	repo := &editingRepoStub{doc: doc}
	service := NewService(repo, nil, nil, editingTx{})

	for _, role := range []string{"user", "security_admin"} {
		t.Run(role, func(t *testing.T) {
			unauthorizedCtx := appctx.WithUser(context.Background(), &appctx.UserContext{Roles: []string{role}})
			_, err := service.AllowEditing(unauthorizedCtx, doc.ID)
			require.Error(t, err)
			require.False(t, doc.ManualEditAllowed)
			require.Zero(t, repo.setCalls)
		})
	}

	adminCtx := appctx.WithUser(context.Background(), &appctx.UserContext{Roles: []string{"admin"}})
	unlocked, err := service.AllowEditing(adminCtx, doc.ID)
	require.NoError(t, err)
	require.True(t, unlocked.ManualEditAllowed)
	require.Equal(t, 1, repo.setCalls)
}

func TestManualUpdateRechecksPermissionAfterResourceWait(t *testing.T) {
	doc := finalActivation(types.NewCryptoAmountFromInt64(10))
	doc.ManualEditAllowed = true
	repo := &editingRepoStub{doc: doc}
	repo.lock = func() error {
		// A different administrator revoked permission while this command waited.
		doc.ManualEditAllowed = false
		return nil
	}
	ctx := appctx.WithUser(context.Background(), &appctx.UserContext{Roles: []string{"admin"}})
	ctx = tenant.WithTxManager(ctx, editingTx{})
	service := NewService(repo, nil, nil, editingTx{}).UserService()
	updated := *doc
	err := service.Update(ctx, &updated)
	require.ErrorContains(t, err, "strict document is locked")
	require.Zero(t, repo.setCalls)
}

func TestValidateManualUpdateAllowsOnlyAccountingFields(t *testing.T) {
	current := finalActivation(types.NewCryptoAmountFromInt64(10))
	current.ManualEditAllowed = true
	updated := *current
	updated.SourceWalletID = id.New()
	updated.CostTokenID = id.New()
	updated.CostAmount = types.NewCryptoAmountFromInt64(20)
	updated.Description = "manual correction"
	require.NoError(t, ValidateManualUpdate(current, &updated))

	updated.TxHash = "other"
	require.Error(t, ValidateManualUpdate(current, &updated))
}
