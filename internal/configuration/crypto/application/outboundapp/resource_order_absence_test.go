package outboundapp

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

// Stand: two rentals placed against "https://https//nile.itrx.io" never
// reached iTRX; once the URL was fixed iTRX answered "Can’t find the order"
// and the rentals stayed held forever.
func TestOrderAbsenceIsSettledOnlyForOldUnacceptedRentals(t *testing.T) {
	now := time.Date(2026, 11, 19, 19, 0, 0, 0, time.UTC)
	service := &ResourceRentalService{now: func() time.Time { return now }}
	notFound := fmt.Errorf("%w: itrx query order rejected: Can’t find the order.", crypto.ErrResourceOrderNotFound)
	old := &ResourceRental{CreatedAt: now.Add(-time.Hour)}
	if !service.orderAbsenceIsSettled(old, notFound) {
		t.Fatal("an hour-old rental the provider does not know was never placed")
	}
	for name, rental := range map[string]*ResourceRental{
		"fresh":    {CreatedAt: now.Add(-time.Minute)},
		"accepted": {CreatedAt: now.Add(-time.Hour), ExternalSerial: "serial-1"},
		"unknown":  {},
	} {
		if service.orderAbsenceIsSettled(rental, notFound) {
			t.Errorf("%s rental must stay ambiguous", name)
		}
	}
	if service.orderAbsenceIsSettled(old, errors.New("connection reset")) {
		t.Error("a transport failure is not evidence of absence")
	}
}

type replaceRentalRepo struct {
	ResourceRentalRepository
	account   *ResourceProviderAccount
	expiresAt time.Time
}

func (s *replaceRentalRepo) GetProviderAccountForRental(context.Context, id.ID) (*ResourceProviderAccount, error) {
	return s.account, nil
}
func (s *replaceRentalRepo) ActivateResourceRental(_ context.Context, _ id.ID, _ string, _ int64, _ types.RawAmount, _ *types.RawAmount, _ []byte, expiresAt time.Time) error {
	s.expiresAt = expiresAt
	return nil
}
func (s *replaceRentalRepo) MarkResourceRentalHeld(context.Context, id.ID, string, []byte) error {
	return nil
}

type replaceProvider struct {
	crypto.ExternalResourceProvider
	queryErr error
	order    crypto.ResourceOrder
	placed   int
}

func (s *replaceProvider) Capabilities() crypto.ExternalResourceCapabilities {
	return crypto.ExternalResourceCapabilities{DefaultPeriod: "1H", DefaultDuration: time.Hour}
}
func (s *replaceProvider) Query(context.Context, crypto.ResourceOrderRef) (crypto.ResourceOrder, error) {
	return s.order, s.queryErr
}
func (s *replaceProvider) Place(context.Context, crypto.ResourceOrderIntent) (crypto.ResourceOrder, error) {
	s.placed++
	return crypto.ResourceOrder{ExternalID: "serial-new", Status: crypto.ResourceOrderActive, CostAmount: types.MustRawAmount("650000")}, nil
}

type replaceConnection struct {
	provider crypto.ExternalResourceProvider
}

func (s replaceConnection) Resolve(context.Context) (crypto.ExternalResourceProvider, error) {
	return s.provider, nil
}

// Stand: both re-placed rentals inherited the 13:34 expiry of the original
// attempt and were reclaimed twenty seconds after delegation.
func TestReplacedRentalLivesAFullPeriodFromNow(t *testing.T) {
	now := time.Date(2026, 11, 19, 19, 48, 0, 0, time.UTC)
	original := now.Add(-6 * time.Hour)
	for name, tc := range map[string]struct {
		provider   *replaceProvider
		wantExpiry time.Time
		wantPlaced int
	}{
		"re-placed after absence":  {&replaceProvider{queryErr: fmt.Errorf("%w: Can’t find the order.", crypto.ErrResourceOrderNotFound)}, now.Add(time.Hour), 1},
		"recovered existing order": {&replaceProvider{order: crypto.ResourceOrder{ExternalID: "serial-old", Status: crypto.ResourceOrderActive}}, original, 0},
	} {
		t.Run(name, func(t *testing.T) {
			networkID, accountID := id.New(), id.New()
			repo := &replaceRentalRepo{account: &ResourceProviderAccount{ID: accountID, Provider: "itrx", NetworkID: networkID, Connection: replaceConnection{tc.provider}}}
			service := &ResourceRentalService{sharedRepo: repo, now: func() time.Time { return now }}
			expires := original
			rental := &ResourceRental{ID: id.New(), ProviderAccountID: accountID, NetworkID: networkID, Provider: "itrx",
				OutTradeNo: "mp_rr_test", RequestedAmount: 65000, ExpiresAt: &expires, CreatedAt: original.Add(-time.Hour)}
			recovered, err := service.recoverPendingRental(t.Context(), rental, ResourceRequest{SourceAddress: "TReceive"})
			if err != nil || !recovered {
				t.Fatalf("recover: %v %v", recovered, err)
			}
			if !repo.expiresAt.Equal(tc.wantExpiry) {
				t.Fatalf("expiry = %s, want %s", repo.expiresAt, tc.wantExpiry)
			}
			if tc.provider.placed != tc.wantPlaced {
				t.Fatalf("placed %d orders, want %d", tc.provider.placed, tc.wantPlaced)
			}
		})
	}
}
