package crypto

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/appctx"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/eventlog"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/tenant"
)

type ShadowControlState struct {
	NetworkID        id.ID       `json:"networkId"`
	Family           ChainFamily `json:"family"`
	Mode             string      `json:"mode"`
	ActivatedAt      *time.Time  `json:"activatedAt"`
	Paused           bool        `json:"paused"`
	Version          int         `json:"version"`
	LeaseActive      bool        `json:"leaseActive"`
	CheckpointHeight *int64      `json:"checkpointHeight"`
	LastProgressAt   *time.Time  `json:"lastProgressAt"`
	WorkerObservedAt *time.Time  `json:"workerObservedAt"`
	LastErrorCode    string      `json:"lastErrorCode"`
	Status           string      `json:"status"`
	CanChange        bool        `json:"canChange"`
	LockedReason     string      `json:"lockedReason"`
}

func (s ShadowControlState) Observed(now time.Time) ShadowControlState {
	s.CanChange = false
	s.Status = "unavailable"
	switch {
	case s.Family != ChainFamilyTRON:
		s.LockedReason = "unsupported_family"
	case s.ActivatedAt != nil:
		s.Status, s.LockedReason = "financial", "financially_activated"
	case s.Mode != "shadow" || s.Version < 1:
		s.LockedReason = "shadow_not_configured"
	default:
		s.CanChange, s.LockedReason = true, ""
		switch {
		case s.Paused && s.LeaseActive:
			s.Status = "pausing"
		case s.Paused:
			s.Status = "paused"
		case s.LeaseActive:
			s.Status = "running"
		case s.WorkerObservedAt == nil || now.Sub(*s.WorkerObservedAt) > time.Minute || s.WorkerObservedAt.After(now.Add(time.Minute)):
			s.Status = "worker_unavailable"
		default:
			s.Status = "starting"
		}
	}
	return s
}

type ShadowControlRepository interface {
	GetShadowControl(context.Context, id.ID) (ShadowControlState, error)
	SetShadowPaused(context.Context, id.ID, int, bool) (ShadowControlState, error)
}

type ShadowControlAdminService struct {
	repo   ShadowControlRepository
	writer eventlog.Writer
}

func NewShadowControlAdminService(repo ShadowControlRepository, writer eventlog.Writer) *ShadowControlAdminService {
	if repo == nil || writer == nil {
		panic("shadow control repository and audit writer are required")
	}
	return &ShadowControlAdminService{repo: repo, writer: writer}
}

func (s *ShadowControlAdminService) Get(ctx context.Context, network id.ID) (ShadowControlState, error) {
	state, err := s.repo.GetShadowControl(ctx, network)
	return state.Observed(time.Now().UTC()), err
}

func (s *ShadowControlAdminService) SetPaused(ctx context.Context, network id.ID, version int, paused bool, reason string) (ShadowControlState, error) {
	if !appctx.IsSystemAdmin(appctx.GetUser(ctx)) {
		return ShadowControlState{}, apperror.NewForbidden("system administrator access is required")
	}
	reason = strings.TrimSpace(reason)
	if version < 1 || utf8.RuneCountInString(reason) < 3 || utf8.RuneCountInString(reason) > 500 {
		return ShadowControlState{}, apperror.NewValidation("positive version and a reason of 3–500 characters are required")
	}
	txm, err := tenant.GetTxManager(ctx)
	if err != nil {
		return ShadowControlState{}, apperror.NewInternal(err)
	}
	var updated ShadowControlState
	err = txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		current, err := s.Get(txCtx, network)
		if err != nil {
			return err
		}
		if !current.CanChange {
			return apperror.NewValidation("shadow control is unavailable for this network: " + current.LockedReason)
		}
		if current.Version != version {
			return apperror.NewConcurrentModification("crypto_shadow_control", network.String())
		}
		updated, err = s.repo.SetShadowPaused(txCtx, network, version, paused)
		if err != nil {
			return err
		}
		if updated.Version == version {
			return nil
		}
		if err := s.writer.Write(txCtx, eventlog.Event{
			Category: eventlog.CategorySecurity, Severity: eventlog.SeverityWarning,
			EventType: "crypto.ingestion.shadow_control_changed", Source: "api",
			EntityType: "blockchain_network", EntityID: &network, Message: "Shadow check control updated",
			Details: map[string]any{"paused": paused, "reason": reason, "previousVersion": version, "version": updated.Version, "actor": walletPoolActor(txCtx)},
		}); err != nil {
			return err
		}
		return nil
	})
	return updated.Observed(time.Now().UTC()), err
}
