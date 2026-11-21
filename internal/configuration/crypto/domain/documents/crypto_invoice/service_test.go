package crypto_invoice

import (
	"context"
	"testing"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/core/types"
	"metapus/internal/domain"
)

type creationTxMarker struct{}

type creationTxManager struct{ calls int }

func (m *creationTxManager) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	m.calls++
	return fn(context.WithValue(ctx, creationTxMarker{}, true))
}

type creationRepoStub struct {
	createInTransaction bool
}

func (r *creationRepoStub) Create(ctx context.Context, _ *CryptoInvoice) error {
	r.createInTransaction, _ = ctx.Value(creationTxMarker{}).(bool)
	return nil
}

func (*creationRepoStub) GetByID(context.Context, id.ID) (*CryptoInvoice, error) { return nil, nil }
func (*creationRepoStub) GetByNumber(context.Context, string) (*CryptoInvoice, error) {
	return nil, nil
}
func (*creationRepoStub) Update(context.Context, *CryptoInvoice) error { return nil }
func (*creationRepoStub) Delete(context.Context, id.ID) error          { return nil }
func (*creationRepoStub) List(context.Context, domain.ListFilter) (domain.CursorListResult[*CryptoInvoice], error) {
	return domain.CursorListResult[*CryptoInvoice]{}, nil
}
func (*creationRepoStub) ListIDs(context.Context, domain.ListFilter, int) ([]id.ID, error) {
	return nil, nil
}
func (*creationRepoStub) FindByExternalID(context.Context, string) (*CryptoInvoice, error) {
	return nil, nil
}
func (*creationRepoStub) FindByPublicRef(context.Context, string) (*CryptoInvoice, error) {
	return nil, nil
}
func (*creationRepoStub) FindByMerchantOrderID(context.Context, id.ID, string) (*CryptoInvoice, error) {
	return nil, nil
}
func (*creationRepoStub) FindByMerchantExternalID(context.Context, id.ID, string) (*CryptoInvoice, error) {
	return nil, nil
}
func (*creationRepoStub) GetByIDForUpdate(context.Context, id.ID) (*CryptoInvoice, error) {
	return nil, nil
}
func (*creationRepoStub) FindOverdueIDs(context.Context, int) ([]id.ID, error)      { return nil, nil }
func (*creationRepoStub) LockWalletForInvoice(context.Context, id.ID) error         { return nil }
func (*creationRepoStub) ReleaseInvoiceWallet(context.Context, id.ID, string) error { return nil }
func (*creationRepoStub) CleanupOrphanedExpiredLeases(context.Context) error        { return nil }

func TestCreateRunsWalletAdmissionHookAndInsertInSameTransaction(t *testing.T) {
	repo := &creationRepoStub{}
	txm := &creationTxManager{}
	service := NewService(repo, nil, numerator.Noop(), txm)
	hookInTransaction := false
	service.Hooks().OnBeforeCreate(func(ctx context.Context, _ *CryptoInvoice) error {
		hookInTransaction, _ = ctx.Value(creationTxMarker{}).(bool)
		return nil
	})

	invoice := NewCryptoInvoice(id.New(), id.New(), types.NewRawAmountFromInt64(1))
	invoice.SetNumber("CI-TEST")
	invoice.ExpiresAt = time.Now().UTC().Add(time.Hour)

	if err := service.Create(context.Background(), invoice); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !hookInTransaction {
		t.Fatal("before-create hook ran outside transaction")
	}
	if !repo.createInTransaction {
		t.Fatal("invoice INSERT ran outside transaction")
	}
	if txm.calls == 0 {
		t.Fatalf("transaction calls = %d, want a creation transaction", txm.calls)
	}
}
