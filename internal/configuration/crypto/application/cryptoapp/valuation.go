package cryptoapp

import (
	"context"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/registers/exchange_rate"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

type ValuationService struct {
	balances crypto.BalanceQueryRepo
	rates    *exchange_rate.Service
	snapshot exchange_rate.Snapshot
}

func NewValuationService(balances crypto.BalanceQueryRepo, rates *exchange_rate.Service, snapshot exchange_rate.Snapshot) *ValuationService {
	return &ValuationService{balances: balances, rates: rates, snapshot: snapshot}
}
func (s *ValuationService) Calculate(ctx context.Context, merchants []id.ID) (crypto.MerchantBalance, error) {
	var cfg exchange_rate.Configuration
	var rows []crypto.BalanceRow
	var pending map[string]types.RawAmount
	var rates map[id.ID]exchange_rate.Resolution
	var at time.Time
	err := s.snapshot(ctx, func(ctx context.Context) error {
		at = time.Now().UTC()
		var err error
		cfg, err = s.rates.Configuration(ctx)
		if err != nil {
			return err
		}
		rows, err = s.balances.GetMerchantBalances(ctx, merchants)
		if err != nil {
			return err
		}
		pending, err = s.balances.GetPendingAmounts(ctx, merchants)
		if err != nil {
			return err
		}
		currencies := make([]id.ID, 0, len(rows))
		for _, row := range rows {
			if row.CurrencyID != nil {
				currencies = append(currencies, *row.CurrencyID)
			}
		}
		rates, err = s.rates.ResolveBatch(ctx, cfg, currencies, at)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return crypto.MerchantBalance{}, err
	}
	return crypto.CalculateBalance(rows, pending, rates, cfg, at), nil
}
