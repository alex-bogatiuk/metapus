package crypto_network_operation

import (
	"context"
	"time"

	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/core/tx"
	"metapus/internal/domain"
	"metapus/internal/domain/posting"
)

// Service is the unrestricted system service. HTTP handlers must receive the
// guarded service returned by UserService; workers use this concrete service.
type Service struct {
	*domain.BaseHeaderDocumentService[*CryptoNetworkOperation]
	repo Repository
}

func NewService(repo Repository, engine *posting.Engine, num numerator.Generator, txm tx.Manager) *Service {
	return &Service{
		BaseHeaderDocumentService: domain.NewBaseHeaderDocumentService(domain.BaseHeaderDocumentServiceConfig[*CryptoNetworkOperation]{
			Repo: repo, PostingEngine: engine, Numerator: num, TxManager: txm,
			NumeratorPrefix: "CNO", Definition: &Definition,
		}),
		repo: repo,
	}
}

func (s *Service) Hooks() *domain.HookRegistry[*CryptoNetworkOperation] { return s.GetHooks() }

func (s *Service) Post(ctx context.Context, docID id.ID) error {
	return s.withResources(ctx, docID, nil, func(txCtx context.Context) error {
		doc, err := s.repo.GetByID(txCtx, docID)
		if err != nil {
			return err
		}
		if err := s.repo.ValidatePostingReferences(txCtx, doc); err != nil {
			return err
		}
		return s.BaseHeaderDocumentService.Post(txCtx, docID)
	})
}

func (s *Service) UpdateAndRepost(ctx context.Context, doc *CryptoNetworkOperation) error {
	return s.withResources(ctx, doc.ID, []id.ID{doc.SourceWalletID, doc.TargetWalletID}, func(txCtx context.Context) error {
		if err := s.repo.ValidatePostingReferences(txCtx, doc); err != nil {
			return err
		}
		return s.BaseHeaderDocumentService.UpdateAndRepost(txCtx, doc)
	})
}

func (s *Service) UserService() domain.DocumentService[*CryptoNetworkOperation] {
	return &lockedUserService{domain.ProtectStrictDocument[*CryptoNetworkOperation](s, s.repo, ValidateManualUpdate), s}
}

func (s *Service) withResources(ctx context.Context, docID id.ID, extra []id.ID, action func(context.Context) error) error {
	txm, err := s.GetTxManager(ctx)
	if err != nil {
		return err
	}
	return txm.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.LockResources(ctx, docID, extra...); err != nil {
			return err
		}
		return action(ctx)
	})
}

func (s *Service) Unpost(ctx context.Context, docID id.ID) error {
	return s.withResources(ctx, docID, nil, func(ctx context.Context) error {
		return s.BaseHeaderDocumentService.Unpost(ctx, docID)
	})
}

func (s *Service) SetDeletionMark(ctx context.Context, docID id.ID, marked bool) error {
	return s.withResources(ctx, docID, nil, func(ctx context.Context) error {
		return s.BaseHeaderDocumentService.SetDeletionMark(ctx, docID, marked)
	})
}

func (s *Service) AllowEditing(ctx context.Context, docID id.ID) (*CryptoNetworkOperation, error) {
	if !appctx.IsSystemAdmin(appctx.GetUser(ctx)) {
		return nil, apperror.NewForbidden("system administrator access is required")
	}
	var doc *CryptoNetworkOperation
	err := s.withResources(ctx, docID, nil, func(ctx context.Context) error {
		var err error
		doc, err = s.repo.GetByID(ctx, docID)
		if err != nil {
			return err
		}
		if !doc.Status.Final() {
			return apperror.NewBusinessRule("CRYPTO_NETWORK_OPERATION_NOT_FINAL", "only a final network operation can be unlocked")
		}
		if !doc.ManualEditAllowed {
			if err := s.repo.SetManualEditAllowed(ctx, docID, true); err != nil {
				return err
			}
		}
		doc, err = s.repo.GetByID(ctx, docID)
		return err
	})
	return doc, err
}

func (s *Service) DenyEditing(ctx context.Context, docID id.ID) (*CryptoNetworkOperation, error) {
	if !appctx.IsSystemAdmin(appctx.GetUser(ctx)) {
		return nil, apperror.NewForbidden("system administrator access is required")
	}
	var doc *CryptoNetworkOperation
	err := s.withResources(ctx, docID, nil, func(ctx context.Context) error {
		if err := s.repo.SetManualEditAllowed(ctx, docID, false); err != nil {
			return err
		}
		var err error
		doc, err = s.repo.GetByID(ctx, docID)
		return err
	})
	return doc, err
}

// ValidateManualUpdate rejects attempts to rewrite blockchain identity or
// lifecycle facts. Only accounting dimensions/amount and description may vary.
func ValidateManualUpdate(current, updated *CryptoNetworkOperation) error {
	immutableChanged := current.OperationType != updated.OperationType ||
		current.Status != updated.Status ||
		current.NetworkID != updated.NetworkID ||
		current.TargetWalletID != updated.TargetWalletID ||
		current.TargetAddress != updated.TargetAddress ||
		current.TxHash != updated.TxHash ||
		!sameID(current.ChainSettlementID, updated.ChainSettlementID) ||
		!sameTime(current.FinalizedAt, updated.FinalizedAt) ||
		current.BasisType != updated.BasisType || !sameID(current.BasisID, updated.BasisID) ||
		current.Date != updated.Date || current.Number != updated.Number ||
		current.Posted != updated.Posted || current.PostedVersion != updated.PostedVersion ||
		current.DeletionMark != updated.DeletionMark ||
		current.ManualEditAllowed != updated.ManualEditAllowed
	if immutableChanged {
		return apperror.NewBusinessRule("STRICT_DOCUMENT_IMMUTABLE_FIELD", "blockchain operation identity and lifecycle fields cannot be changed")
	}
	return updated.Validate(context.Background())
}

func sameID(left, right *id.ID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func sameTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}
