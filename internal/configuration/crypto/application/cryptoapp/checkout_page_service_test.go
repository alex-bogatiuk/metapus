package cryptoapp

import (
	"context"
	"testing"
	"time"

	"metapus/internal/configuration/crypto/domain/documents/crypto_invoice"
	"metapus/pkg/sdk/domain"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/tenant"
	"metapus/pkg/sdk/types"
)

type checkoutPageRepoStub struct {
	link          RedeemablePaymentLink
	page          CheckoutPageData
	markCalls     int
	viewCalls     int
	markSucceeded bool
}

func (r *checkoutPageRepoStub) GetCheckoutPage(context.Context, string) (CheckoutPageData, error) {
	return r.page, nil
}
func (r *checkoutPageRepoStub) GetCheckoutStatus(context.Context, string) (CheckoutStatus, error) {
	return CheckoutStatus{}, nil
}
func (r *checkoutPageRepoStub) RecordInvoiceViewed(context.Context, id.ID) error {
	r.viewCalls++
	return nil
}
func (r *checkoutPageRepoStub) LockPaymentLinkForRedemption(context.Context, string) (RedeemablePaymentLink, error) {
	return r.link, nil
}
func (r *checkoutPageRepoStub) MarkPaymentLinkRedeemed(context.Context, id.ID) (bool, error) {
	r.markCalls++
	return r.markSucceeded, nil
}

type invoiceDocumentServiceStub struct {
	created []*crypto_invoice.CryptoInvoice
}

func (s *invoiceDocumentServiceStub) Create(_ context.Context, invoice *crypto_invoice.CryptoInvoice) error {
	invoice.PublicRef = "inv_0123456789ABCDEFGHJKMNPQRS"
	s.created = append(s.created, invoice)
	return nil
}
func (s *invoiceDocumentServiceStub) GetByID(context.Context, id.ID) (*crypto_invoice.CryptoInvoice, error) {
	return nil, nil
}
func (s *invoiceDocumentServiceStub) GetByIDForUpdate(context.Context, id.ID) (*crypto_invoice.CryptoInvoice, error) {
	return nil, nil
}
func (s *invoiceDocumentServiceStub) Update(context.Context, *crypto_invoice.CryptoInvoice) error {
	return nil
}
func (s *invoiceDocumentServiceStub) Delete(context.Context, id.ID) error { return nil }
func (s *invoiceDocumentServiceStub) Post(context.Context, id.ID) error   { return nil }
func (s *invoiceDocumentServiceStub) Unpost(context.Context, id.ID) error { return nil }
func (s *invoiceDocumentServiceStub) PostAndSave(context.Context, *crypto_invoice.CryptoInvoice) error {
	return nil
}
func (s *invoiceDocumentServiceStub) UpdateAndRepost(context.Context, *crypto_invoice.CryptoInvoice) error {
	return nil
}
func (s *invoiceDocumentServiceStub) SetDeletionMark(context.Context, id.ID, bool) error { return nil }
func (s *invoiceDocumentServiceStub) List(context.Context, domain.ListFilter) (domain.CursorListResult[*crypto_invoice.CryptoInvoice], error) {
	return domain.CursorListResult[*crypto_invoice.CryptoInvoice]{}, nil
}
func (s *invoiceDocumentServiceStub) ListIDs(context.Context, domain.ListFilter, int) ([]id.ID, error) {
	return nil, nil
}

func TestCheckoutPageRedeemOwnsAtomicInvoiceAndLinkTransition(t *testing.T) {
	fixedNow := time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC)
	repo := &checkoutPageRepoStub{
		link: RedeemablePaymentLink{
			ID: id.New(), MerchantID: id.New(), TokenID: id.New(), Amount: types.NewRawAmountFromInt64(2500000),
			Description: "order", MaxUses: 1, Status: "active", TTLMinutes: 30,
		},
		page:          CheckoutPageData{ID: id.New(), Status: "created"},
		markSucceeded: true,
	}
	invoices := &invoiceDocumentServiceStub{}
	service := NewCheckoutPageService(repo, invoices)
	service.now = func() time.Time { return fixedNow }
	txm := &immediateTxManager{}
	ctx := tenant.WithTxManager(context.Background(), txm)

	ref, page, err := service.RedeemPaymentLink(ctx, "pl_0123456789ABCDEFGHJKMNPQRS")
	if err != nil {
		t.Fatalf("redeem payment link: %v", err)
	}
	if ref == "" || page.Status != "created" || len(invoices.created) != 1 || repo.markCalls != 1 || txm.calls != 1 {
		t.Fatalf("unexpected redemption effects: ref=%q page=%s invoices=%d marks=%d tx=%d", ref, page.Status, len(invoices.created), repo.markCalls, txm.calls)
	}
	if !invoices.created[0].ExpiresAt.Equal(fixedNow.Add(30 * time.Minute)) {
		t.Fatalf("TTL was not frozen at first execution: %s", invoices.created[0].ExpiresAt)
	}
	if repo.viewCalls != 1 {
		t.Fatalf("created checkout should record one best-effort view, got %d", repo.viewCalls)
	}
}

func TestCheckoutPageRedeemRejectsInactiveLinkBeforeEconomicEffect(t *testing.T) {
	repo := &checkoutPageRepoStub{
		link:          RedeemablePaymentLink{ID: id.New(), Status: "disabled", TTLMinutes: 30},
		markSucceeded: true,
	}
	invoices := &invoiceDocumentServiceStub{}
	service := NewCheckoutPageService(repo, invoices)
	ctx := tenant.WithTxManager(context.Background(), &immediateTxManager{})

	_, _, err := service.RedeemPaymentLink(ctx, "pl_0123456789ABCDEFGHJKMNPQRS")
	requireAppErrorCode(t, err, "CONFLICT")
	if len(invoices.created) != 0 || repo.markCalls != 0 {
		t.Fatalf("inactive link produced economic effects: invoices=%d marks=%d", len(invoices.created), repo.markCalls)
	}
}
