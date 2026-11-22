package dashboardapp

import (
	"context"
	"time"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/domain/dashboard"
	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/registers/exchange_rate"
	"metapus/pkg/sdk/types"
)

type Service struct {
	repo     dashboard.Repository
	rates    *exchange_rate.Service
	snapshot exchange_rate.Snapshot
}

func NewService(repo dashboard.Repository, rates *exchange_rate.Service, snapshot exchange_rate.Snapshot) *Service {
	return &Service{repo: repo, rates: rates, snapshot: snapshot}
}

func (s *Service) Queues(ctx context.Context, kind string, limit int) (dashboard.Queues, error) {
	if kind != "all" && kind != "withdrawal" && kind != "inbound" && kind != "outflow" {
		return dashboard.Queues{}, apperror.NewValidation("invalid queue kind")
	}
	if limit < 1 || limit > 20 {
		return dashboard.Queues{}, apperror.NewValidation("limit must be between 1 and 20")
	}
	var result dashboard.Queues
	err := s.snapshot(ctx, func(ctx context.Context) error {
		var err error
		result, err = s.repo.Queues(ctx, kind, limit)
		return err
	})
	return result, err
}

func (s *Service) Analytics(ctx context.Context, period string) (dashboard.Analytics, error) {
	if period != "7d" && period != "30d" && period != "90d" {
		return dashboard.Analytics{}, apperror.NewValidation("invalid analytics period")
	}
	var result dashboard.Analytics
	err := s.snapshot(ctx, func(ctx context.Context) error {
		var err error
		result, err = s.repo.Analytics(ctx, period)
		return err
	})
	return result, err
}

func (s *Service) Finance(ctx context.Context) (dashboard.Finance, error) {
	result := dashboard.Finance{}
	err := s.snapshot(ctx, func(ctx context.Context) error {
		at := time.Now().UTC()
		assets, err := s.repo.Assets(ctx)
		if err != nil {
			return err
		}
		cfg, err := s.rates.Configuration(ctx)
		if err != nil {
			return err
		}
		currencies := make([]id.ID, 0, len(assets))
		merchantRows, walletRows := make([]crypto.BalanceRow, 0, len(assets)), make([]crypto.BalanceRow, 0, len(assets))
		for _, asset := range assets {
			if asset.CurrencyID != nil {
				currencies = append(currencies, *asset.CurrencyID)
			}
			tokenID, err := id.Parse(asset.TokenID)
			if err != nil {
				return err
			}
			networkID, err := id.Parse(asset.NetworkID)
			if err != nil {
				return err
			}
			row := crypto.BalanceRow{TokenID: tokenID, NetworkID: networkID, TokenSymbol: asset.Symbol, Network: asset.Network, DecimalPlaces: asset.DecimalPlaces, CurrencyID: asset.CurrencyID, CurrencyCode: asset.CurrencyCode}
			row.RawAmount, err = types.NewRawAmountFromString(asset.ObligationMinor)
			if err != nil {
				return err
			}
			merchantRows = append(merchantRows, row)
			row.RawAmount, err = types.NewRawAmountFromString(asset.WalletMinor)
			if err != nil {
				return err
			}
			walletRows = append(walletRows, row)
		}
		quotes, err := s.rates.ResolveBatch(ctx, cfg, currencies, at)
		if err != nil {
			return err
		}
		result.Assets, result.ObservedAt = assets, at
		result.Obligations = crypto.CalculateBalance(merchantRows, nil, quotes, cfg, at)
		result.Wallets = crypto.CalculateBalance(walletRows, nil, quotes, cfg, at)
		return nil
	})
	return result, err
}
