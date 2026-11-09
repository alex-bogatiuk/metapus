package crypto_network_operation

import (
	"context"

	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/domain"
)

// Read protection and immutable fields only after the same prefix as finality.
// A concurrent administrator must not revoke permission between check and write.
type lockedUserService struct {
	domain.DocumentService[*CryptoNetworkOperation]
	system *Service
}

func (s *lockedUserService) withResources(ctx context.Context, docID id.ID, extra []id.ID, action func(context.Context) error) error {
	if !appctx.IsSystemAdmin(appctx.GetUser(ctx)) {
		return apperror.NewForbidden("system administrator access is required")
	}
	return s.system.withResources(ctx, docID, extra, action)
}

func (s *lockedUserService) Update(ctx context.Context, doc *CryptoNetworkOperation) error {
	return s.withResources(ctx, doc.ID, []id.ID{doc.SourceWalletID, doc.TargetWalletID}, func(ctx context.Context) error {
		return s.DocumentService.Update(ctx, doc)
	})
}

func (s *lockedUserService) Post(ctx context.Context, docID id.ID) error {
	return s.withResources(ctx, docID, nil, func(ctx context.Context) error {
		return s.DocumentService.Post(ctx, docID)
	})
}

func (s *lockedUserService) Unpost(ctx context.Context, docID id.ID) error {
	return s.withResources(ctx, docID, nil, func(ctx context.Context) error {
		return s.DocumentService.Unpost(ctx, docID)
	})
}

func (s *lockedUserService) UpdateAndRepost(ctx context.Context, doc *CryptoNetworkOperation) error {
	return s.withResources(ctx, doc.ID, []id.ID{doc.SourceWalletID, doc.TargetWalletID}, func(ctx context.Context) error {
		return s.DocumentService.UpdateAndRepost(ctx, doc)
	})
}

func (s *lockedUserService) SetDeletionMark(ctx context.Context, docID id.ID, marked bool) error {
	return s.withResources(ctx, docID, nil, func(ctx context.Context) error {
		return s.DocumentService.SetDeletionMark(ctx, docID, marked)
	})
}
