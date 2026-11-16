package integrationapp

import (
	"context"
	"strings"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/tx"
	"metapus/internal/domain/eventlog"
	"metapus/internal/domain/integrations"
)

type Service struct {
	repo   integrations.AdministrationRepository
	txm    tx.Manager
	writer eventlog.Writer
}

func New(repo integrations.AdministrationRepository, txm tx.Manager, writer eventlog.Writer) *Service {
	return &Service{repo: repo, txm: txm, writer: writer}
}
func (s *Service) Status(ctx context.Context, uid id.ID) (integrations.PoolStatus, error) {
	return s.repo.Status(ctx, uid)
}

func (s *Service) Command(ctx context.Context, uid id.ID, action, reason string, expected int64) (integrations.PoolStatus, error) {
	var out integrations.PoolStatus
	if len(strings.TrimSpace(reason)) < 3 || len(reason) > 500 {
		return out, apperror.NewValidation("Укажите причину от 3 до 500 символов")
	}
	if action != "next" && action != "exhaust" && action != "resume" {
		return out, apperror.NewValidation("Неизвестная команда пула")
	}
	err := s.txm.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.Command(ctx, uid, action, expected); err != nil {
			return err
		}
		if s.writer != nil {
			if err := s.writer.Write(ctx, eventlog.Event{Category: eventlog.CategorySecurity, Severity: eventlog.SeverityWarning, EventType: "integration.pool.command", EntityType: "integration_pool", EntityID: &uid, Message: "Integration pool command", Details: map[string]any{"action": action, "reason": reason, "selectionVersion": expected}}); err != nil {
				return err
			}
		}
		var err error
		out, err = s.repo.Status(ctx, uid)
		return err
	})
	return out, err
}
