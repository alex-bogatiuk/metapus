package cryptoapp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/core/types"
	"metapus/internal/domain"
	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/domain/documents/crypto_invoice"
)

func TestCreateMerchantInvoice_ReplayReturnsExistingInvoice(t *testing.T) {
	ctx := context.Background()
	merchantID := id.New()
	tokenID := id.New()
	apiKeyID := id.New()
	input := merchantInvoiceInput(merchantID, tokenID, apiKeyID, types.NewCryptoAmountFromInt64(11000000))
	existing := invoiceFromInput(input)

	repo := newFakeInvoiceRepo()
	repo.invoices[existing.ID] = existing
	svc := newTestMerchantInvoiceService(t, repo, &fakeInvoiceDocumentService{repo: repo}, nil)

	result, err := svc.CreateMerchantInvoice(ctx, input)
	require.NoError(t, err)
	require.False(t, result.Created)
	require.Equal(t, existing.ID, result.Invoice.ID)
}

type testMerchantInvoiceSettings struct{ value MerchantInvoiceSettings }

func (s *testMerchantInvoiceSettings) GetMerchantInvoiceSettings(context.Context, id.ID) (MerchantInvoiceSettings, error) {
	return s.value, nil
}

func TestCreateMerchantInvoice_WeekDefaultExplicitOverrideAndLimit(t *testing.T) {
	now := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	repo := newFakeInvoiceRepo()
	settings := &testMerchantInvoiceSettings{MerchantInvoiceSettings{DefaultTTLMinutes: 10080, SinglePaymentPerInvoice: true}}
	svc, err := NewMerchantInvoiceService(MerchantInvoiceServiceConfig{
		InvoiceService: &fakeInvoiceDocumentService{repo: repo}, InvoiceRepo: repo,
		Settings: settings, IsUniqueViolation: func(error) bool { return false }, Now: func() time.Time { return now },
	})
	require.NoError(t, err)
	input := merchantInvoiceInput(id.New(), id.New(), id.New(), types.NewCryptoAmountFromInt64(11_000_000))
	input.TTLMinutes = 0
	input.ExpiresAt = time.Time{}
	created, err := svc.CreateMerchantInvoice(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, 10080, created.Invoice.TTLMinutes)
	require.Equal(t, now.Add(7*24*time.Hour), created.Invoice.ExpiresAt)
	require.True(t, created.Invoice.SinglePayment)

	settings.value = MerchantInvoiceSettings{DefaultTTLMinutes: 60}
	replayed, err := svc.CreateMerchantInvoice(context.Background(), input)
	require.NoError(t, err)
	require.False(t, replayed.Created)
	require.Equal(t, created.Invoice.ID, replayed.Invoice.ID)

	input.OrderID, input.ExternalID = "next-order", "next-idempotency-key"
	input.TTLMinutes = 10080
	explicit, err := svc.CreateMerchantInvoice(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, 10080, explicit.Invoice.TTLMinutes)
	require.False(t, explicit.Invoice.SinglePayment)

	input.OrderID, input.ExternalID = "bad-order", "bad-idempotency-key"
	input.TTLMinutes = 10081
	_, err = svc.CreateMerchantInvoice(context.Background(), input)
	var appErr *apperror.AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, apperror.CodeValidation, appErr.Code)
}

func TestCreateMerchantInvoice_ReplayWithDifferentPayloadReturnsConflict(t *testing.T) {
	ctx := context.Background()
	merchantID := id.New()
	tokenID := id.New()
	apiKeyID := id.New()
	input := merchantInvoiceInput(merchantID, tokenID, apiKeyID, types.NewCryptoAmountFromInt64(11000000))
	existing := invoiceFromInput(input)
	existing.ExpectedAmount = types.NewCryptoAmountFromInt64(12000000)

	repo := newFakeInvoiceRepo()
	repo.invoices[existing.ID] = existing
	svc := newTestMerchantInvoiceService(t, repo, &fakeInvoiceDocumentService{repo: repo}, nil)

	result, err := svc.CreateMerchantInvoice(ctx, input)
	require.Nil(t, result)
	var appErr *apperror.AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, apperror.CodeConflict, appErr.Code)
}

func TestCreateMerchantInvoice_OrderReplayAllowsNewRequestIdempotencyKey(t *testing.T) {
	ctx := context.Background()
	input := merchantInvoiceInput(id.New(), id.New(), id.New(), types.NewCryptoAmountFromInt64(11_000_000))
	existing := invoiceFromInput(input)

	repo := newFakeInvoiceRepo()
	repo.invoices[existing.ID] = existing
	svc := newTestMerchantInvoiceService(t, repo, &fakeInvoiceDocumentService{repo: repo}, nil)

	input.ExternalID = "another-request-idempotency-key"
	result, err := svc.CreateMerchantInvoice(ctx, input)
	require.NoError(t, err)
	require.False(t, result.Created)
	require.Equal(t, existing.ID, result.Invoice.ID)
}

func TestCreateMerchantInvoice_PortalReplayUsesSharedIdempotencyPath(t *testing.T) {
	ctx := context.Background()
	input := CreateMerchantInvoiceInput{
		MerchantID:     id.New(),
		TokenID:        id.New(),
		Amount:         types.NewCryptoAmountFromInt64(11_000_000),
		CreatedByID:    id.New(),
		CreationSource: crypto_invoice.CreationSourcePortal,
		ExpiresAt:      time.Now().Add(time.Hour),
		OrderID:        "portal-order-1",
		Description:    "Portal E2E invoice",
		CustomerID:     "customer@example.test",
	}
	repo := newFakeInvoiceRepo()
	docSvc := &fakeInvoiceDocumentService{repo: repo}
	svc := newTestMerchantInvoiceService(t, repo, docSvc, nil)

	created, err := svc.CreateMerchantInvoice(ctx, input)
	require.NoError(t, err)
	require.True(t, created.Created)
	require.Equal(t, crypto_invoice.CreationSourcePortal, created.Invoice.CreationSource)
	require.Equal(t, input.CreatedByID, created.Invoice.CreatedBy)
	require.Nil(t, created.Invoice.APIKeyID)

	replayed, err := svc.CreateMerchantInvoice(ctx, input)
	require.NoError(t, err)
	require.False(t, replayed.Created)
	require.Equal(t, created.Invoice.ID, replayed.Invoice.ID)
	require.Len(t, repo.invoices, 1)
}

func TestCreateMerchantInvoice_ConcurrentOrderReplayConverges(t *testing.T) {
	ctx := context.Background()
	input := merchantInvoiceInput(id.New(), id.New(), id.New(), types.NewCryptoAmountFromInt64(11_000_000))
	input.ExternalID = ""
	errUnique := errors.New("unique violation")

	baseRepo := newFakeInvoiceRepo()
	repo := &racingInvoiceRepo{
		fakeInvoiceRepo: baseRepo,
		initialFinds:    make(chan struct{}),
		uniqueErr:       errUnique,
	}
	docSvc := &racingInvoiceDocumentService{
		fakeInvoiceDocumentService: &fakeInvoiceDocumentService{repo: baseRepo},
		repo:                       repo,
	}
	svc, err := NewMerchantInvoiceService(MerchantInvoiceServiceConfig{
		InvoiceService: docSvc,
		InvoiceRepo:    repo,
		IsUniqueViolation: func(err error) bool {
			return errors.Is(err, errUnique)
		},
	})
	require.NoError(t, err)

	results := make([]*CreateMerchantInvoiceResult, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			results[index], errs[index] = svc.CreateMerchantInvoice(ctx, input)
		}(i)
	}
	wg.Wait()

	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.Equal(t, results[0].Invoice.ID, results[1].Invoice.ID)
	require.NotEqual(t, results[0].Created, results[1].Created)
	require.Len(t, baseRepo.invoices, 1)
}

func TestCreateMerchantInvoice_UniqueViolationRereadsExistingInvoice(t *testing.T) {
	ctx := context.Background()
	merchantID := id.New()
	tokenID := id.New()
	apiKeyID := id.New()
	input := merchantInvoiceInput(merchantID, tokenID, apiKeyID, types.NewCryptoAmountFromInt64(11000000))
	existing := invoiceFromInput(input)
	errUnique := errors.New("unique violation")

	repo := newFakeInvoiceRepo()
	repo.invoices[existing.ID] = existing
	repo.hideFinds = 2
	docSvc := &fakeInvoiceDocumentService{repo: repo, createErr: errUnique}
	svc := newTestMerchantInvoiceService(t, repo, docSvc, errUnique)

	result, err := svc.CreateMerchantInvoice(ctx, input)
	require.NoError(t, err)
	require.False(t, result.Created)
	require.Equal(t, existing.ID, result.Invoice.ID)
	require.Equal(t, 1, docSvc.createCalls)
}

func TestCreateMerchantInvoice_PoolDepletionCanRetryWithoutDuplicate(t *testing.T) {
	ctx := context.Background()
	input := merchantInvoiceInput(
		id.New(),
		id.New(),
		id.New(),
		types.NewCryptoAmountFromInt64(11_000_000),
	)
	repo := newFakeInvoiceRepo()
	poolErr := wallet.NewWalletPoolExhausted(id.New().String(), 30)
	docSvc := &fakeInvoiceDocumentService{repo: repo, createErr: poolErr}
	svc := newTestMerchantInvoiceService(t, repo, docSvc, nil)

	result, err := svc.CreateMerchantInvoice(ctx, input)
	require.Nil(t, result)
	require.ErrorIs(t, err, poolErr)
	require.Empty(t, repo.invoices, "depletion must not persist an invoice")

	docSvc.createErr = nil
	result, err = svc.CreateMerchantInvoice(ctx, input)
	require.NoError(t, err)
	require.True(t, result.Created)
	require.Len(t, repo.invoices, 1)

	replayed, err := svc.CreateMerchantInvoice(ctx, input)
	require.NoError(t, err)
	require.False(t, replayed.Created)
	require.Equal(t, result.Invoice.ID, replayed.Invoice.ID)
	require.Len(t, repo.invoices, 1)
}

func TestCancelMerchantInvoiceOwnsTransactionAndWalletRelease(t *testing.T) {
	merchantID := id.New()
	invoice := crypto_invoice.NewCryptoInvoice(merchantID, id.New(), types.NewCryptoAmountFromInt64(10))
	repo := newFakeInvoiceRepo()
	repo.invoices[invoice.ID] = invoice
	lifecycle := &fakeInvoiceCancellationLifecycle{cancelled: true}
	documents := &fakeInvoiceDocumentService{repo: repo}
	svc, err := NewMerchantInvoiceService(MerchantInvoiceServiceConfig{
		InvoiceService:    documents,
		InvoiceRepo:       repo,
		InvoiceLifecycle:  lifecycle,
		IsUniqueViolation: func(error) bool { return false },
	})
	require.NoError(t, err)
	txm := &merchantInvoiceTestTxManager{}
	ctx := tenant.WithTxManager(context.Background(), txm)

	require.NoError(t, svc.CancelMerchantInvoice(ctx, merchantID, invoice.ID))
	require.Equal(t, 1, txm.calls)
	require.Equal(t, invoice.ID, lifecycle.invoiceID)
	require.Equal(t, 1, repo.walletLocks)
	require.Equal(t, 1, repo.walletReleases)
}

func TestCancelMerchantInvoiceHidesForeignInvoice(t *testing.T) {
	invoice := crypto_invoice.NewCryptoInvoice(id.New(), id.New(), types.NewCryptoAmountFromInt64(10))
	repo := newFakeInvoiceRepo()
	repo.invoices[invoice.ID] = invoice
	svc, err := NewMerchantInvoiceService(MerchantInvoiceServiceConfig{
		InvoiceService:    &fakeInvoiceDocumentService{repo: repo},
		InvoiceRepo:       repo,
		InvoiceLifecycle:  &fakeInvoiceCancellationLifecycle{cancelled: true},
		IsUniqueViolation: func(error) bool { return false },
	})
	require.NoError(t, err)

	err = svc.CancelMerchantInvoice(tenant.WithTxManager(context.Background(), &merchantInvoiceTestTxManager{}), id.New(), invoice.ID)
	var appErr *apperror.AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, apperror.CodeNotFound, appErr.Code)
	require.Zero(t, repo.walletLocks)
}

type activationInvoiceRepo struct {
	*fakeInvoiceRepo
	payments map[id.ID][]id.ID
}

func (r *activationInvoiceRepo) ListActiveMerchantInvoiceIDs(_ context.Context, merchantID id.ID) ([]id.ID, error) {
	var ids []id.ID
	for invoiceID, invoice := range r.invoices {
		if invoice.MerchantID == merchantID {
			ids = append(ids, invoiceID)
		}
	}
	return ids, nil
}

func (r *activationInvoiceRepo) FirstTwoPaymentIDs(_ context.Context, invoiceID id.ID) ([]id.ID, error) {
	return r.payments[invoiceID], nil
}

type activationLifecycleStub struct{ closed map[id.ID]bool }

func (s activationLifecycleStub) SyncInTransaction(_ context.Context, invoiceID id.ID) (bool, error) {
	return s.closed[invoiceID], nil
}

func TestEnableSinglePaymentForActiveInvoicesSkipsMultiplePayments(t *testing.T) {
	merchantID := id.New()
	base := newFakeInvoiceRepo()
	repo := &activationInvoiceRepo{fakeInvoiceRepo: base, payments: make(map[id.ID][]id.ID)}
	empty := crypto_invoice.NewCryptoInvoice(merchantID, id.New(), types.NewCryptoAmountFromInt64(10))
	one := crypto_invoice.NewCryptoInvoice(merchantID, id.New(), types.NewCryptoAmountFromInt64(10))
	one.Status = crypto_invoice.InvoiceStatusPartiallyPaid
	multiple := crypto_invoice.NewCryptoInvoice(merchantID, id.New(), types.NewCryptoAmountFromInt64(10))
	multiple.Status = crypto_invoice.InvoiceStatusPartiallyPaid
	for _, invoice := range []*crypto_invoice.CryptoInvoice{empty, one, multiple} {
		base.invoices[invoice.ID] = invoice
	}
	repo.payments[one.ID] = []id.ID{id.New()}
	repo.payments[multiple.ID] = []id.ID{id.New(), id.New()}
	svc, err := NewMerchantInvoiceService(MerchantInvoiceServiceConfig{
		InvoiceService: &fakeInvoiceDocumentService{repo: base}, InvoiceRepo: repo,
		ActivationLifecycle: activationLifecycleStub{closed: map[id.ID]bool{one.ID: true}},
		IsUniqueViolation:   func(error) bool { return false },
	})
	require.NoError(t, err)
	require.NoError(t, svc.EnableSinglePaymentForActiveInvoices(context.Background(), merchantID))
	require.True(t, empty.SinglePayment)
	require.Nil(t, empty.SelectedPaymentID)
	require.True(t, one.SinglePayment)
	require.Equal(t, repo.payments[one.ID][0], *one.SelectedPaymentID)
	require.False(t, multiple.SinglePayment)
	require.Equal(t, 1, base.walletReleases)
}

func newTestMerchantInvoiceService(
	t *testing.T,
	repo *fakeInvoiceRepo,
	docSvc *fakeInvoiceDocumentService,
	errUnique error,
) *MerchantInvoiceService {
	t.Helper()

	svc, err := NewMerchantInvoiceService(MerchantInvoiceServiceConfig{
		InvoiceService: docSvc,
		InvoiceRepo:    repo,
		IsUniqueViolation: func(err error) bool {
			return errUnique != nil && errors.Is(err, errUnique)
		},
	})
	require.NoError(t, err)
	return svc
}

func merchantInvoiceInput(merchantID, tokenID, apiKeyID id.ID, amount types.CryptoAmount) CreateMerchantInvoiceInput {
	keyID := apiKeyID
	return CreateMerchantInvoiceInput{
		MerchantID:      merchantID,
		TokenID:         tokenID,
		Amount:          amount,
		APIKeyID:        &keyID,
		CreationSource:  crypto_invoice.CreationSourceMerchantAPI,
		ExpiresAt:       time.Now().Add(time.Hour),
		TTLMinutes:      60,
		OrderID:         "order-1",
		ExternalID:      "idem-1",
		Description:     "E2E invoice",
		NotificationURL: "https://merchant.example/callback",
		CustomerID:      "customer@example.test",
	}
}

func invoiceFromInput(in CreateMerchantInvoiceInput) *crypto_invoice.CryptoInvoice {
	inv := crypto_invoice.NewCryptoInvoice(in.MerchantID, in.TokenID, in.Amount)
	if in.APIKeyID != nil {
		keyID := *in.APIKeyID
		inv.APIKeyID = &keyID
	}
	inv.CreationSource = in.CreationSource
	inv.CreatedBy = in.CreatedByID
	inv.UpdatedBy = in.CreatedByID
	inv.ExpiresAt = in.ExpiresAt
	inv.TTLMinutes = in.TTLMinutes
	inv.OrderID = in.OrderID
	inv.ExternalID = in.ExternalID
	inv.Description = in.Description
	inv.NotificationURL = in.NotificationURL
	inv.CustomerID = in.CustomerID
	return inv
}

type fakeInvoiceRepo struct {
	invoices       map[id.ID]*crypto_invoice.CryptoInvoice
	hideFinds      int
	findCalls      int
	walletLocks    int
	walletReleases int
}

func newFakeInvoiceRepo() *fakeInvoiceRepo {
	return &fakeInvoiceRepo{invoices: make(map[id.ID]*crypto_invoice.CryptoInvoice)}
}

func (r *fakeInvoiceRepo) Create(_ context.Context, doc *crypto_invoice.CryptoInvoice) error {
	r.invoices[doc.ID] = doc
	return nil
}

func (r *fakeInvoiceRepo) GetByID(_ context.Context, docID id.ID) (*crypto_invoice.CryptoInvoice, error) {
	doc := r.invoices[docID]
	if doc == nil {
		return nil, apperror.NewNotFound("crypto_invoice", docID.String())
	}
	return doc, nil
}

func (r *fakeInvoiceRepo) GetByNumber(context.Context, string) (*crypto_invoice.CryptoInvoice, error) {
	return nil, apperror.NewNotFound("crypto_invoice", "")
}

func (r *fakeInvoiceRepo) Update(_ context.Context, doc *crypto_invoice.CryptoInvoice) error {
	r.invoices[doc.ID] = doc
	return nil
}

func (r *fakeInvoiceRepo) Delete(context.Context, id.ID) error { return nil }

func (r *fakeInvoiceRepo) List(context.Context, domain.ListFilter) (domain.CursorListResult[*crypto_invoice.CryptoInvoice], error) {
	return domain.CursorListResult[*crypto_invoice.CryptoInvoice]{}, nil
}

func (r *fakeInvoiceRepo) ListIDs(context.Context, domain.ListFilter, int) ([]id.ID, error) {
	return nil, nil
}

func (r *fakeInvoiceRepo) FindByExternalID(_ context.Context, externalID string) (*crypto_invoice.CryptoInvoice, error) {
	for _, inv := range r.invoices {
		if inv.ExternalID == externalID {
			return inv, nil
		}
	}
	return nil, nil
}

func (r *fakeInvoiceRepo) FindByPublicRef(_ context.Context, publicRef string) (*crypto_invoice.CryptoInvoice, error) {
	for _, inv := range r.invoices {
		if inv.PublicRef == publicRef {
			return inv, nil
		}
	}
	return nil, nil
}

func (r *fakeInvoiceRepo) FindByMerchantOrderID(_ context.Context, merchantID id.ID, orderID string) (*crypto_invoice.CryptoInvoice, error) {
	if r.shouldHideFind() {
		return nil, nil
	}
	for _, inv := range r.invoices {
		if inv.MerchantID == merchantID && inv.OrderID == orderID {
			return inv, nil
		}
	}
	return nil, nil
}

func (r *fakeInvoiceRepo) FindByMerchantExternalID(_ context.Context, merchantID id.ID, externalID string) (*crypto_invoice.CryptoInvoice, error) {
	if r.shouldHideFind() {
		return nil, nil
	}
	for _, inv := range r.invoices {
		if inv.MerchantID == merchantID && inv.ExternalID == externalID {
			return inv, nil
		}
	}
	return nil, nil
}

func (r *fakeInvoiceRepo) shouldHideFind() bool {
	r.findCalls++
	return r.findCalls <= r.hideFinds
}

func (r *fakeInvoiceRepo) GetByIDForUpdate(ctx context.Context, docID id.ID) (*crypto_invoice.CryptoInvoice, error) {
	return r.GetByID(ctx, docID)
}
func (r *fakeInvoiceRepo) FindOverdueIDs(context.Context, int) ([]id.ID, error) { return nil, nil }
func (r *fakeInvoiceRepo) LockWalletForInvoice(context.Context, id.ID) error {
	r.walletLocks++
	return nil
}
func (r *fakeInvoiceRepo) ReleaseInvoiceWallet(context.Context, id.ID, string) error {
	r.walletReleases++
	return nil
}
func (r *fakeInvoiceRepo) CleanupOrphanedExpiredLeases(context.Context) error { return nil }

type fakeInvoiceDocumentService struct {
	repo        *fakeInvoiceRepo
	createErr   error
	createCalls int
}

type racingInvoiceRepo struct {
	*fakeInvoiceRepo
	mu               sync.Mutex
	initialFinds     chan struct{}
	initialFindCount int
	uniqueErr        error
}

func (r *racingInvoiceRepo) FindByMerchantOrderID(_ context.Context, merchantID id.ID, orderID string) (*crypto_invoice.CryptoInvoice, error) {
	r.mu.Lock()
	r.initialFindCount++
	findNumber := r.initialFindCount
	if findNumber == 2 {
		close(r.initialFinds)
	}
	r.mu.Unlock()
	if findNumber <= 2 {
		<-r.initialFinds
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for _, inv := range r.invoices {
		if inv.MerchantID == merchantID && inv.OrderID == orderID {
			return inv, nil
		}
	}
	return nil, nil
}

func (r *racingInvoiceRepo) Create(_ context.Context, doc *crypto_invoice.CryptoInvoice) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, inv := range r.invoices {
		if inv.MerchantID == doc.MerchantID && inv.OrderID == doc.OrderID {
			return r.uniqueErr
		}
	}
	r.invoices[doc.ID] = doc
	return nil
}

type racingInvoiceDocumentService struct {
	*fakeInvoiceDocumentService
	repo *racingInvoiceRepo
}

func (s *racingInvoiceDocumentService) Create(ctx context.Context, doc *crypto_invoice.CryptoInvoice) error {
	return s.repo.Create(ctx, doc)
}

func (s *fakeInvoiceDocumentService) Create(ctx context.Context, doc *crypto_invoice.CryptoInvoice) error {
	s.createCalls++
	if s.createErr != nil {
		return s.createErr
	}
	return s.repo.Create(ctx, doc)
}

func (s *fakeInvoiceDocumentService) GetByID(ctx context.Context, entityID id.ID) (*crypto_invoice.CryptoInvoice, error) {
	return s.repo.GetByID(ctx, entityID)
}

func (s *fakeInvoiceDocumentService) GetByIDForUpdate(ctx context.Context, entityID id.ID) (*crypto_invoice.CryptoInvoice, error) {
	return s.repo.GetByIDForUpdate(ctx, entityID)
}

func (s *fakeInvoiceDocumentService) Update(ctx context.Context, doc *crypto_invoice.CryptoInvoice) error {
	return s.repo.Update(ctx, doc)
}

func (s *fakeInvoiceDocumentService) Delete(context.Context, id.ID) error { return nil }
func (s *fakeInvoiceDocumentService) Post(context.Context, id.ID) error   { return nil }
func (s *fakeInvoiceDocumentService) Unpost(context.Context, id.ID) error { return nil }
func (s *fakeInvoiceDocumentService) PostAndSave(ctx context.Context, doc *crypto_invoice.CryptoInvoice) error {
	return s.Create(ctx, doc)
}
func (s *fakeInvoiceDocumentService) UpdateAndRepost(ctx context.Context, doc *crypto_invoice.CryptoInvoice) error {
	return s.Update(ctx, doc)
}
func (s *fakeInvoiceDocumentService) SetDeletionMark(context.Context, id.ID, bool) error {
	return nil
}
func (s *fakeInvoiceDocumentService) List(context.Context, domain.ListFilter) (domain.CursorListResult[*crypto_invoice.CryptoInvoice], error) {
	return domain.CursorListResult[*crypto_invoice.CryptoInvoice]{}, nil
}
func (s *fakeInvoiceDocumentService) ListIDs(context.Context, domain.ListFilter, int) ([]id.ID, error) {
	return nil, nil
}

var _ crypto_invoice.Repository = (*fakeInvoiceRepo)(nil)
var _ domain.DocumentService[*crypto_invoice.CryptoInvoice] = (*fakeInvoiceDocumentService)(nil)

type fakeInvoiceCancellationLifecycle struct {
	cancelled bool
	err       error
	invoiceID id.ID
}

func (l *fakeInvoiceCancellationLifecycle) CancelInTransaction(_ context.Context, invoiceID id.ID) (bool, error) {
	l.invoiceID = invoiceID
	return l.cancelled, l.err
}

type merchantInvoiceTestTxManager struct{ calls int }

func (m *merchantInvoiceTestTxManager) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	m.calls++
	return fn(ctx)
}
