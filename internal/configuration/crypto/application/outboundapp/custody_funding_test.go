package outboundapp

import (
	"context"
	"errors"
	"testing"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/stretchr/testify/require"
)

type fundingCustodyStore struct {
	CustodyRepository
	destination, source CustodyWallet
	enforced            bool
	lookupErr           error
	lookups             int
	requestedOwner      *id.ID
}

func (s *fundingCustodyStore) LoadWallet(_ context.Context, wallet id.ID) (CustodyWallet, error) {
	if wallet == s.destination.ID {
		return s.destination, nil
	}
	return s.source, nil
}
func (s *fundingCustodyStore) IsEnforced(context.Context, id.ID) (bool, error) {
	return s.enforced, nil
}
func (s *fundingCustodyStore) FindFeeWalletForOwner(_ context.Context, _ id.ID, owner *id.ID) (id.ID, error) {
	s.lookups++
	s.requestedOwner = owner
	return s.source.ID, s.lookupErr
}
func (s *fundingCustodyStore) LoadFundingWallet(_ context.Context, wallet, _ id.ID, _ string) (HotWallet, error) {
	return HotWallet{ID: wallet}, nil
}

func TestFundingSourceUsesExactCustodyOwner(t *testing.T) {
	for _, tc := range []struct {
		name               string
		merchant, enforced bool
	}{
		{"merchant", true, true},
		{"merchant before enforcement", true, false},
		{"platform", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			network, merchant := id.New(), id.New()
			var owner *id.ID
			if tc.merchant {
				owner = &merchant
			}
			store := &fundingCustodyStore{
				destination: CustodyWallet{ID: id.New(), NetworkID: network, MerchantID: owner},
				source:      CustodyWallet{ID: id.New(), NetworkID: network, MerchantID: owner, Role: "network_fee"},
				enforced:    tc.enforced,
			}
			wallet, err := NewCustodyService(store).ResolveNativeFundingSource(t.Context(), network, store.destination.ID)
			require.NoError(t, err)
			require.Equal(t, store.source.ID, wallet.ID)
			require.Equal(t, owner, store.requestedOwner)
			store.lookupErr = errors.New("registered source is unavailable")
			_, err = NewCustodyService(store).ResolveNativeFundingSource(t.Context(), network, store.destination.ID)
			require.ErrorIs(t, err, store.lookupErr)
			require.Equal(t, 2, store.lookups, "a failed exact lookup must not retry another owner")
		})
	}
}

func TestFundingSourceRejectsInvalidCustodyScope(t *testing.T) {
	for _, scenario := range []string{"foreign owner", "platform destination under enforcement", "wrong source network", "principal source", "self funding"} {
		t.Run(scenario, func(t *testing.T) {
			network, merchant, other := id.New(), id.New(), id.New()
			store := &fundingCustodyStore{
				destination: CustodyWallet{ID: id.New(), NetworkID: network, MerchantID: &merchant},
				source:      CustodyWallet{ID: id.New(), NetworkID: network, MerchantID: &merchant, Role: "network_fee"},
				enforced:    true,
			}
			switch scenario {
			case "foreign owner":
				store.source.MerchantID = &other
			case "platform destination under enforcement":
				store.destination.MerchantID = nil
			case "wrong source network":
				store.source.NetworkID = id.New()
			case "principal source":
				store.source.Role = "liquidity_hot"
			case "self funding":
				store.source.ID = store.destination.ID
			}
			_, err := NewCustodyService(store).ResolveNativeFundingSource(t.Context(), network, store.destination.ID)
			require.ErrorIs(t, err, ErrMerchantCustody)
		})
	}
}
