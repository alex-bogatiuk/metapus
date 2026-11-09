package integrationapp

import (
	"context"

	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/tx"
	"metapus/internal/domain/eventlog"
	"metapus/internal/domain/integrations"
)

type RPCConfigurationService struct {
	repo   integrations.RPCConfigurationRepository
	txm    tx.Manager
	writer eventlog.Writer
}

func NewRPCConfigurationService(repo integrations.RPCConfigurationRepository, txm tx.Manager, writer eventlog.Writer) *RPCConfigurationService {
	return &RPCConfigurationService{repo: repo, txm: txm, writer: writer}
}

func (s *RPCConfigurationService) Get(ctx context.Context, network id.ID) (integrations.RPCConfiguration, error) {
	return s.repo.Get(ctx, network)
}

func (s *RPCConfigurationService) Update(ctx context.Context, network id.ID, update integrations.RPCConfigurationUpdate) (integrations.RPCConfiguration, error) {
	var out integrations.RPCConfiguration
	if err := update.Validate(); err != nil {
		return out, err
	}
	err := s.txm.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.Lock(ctx); err != nil {
			return err
		}
		before, err := s.repo.Get(ctx, network)
		if err != nil {
			return err
		}
		changes, err := before.Plan(update)
		if err != nil {
			return err
		}
		settings := update.Settings
		if settings != nil && settings.Equal(before.Settings) {
			settings = nil
		}
		if len(changes) == 0 && settings == nil {
			out = before
			return nil
		}
		after, err := s.repo.Save(ctx, network, before.Revision, changes, settings)
		if err != nil {
			return err
		}
		if integrations.RPCConfigurationChanged(before, after) && s.writer != nil {
			if err := s.writer.Write(ctx, eventlog.Event{
				Category: eventlog.CategorySecurity, Severity: eventlog.SeverityInfo,
				EventType: "integration.rpc.configuration_changed", Source: "api", UserID: appctx.GetUserID(ctx),
				EntityType: "blockchain_network", EntityID: &network, Message: "RPC configuration updated",
				Details: map[string]any{"before": before.Endpoints, "after": after.Endpoints, "previousSettings": before.Settings, "settings": after.Settings, "previousRevision": before.Revision, "revision": after.Revision, "comment": integrations.RPCConfigurationComment(update.Comment)},
			}); err != nil {
				return err
			}
		}
		out = after
		return nil
	})
	return out, err
}
