package outbound_repo

import (
	"context"

	"metapus/internal/application/outboundapp"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
)

type NativeFundingStore struct{ *OutboundRepo }

func (NativeFundingStore) Lookup(ctx context.Context, basis string, id id.ID, attempt int) (crypto.NativeFunding, bool, error) {
	return lookupNativeFunding(ctx, basis, id, attempt)
}

func (NativeFundingStore) Create(ctx context.Context, value crypto.NativeFunding) (crypto.NativeFunding, error) {
	return getOrCreateNativeFunding(ctx, value)
}

func (NativeFundingStore) LoadWallet(ctx context.Context, wallet, network id.ID, role string) (outboundapp.HotWallet, error) {
	return loadNativeFundingWallet(ctx, wallet, network, role)
}

func (NativeFundingStore) OperatingBalance(ctx context.Context, wallet, token id.ID) (types.CryptoAmount, bool, error) {
	return amlAvailableOperatingBalance(ctx, wallet, token)
}

func (NativeFundingStore) LinkReservation(ctx context.Context, funding, reservation id.ID) error {
	return linkNativeFundingReservation(ctx, funding, reservation)
}

func (NativeFundingStore) RetryBeforeAuthorization(ctx context.Context, funding id.ID) error {
	return retryNativeFundingBeforeAuthorization(ctx, funding)
}

func (NativeFundingStore) SetStatus(ctx context.Context, funding id.ID, status, reason, fingerprint string, authorization *id.ID) error {
	return markNativeFundingStatus(ctx, funding, status, reason, fingerprint, authorization)
}

func (NativeFundingStore) MarkBroadcast(ctx context.Context, funding id.ID, hash, fingerprint string) error {
	return markNativeFundingBroadcast(ctx, funding, hash, fingerprint)
}
