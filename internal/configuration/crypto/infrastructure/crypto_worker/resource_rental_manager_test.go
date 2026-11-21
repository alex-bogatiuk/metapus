package crypto_worker

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/application/outboundapp"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/domain/documents/crypto_withdrawal"
	"metapus/internal/configuration/crypto/domain/documents/withdrawal_request"
	"metapus/internal/configuration/crypto/domain/registers/crypto_platform_cost"
	corecrypto "metapus/internal/core/crypto"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

type fakeSharedResourceStore struct {
	providerAccountID id.ID
	costTokenID       id.ID
	rental            *resourceRentalRecord
	allocations       map[id.ID]*resourceAllocationRecord
	reserved          int64
	createCount       int
	activatedCost     types.RawAmount
	closedStatus      string
	closedRefund      types.RawAmount
	rentalBasisType   string
	rentalBasisID     id.ID
	accounts          map[id.ID]*providerAccountRecord
	accountOrder      []id.ID
	accountListErr    error
	maintenanceClaim  bool
	renewCalls        int
	renewErrAt        int
	claimCloseCalls   int
	claimCloseErr     error
	claimHeldCalls    int
}

func (s *fakeSharedResourceStore) GetProviderAccount(_ context.Context, accountID id.ID) (*providerAccountRecord, error) {
	return s.accounts[accountID], nil
}
func (s *fakeSharedResourceStore) GetProviderAccountForRental(ctx context.Context, accountID id.ID) (*providerAccountRecord, error) {
	return s.GetProviderAccount(ctx, accountID)
}
func (s *fakeSharedResourceStore) GetActiveNetworkProviderAccounts(_ context.Context, networkID id.ID) ([]*providerAccountRecord, error) {
	if s.accountListErr != nil {
		return nil, s.accountListErr
	}
	accounts := make([]*providerAccountRecord, 0, len(s.accounts))
	for _, account := range s.accounts {
		if id.IsNil(account.NetworkID) {
			account.NetworkID = networkID
		}
		if account.NetworkID == networkID && account.IsActive && !account.IsFolder && len(account.CredentialsEnc) > 0 {
			accounts = append(accounts, account)
		}
	}
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].Name < accounts[j].Name })
	return accounts, nil
}
func (s *fakeSharedResourceStore) GetResourceAllocationByBasis(_ context.Context, _ string, basisID id.ID) (*resourceAllocationRecord, error) {
	return s.allocations[basisID], nil
}
func (s *fakeSharedResourceStore) GetOpenWalletRental(context.Context, id.ID, id.ID) (*resourceRentalRecord, error) {
	return s.rental, nil
}
func (s *fakeSharedResourceStore) TryReserveResource(_ context.Context, req ResourceRequest, required int64, _ time.Duration) (*resourceAllocationRecord, *resourceRentalRecord, error) {
	if existing := s.allocations[req.BasisID]; existing != nil {
		return existing, s.rental, nil
	}
	if s.rental == nil || s.rental.Status != "active" {
		return nil, s.rental, ErrResourceCapacityInsufficient
	}
	if s.rental.DelegatedAmount-s.reserved < required {
		return nil, s.rental, ErrResourceCapacityInsufficient
	}
	allocation := &resourceAllocationRecord{ID: id.New(), RentalID: s.rental.ID, ReservedAmount: required}
	s.allocations[req.BasisID] = allocation
	s.reserved += required
	return allocation, s.rental, nil
}
func (s *fakeSharedResourceStore) CreateResourceRental(_ context.Context, rental *resourceRentalRecord, req ResourceRequest, _ []byte) error {
	copy := *rental
	s.rental = &copy
	s.createCount++
	s.rentalBasisType = req.RentalBasisType
	s.rentalBasisID = req.RentalBasisID
	return nil
}
func (s *fakeSharedResourceStore) ActivateResourceRental(_ context.Context, rentalID id.ID, serial string, delegated int64, cost types.RawAmount, _ *types.RawAmount, _ []byte, expiresAt time.Time) error {
	s.rental.ID = rentalID
	s.rental.ExternalSerial = serial
	s.rental.DelegatedAmount = delegated
	s.rental.GrossCostAmount = cost
	s.activatedCost = cost
	s.rental.Status = "active"
	s.rental.ExpiresAt = &expiresAt
	return nil
}
func (*fakeSharedResourceStore) MarkResourceRentalHeld(context.Context, id.ID, string, []byte) error {
	return nil
}
func (*fakeSharedResourceStore) MarkResourceRentalReclaiming(context.Context, id.ID) error {
	return nil
}
func (s *fakeSharedResourceStore) CloseResourceRental(_ context.Context, _ id.ID, refund types.RawAmount, _ *types.RawAmount, _ []byte, status string) error {
	s.closedStatus = status
	s.closedRefund = refund
	s.rental.Status = status
	return nil
}
func (*fakeSharedResourceStore) MarkResourceAllocationBroadcast(context.Context, ResourceBroadcastCommand) error {
	return nil
}
func (*fakeSharedResourceStore) SettleResourceAllocation(context.Context, string, crypto.ChainReceipt) error {
	return nil
}
func (*fakeSharedResourceStore) ReleaseResourceAllocation(context.Context, id.ID, string) error {
	return nil
}
func (s *fakeSharedResourceStore) ClaimResourceRentalsForMaintenance(context.Context, string, int) ([]*resourceRentalRecord, error) {
	if s.maintenanceClaim && s.rental != nil {
		s.rental.MaintenanceClaimToken = id.New()
		return []*resourceRentalRecord{s.rental}, nil
	}
	return nil, nil
}
func (*fakeSharedResourceStore) ReleaseResourceRentalClaim(context.Context, outboundapp.ResourceRentalClaim) error {
	return nil
}
func (s *fakeSharedResourceStore) RenewResourceRentalClaim(_ context.Context, c outboundapp.ResourceRentalClaim) error {
	s.renewCalls++
	if id.IsNil(c.Token) || s.renewCalls == s.renewErrAt {
		return errors.New("stale maintenance claim")
	}
	return nil
}
func (s *fakeSharedResourceStore) MarkResourceRentalClaimReclaiming(ctx context.Context, claim outboundapp.ResourceRentalClaim) error {
	return s.MarkResourceRentalReclaiming(ctx, claim.RentalID)
}
func (s *fakeSharedResourceStore) MarkResourceRentalClaimHeld(ctx context.Context, claim outboundapp.ResourceRentalClaim, reason string, body []byte) error {
	s.claimHeldCalls++
	return s.MarkResourceRentalHeld(ctx, claim.RentalID, reason, body)
}
func (s *fakeSharedResourceStore) CloseResourceRentalClaim(ctx context.Context, claim outboundapp.ResourceRentalClaim, refund types.RawAmount, balance *types.RawAmount, body []byte, status string) error {
	s.claimCloseCalls++
	if s.claimCloseErr != nil {
		return s.claimCloseErr
	}
	return s.CloseResourceRental(ctx, claim.RentalID, refund, balance, body, status)
}

type fakeResourceProvider struct {
	code         string
	placeCalls   int
	queryCalls   int
	placeErr     error
	queryErr     error
	queryOrder   crypto.ResourceOrder
	placeOrder   crypto.ResourceOrder
	reclaimCalls int
}

func (p *fakeResourceProvider) Code() string { return p.code }
func (p *fakeResourceProvider) Capabilities() crypto.ExternalResourceCapabilities {
	return crypto.ExternalResourceCapabilities{
		Code: p.code, Family: crypto.ChainFamilyTRON, ResourceKinds: []crypto.ResourceKind{crypto.ResourceKindEnergy},
		ReusableFixedLease: true, SupportsIdempotentPlace: true, SupportsQueryByClientOrderID: true, SupportsReclaim: true,
		DefaultPeriod: "1H", DefaultDuration: time.Hour, MinQuantity: 1,
	}
}
func (*fakeResourceProvider) Quote(context.Context, crypto.ResourceOrderIntent) (crypto.ResourceQuote, error) {
	return crypto.ResourceQuote{}, nil
}
func (p *fakeResourceProvider) Place(_ context.Context, intent crypto.ResourceOrderIntent) (crypto.ResourceOrder, error) {
	p.placeCalls++
	if p.placeErr != nil {
		return crypto.ResourceOrder{}, p.placeErr
	}
	if p.placeOrder.ExternalID != "" {
		order := p.placeOrder
		order.ClientOrderID = intent.ClientOrderID
		order.Provider = p.code
		return order, nil
	}
	balance := types.NewRawAmountFromInt64(900000)
	return crypto.ResourceOrder{
		Provider: p.code, ClientOrderID: intent.ClientOrderID, ExternalID: "lease-1",
		Status: crypto.ResourceOrderActive, DelegatedAmount: intent.Quantity,
		CostAmount: types.NewRawAmountFromInt64(250000), ProviderBalance: &balance,
	}, nil
}
func (p *fakeResourceProvider) Query(context.Context, crypto.ResourceOrderRef) (crypto.ResourceOrder, error) {
	p.queryCalls++
	if p.queryErr != nil {
		return crypto.ResourceOrder{}, p.queryErr
	}
	order := p.queryOrder
	order.Provider = p.code
	return order, nil
}
func (p *fakeResourceProvider) Reclaim(context.Context, crypto.ResourceOrderRef) (crypto.ResourceOrder, error) {
	p.reclaimCalls++
	return crypto.ResourceOrder{Provider: p.code, Status: crypto.ResourceOrderReclaiming}, nil
}
func (*fakeResourceProvider) Balance(context.Context, crypto.ProviderAccountRef) (types.RawAmount, error) {
	return types.ZeroRawAmount(), nil
}

func testPreparer(store *fakeSharedResourceStore, providers ...crypto.ExternalResourceProvider) *SharedResourcePreparer {
	if store.accounts == nil {
		store.accounts = make(map[id.ID]*providerAccountRecord, len(providers))
		for i, provider := range providers {
			accountID := id.New()
			if i == 0 && !id.IsNil(store.providerAccountID) {
				accountID = store.providerAccountID
			}
			store.accounts[accountID] = &providerAccountRecord{ID: accountID, Code: provider.Code(), Name: provider.Code(),
				Provider: provider.Code(), BalanceTokenID: store.costTokenID, IsActive: true}
			store.accountOrder = append(store.accountOrder, accountID)
		}
	}
	if store.rental != nil && id.IsNil(store.rental.ProviderAccountID) && len(store.accountOrder) > 0 {
		store.rental.ProviderAccountID = store.accountOrder[0]
	}
	preparer := &SharedResourcePreparer{sharedRepo: store, accountProviderFactory: func(context.Context, string, []byte) (crypto.ExternalResourceProvider, error) {
		return nil, nil
	}, targetEnergy: 65000, safetyMarginPct: 10, reservationCutoff: 2 * time.Minute}
	for _, account := range store.accounts {
		if id.IsNil(account.NetworkID) && store.rental != nil {
			account.NetworkID = store.rental.NetworkID
		}
		account.CredentialsEnc = []byte("test credentials")
		account.CredentialsVersion = 1
		for _, provider := range providers {
			if provider.Code() == account.Provider {
				preparer.accountProviders.Store(account.ID.String(), &cachedAccountProvider{version: 1, provider: account.Provider, adapter: provider})
			}
		}
	}
	return preparer
}

func testResourceRequest(networkID, walletID, tokenID, basisID id.ID) ResourceRequest {
	return ResourceRequest{
		TenantID: "tenant", BasisType: withdrawal_request.DocumentType, BasisID: basisID,
		Purpose: crypto_platform_cost.PlatformCostPurposeWithdrawal, NetworkID: networkID, OperationTokenID: tokenID,
		SourceWalletID: walletID, SourceAddress: "TSource", ContractAddress: "TContract",
		DestinationAddress: "TDest", Amount: types.NewRawAmountFromInt64(1_000_000),
		Requirements: []crypto.ResourceRequirement{{Name: resourceTypeEnergy, Quantity: 14000}},
	}
}

func TestPrepareResourceReusesOneRentalForTwoTransactions(t *testing.T) {
	store := &fakeSharedResourceStore{providerAccountID: id.New(), costTokenID: id.New(), allocations: map[id.ID]*resourceAllocationRecord{}}
	provider := &fakeResourceProvider{code: crypto.ResourceProviderITRX}
	preparer := testPreparer(store, provider)
	networkID, walletID, tokenID := id.New(), id.New(), id.New()
	first, err := preparer.PrepareResource(context.Background(), testResourceRequest(networkID, walletID, tokenID, id.New()))
	require.NoError(t, err)
	second, err := preparer.PrepareResource(context.Background(), testResourceRequest(networkID, walletID, tokenID, id.New()))
	require.NoError(t, err)
	assert.Equal(t, 1, provider.placeCalls)
	assert.Equal(t, 1, store.createCount)
	assert.Equal(t, first.RentalID, second.RentalID)
	assert.NotEqual(t, first.AllocationID, second.AllocationID)
	assert.Equal(t, int64(15400), first.ReservedUnits)
	assert.Equal(t, int64(30800), store.reserved)
}

func TestPrepareResourceRecoversOrderingRentalByOutTradeNo(t *testing.T) {
	networkID, walletID, tokenID := id.New(), id.New(), id.New()
	expires := time.Now().Add(time.Hour)
	store := &fakeSharedResourceStore{
		providerAccountID: id.New(), costTokenID: id.New(), allocations: map[id.ID]*resourceAllocationRecord{},
		rental: &resourceRentalRecord{ID: id.New(), NetworkID: networkID, ReceiveWalletID: walletID,
			RequestedAmount: 65000, Provider: crypto.ResourceProviderITRX, OutTradeNo: "rr-existing", Status: "ordering", ExpiresAt: &expires},
	}
	provider := &fakeResourceProvider{code: crypto.ResourceProviderITRX, queryOrder: crypto.ResourceOrder{
		ExternalID: "serial-existing", DelegatedAmount: 65000, CostAmount: types.NewRawAmountFromInt64(250000), Status: crypto.ResourceOrderActive,
	}}
	preparer := testPreparer(store, provider)
	result, err := preparer.PrepareResource(context.Background(), testResourceRequest(networkID, walletID, tokenID, id.New()))
	require.NoError(t, err)
	assert.Equal(t, 1, provider.queryCalls)
	assert.Equal(t, 0, provider.placeCalls)
	assert.Equal(t, "serial-existing", store.rental.ExternalSerial)
	assert.False(t, id.IsNil(result.AllocationID))
}

func TestPrepareResourceKeepsExistingOrderAmbiguousWhenRecoveryQueryFails(t *testing.T) {
	networkID, walletID, tokenID := id.New(), id.New(), id.New()
	expires := time.Now().Add(time.Hour)
	store := &fakeSharedResourceStore{
		providerAccountID: id.New(), costTokenID: id.New(), allocations: map[id.ID]*resourceAllocationRecord{},
		rental: &resourceRentalRecord{ID: id.New(), NetworkID: networkID, ReceiveWalletID: walletID,
			RequestedAmount: 65000, Provider: crypto.ResourceProviderITRX, OutTradeNo: "rr-ambiguous", Status: "held", ExpiresAt: &expires},
	}
	provider := &fakeResourceProvider{code: crypto.ResourceProviderITRX, queryErr: crypto.ErrResourceProviderTransient}
	preparer := testPreparer(store, provider)
	_, err := preparer.PrepareResource(context.Background(), testResourceRequest(networkID, walletID, tokenID, id.New()))
	require.ErrorIs(t, err, crypto.ErrResourceOrderAmbiguous)
	assert.Equal(t, 1, provider.queryCalls)
	assert.Equal(t, 0, provider.placeCalls)
}

func TestPrepareResourceRecordsFailedProviderChargeAndRefund(t *testing.T) {
	networkID, walletID, tokenID := id.New(), id.New(), id.New()
	expires := time.Now().Add(time.Hour)
	store := &fakeSharedResourceStore{providerAccountID: id.New(), costTokenID: id.New(), allocations: map[id.ID]*resourceAllocationRecord{}, rental: &resourceRentalRecord{ID: id.New(), NetworkID: networkID, ReceiveWalletID: walletID, RequestedAmount: 65000, Provider: crypto.ResourceProviderITRX, OutTradeNo: "rr-failed", Status: "ordering", ExpiresAt: &expires}}
	provider := &fakeResourceProvider{code: crypto.ResourceProviderITRX, queryOrder: crypto.ResourceOrder{
		ExternalID: "serial-failed", DelegatedAmount: 65000, CostAmount: types.NewRawAmountFromInt64(300000),
		RefundAmount: types.NewRawAmountFromInt64(100000), Status: crypto.ResourceOrderFailed,
	}}
	preparer := testPreparer(store, provider)
	_, err := preparer.PrepareResource(context.Background(), testResourceRequest(networkID, walletID, tokenID, id.New()))
	require.Error(t, err)
	assert.Equal(t, "300000", store.activatedCost.String())
	assert.Equal(t, "failed", store.closedStatus)
	assert.Equal(t, "100000", store.closedRefund.String())
}

func TestPrepareResourceKeepsRentalLinkedToExecutionDocument(t *testing.T) {
	store := &fakeSharedResourceStore{providerAccountID: id.New(), costTokenID: id.New(), allocations: map[id.ID]*resourceAllocationRecord{}}
	preparer := testPreparer(store, &fakeResourceProvider{code: crypto.ResourceProviderITRX})
	withdrawalID := id.New()
	_, err := preparer.PrepareResource(context.Background(), ResourceRequest{
		TenantID: "tenant", BasisType: crypto_withdrawal.DocumentType, BasisID: withdrawalID,
		RentalBasisType: crypto_withdrawal.DocumentType, RentalBasisID: withdrawalID,
		Purpose: crypto_platform_cost.PlatformCostPurposeWithdrawal, NetworkID: id.New(), OperationTokenID: id.New(),
		SourceWalletID: id.New(), SourceAddress: "TSource", ContractAddress: "TContract",
		DestinationAddress: "TDest", Amount: types.NewRawAmountFromInt64(1),
		Requirements: []crypto.ResourceRequirement{{Name: resourceTypeEnergy, Quantity: 14000}},
	})
	require.NoError(t, err)
	assert.Equal(t, crypto_withdrawal.DocumentType, store.rentalBasisType)
	assert.Equal(t, withdrawalID, store.rentalBasisID)
}

func TestPrepareResourceDoesNotFailOverAmbiguousOrder(t *testing.T) {
	store := &fakeSharedResourceStore{providerAccountID: id.New(), costTokenID: id.New(), allocations: map[id.ID]*resourceAllocationRecord{}}
	primary := &fakeResourceProvider{code: crypto.ResourceProviderITRX, placeErr: crypto.ErrResourceProviderTransient, queryErr: crypto.ErrResourceProviderTransient}
	fallback := &fakeResourceProvider{code: "tronsave"}
	preparer := testPreparer(store, primary, fallback)
	req := testResourceRequest(id.New(), id.New(), id.New(), id.New())
	req.ProviderAccountIDs = append([]id.ID(nil), store.accountOrder...)
	for _, account := range store.accounts {
		account.NetworkID = req.NetworkID
	}
	_, err := preparer.PrepareResource(context.Background(), req)
	require.ErrorIs(t, err, crypto.ErrResourceOrderAmbiguous)
	assert.Equal(t, 1, primary.placeCalls)
	assert.Equal(t, 0, fallback.placeCalls)
}

func TestPrepareResourceFailsoverTransientToNextProvider(t *testing.T) {
	store := &fakeSharedResourceStore{providerAccountID: id.New(), costTokenID: id.New(), allocations: map[id.ID]*resourceAllocationRecord{}}
	primary := &fakeResourceProvider{code: crypto.ResourceProviderITRX}
	primary.placeErr = crypto.ErrResourceProviderTransient
	fallback := &fakeResourceProvider{code: "tronsave"}
	preparer := testPreparer(store, primary, fallback)
	networkID := id.New()
	// Open the primary circuit so pick skips immediately... actually Place returns transient
	// before Create? placeWithProvider creates rental first then Place. Transient after create
	// is treated as ambiguous (query also fails). For true failover, circuit must be open
	// before Place so we never create an order.
	circuit := preparer.providerCircuit(networkID, store.accountOrder[0].String())
	circuit.failure(time.Now().UTC(), 1, time.Minute)
	req := testResourceRequest(networkID, id.New(), id.New(), id.New())
	req.ProviderAccountIDs = append([]id.ID(nil), store.accountOrder...)
	for _, account := range store.accounts {
		account.NetworkID = networkID
	}
	result, err := preparer.PrepareResource(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, 0, primary.placeCalls)
	assert.Equal(t, 1, fallback.placeCalls)
	assert.Equal(t, "tronsave", store.rental.Provider)
	assert.True(t, result.ResourceUsed)
}

func TestPrepareResourceFailsOverByProviderAccountIDs(t *testing.T) {
	networkID := id.New()
	costTokenID := id.New()
	acc1ID := id.New()
	acc2ID := id.New()

	store := &fakeSharedResourceStore{
		providerAccountID: acc1ID,
		costTokenID:       costTokenID,
		allocations:       map[id.ID]*resourceAllocationRecord{},
		accounts: map[id.ID]*providerAccountRecord{
			acc1ID: {
				ID:             acc1ID,
				Code:           "ITRX-PRIMARY",
				Name:           "iTRX Main",
				Provider:       crypto.ResourceProviderITRX,
				NetworkID:      networkID,
				BalanceTokenID: costTokenID,
				IsActive:       true,
			},
			acc2ID: {
				ID:             acc2ID,
				Code:           "ITRX-BACKUP",
				Name:           "iTRX Backup",
				Provider:       crypto.ResourceProviderITRX,
				NetworkID:      networkID,
				BalanceTokenID: costTokenID,
				IsActive:       true,
			},
		},
	}

	provider := &fakeResourceProvider{code: crypto.ResourceProviderITRX}
	preparer := testPreparer(store, provider)

	// Открываем circuit breaker для acc1ID
	circuit := preparer.providerCircuit(networkID, acc1ID.String())
	circuit.failure(time.Now().UTC(), 1, time.Minute)

	req := testResourceRequest(networkID, id.New(), id.New(), id.New())
	req.ProviderAccountIDs = []id.ID{acc1ID, acc2ID}

	result, err := preparer.PrepareResource(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, result.ResourceUsed)
	require.NotNil(t, store.rental)
	assert.Equal(t, acc2ID, store.rental.ProviderAccountID, "should fail over to secondary account")
}

func TestResourceProviderStatusesUseAccountCodeAndFailClosedOnCatalogError(t *testing.T) {
	networkID := id.New()
	store := &fakeSharedResourceStore{providerAccountID: id.New(), costTokenID: id.New()}
	preparer := testPreparer(store, &fakeResourceProvider{code: crypto.ResourceProviderITRX})
	statuses := preparer.ResourceProviderStatuses(context.Background(), networkID)
	require.Len(t, statuses, 1)
	require.Equal(t, crypto.ResourceProviderITRX, statuses[0].ProviderID)
	require.Equal(t, "closed", statuses[0].State)
	ready, state := preparer.ResourceProviderStatus(context.Background(), networkID)
	require.True(t, ready)
	require.Equal(t, "closed", state)

	store.accountListErr = errors.New("catalog unavailable")
	ready, state = preparer.ResourceProviderStatus(context.Background(), networkID)
	require.False(t, ready)
	require.Equal(t, "unavailable", state)
}

func TestResolveProviderForAccountReplacesRotatedAdapter(t *testing.T) {
	key := []byte("12345678901234567890123456789012")
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", string(key))
	t.Setenv("AUTOMATION_ENCRYPTION_KEY_FILE", "")
	enc, err := corecrypto.Encrypt([]byte(`{"apiKey":"first"}`), key)
	require.NoError(t, err)
	account := &providerAccountRecord{ID: id.New(), NetworkID: id.New(), Provider: crypto.ResourceProviderITRX,
		CredentialsEnc: enc, CredentialsVersion: 1}
	builds := 0
	preparer := &SharedResourcePreparer{accountProviderFactory: func(context.Context, string, []byte) (crypto.ExternalResourceProvider, error) {
		builds++
		return &fakeResourceProvider{code: crypto.ResourceProviderITRX}, nil
	}}
	_, err = preparer.resolveProviderForAccount(context.Background(), account)
	require.NoError(t, err)
	preparer.providerCircuit(account.NetworkID, account.ID.String()).failure(time.Now(), 1, time.Minute)
	_, err = preparer.resolveProviderForAccount(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, 1, builds)

	account.CredentialsVersion = 2
	account.CredentialsEnc, err = corecrypto.Encrypt([]byte(`{"apiKey":"second"}`), key)
	require.NoError(t, err)
	_, err = preparer.resolveProviderForAccount(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, 2, builds)
	require.Equal(t, "closed", preparer.providerCircuit(account.NetworkID, account.ID.String()).state(time.Now()))
	count := 0
	preparer.accountProviders.Range(func(_, _ any) bool { count++; return true })
	require.Equal(t, 1, count)
}

func TestMaintainResourcesUsesDisabledRentalAccount(t *testing.T) {
	networkID, accountID := id.New(), id.New()
	store := &fakeSharedResourceStore{providerAccountID: accountID, costTokenID: id.New(), maintenanceClaim: true,
		rental: &resourceRentalRecord{ID: id.New(), ProviderAccountID: accountID, NetworkID: networkID,
			Provider: crypto.ResourceProviderITRX, ExternalSerial: "lease-1", Status: "reclaiming"}}
	provider := &fakeResourceProvider{code: crypto.ResourceProviderITRX, queryOrder: crypto.ResourceOrder{Status: crypto.ResourceOrderClosed}}
	preparer := testPreparer(store, provider)
	store.accounts[accountID].IsActive = false
	require.NoError(t, preparer.MaintainResources(context.Background(), "worker"))
	require.Equal(t, 1, provider.queryCalls)
	require.Equal(t, "closed", store.closedStatus)
}

func TestMaintainResourcesRejectsStaleClaimBeforeExternalWorkAndClose(t *testing.T) {
	for _, tc := range []struct {
		name                                                        string
		renewErrAt, reclaimCalls, queryCalls, closeCalls, heldCalls int
		closeErr                                                    error
	}{
		{name: "expired while queued", renewErrAt: 1},
		{name: "expired during reclaim", renewErrAt: 2, reclaimCalls: 1},
		{name: "stale terminal query result", reclaimCalls: 1, queryCalls: 1, closeCalls: 1, heldCalls: 1, closeErr: errors.New("stale claim after provider query")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			network, account := id.New(), id.New()
			store := &fakeSharedResourceStore{providerAccountID: account, costTokenID: id.New(), maintenanceClaim: true, renewErrAt: tc.renewErrAt, claimCloseErr: tc.closeErr,
				rental: &resourceRentalRecord{ID: id.New(), ProviderAccountID: account, NetworkID: network, Provider: crypto.ResourceProviderITRX, ExternalSerial: "lease-1", Status: "active"}}
			provider := &fakeResourceProvider{code: crypto.ResourceProviderITRX, queryOrder: crypto.ResourceOrder{Status: crypto.ResourceOrderClosed}}
			require.NoError(t, testPreparer(store, provider).MaintainResources(t.Context(), "same-worker"))
			require.Equal(t, tc.reclaimCalls, provider.reclaimCalls)
			require.Equal(t, tc.queryCalls, provider.queryCalls)
			require.Equal(t, tc.closeCalls, store.claimCloseCalls)
			require.Equal(t, tc.heldCalls, store.claimHeldCalls)
			require.Empty(t, store.closedStatus)
		})
	}
}
