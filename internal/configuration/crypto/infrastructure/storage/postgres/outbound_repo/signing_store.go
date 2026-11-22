package outbound_repo

import (
	"context"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/id"
)

func (r *OutboundRepo) InsertSigningAuthorization(ctx context.Context, auth crypto.SigningAuthorization, jws string, unsigned crypto.UnsignedTxEnvelope, bundle ...crypto.ApprovalBundle) (id.ID, error) {
	return r.insertSigningAuthorization(ctx, auth, jws, unsigned, bundle...)
}

func (r *OutboundRepo) ApplyExactAuthorizationRenewal(ctx context.Context, record SigningAuthorizationRecord) (SigningAuthorizationRecord, error) {
	return r.applyExactAuthorizationRenewal(ctx, record)
}

func (r *OutboundRepo) SetRenewedAuthorizationJWS(ctx context.Context, id id.ID, jws string) error {
	return r.setRenewedAuthorizationJWS(ctx, id, jws)
}
