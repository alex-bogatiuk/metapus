package crypto_repo

import (
	"context"
	"sort"

	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
)

type reconciliationRequest struct {
	network, wallet, token id.ID
	sourceType             string
	source                 id.ID
}

func enqueueReconciliationRequests(ctx context.Context, requests ...reconciliationRequest) error {
	ordered := append([]reconciliationRequest(nil), requests...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].sourceType != ordered[j].sourceType {
			return ordered[i].sourceType < ordered[j].sourceType
		}
		return ordered[i].source.String() < ordered[j].source.String()
	})
	return postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		wallets := make([]id.ID, 0, len(ordered))
		for _, request := range ordered {
			var valid bool
			if err := q.QueryRow(ctx, `SELECT fn_crypto_reconciliation_request_identity_valid($1,$2,$3,$4,$5)`, request.network, request.wallet, request.token, request.sourceType, request.source).Scan(&valid); err != nil {
				return err
			}
			if !valid {
				return apperror.NewConflict("reconciliation request differs from its source target")
			}
			wallets = append(wallets, request.wallet)
		}
		if err := NewCustodyRecognitionRepo().LockWallets(ctx, wallets...); err != nil {
			return err
		}
		for _, request := range ordered {
			if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_reconciliation_requests(network_id,wallet_id,token_id,source_type,source_id)
				VALUES($1,$2,$3,$4,$5) ON CONFLICT(source_type,source_id) DO NOTHING`, request.network, request.wallet, request.token, request.sourceType, request.source); err != nil {
				return err
			}
			var matches bool
			if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_reconciliation_requests
				WHERE source_type=$4 AND source_id=$5 AND network_id=$1 AND wallet_id=$2 AND token_id=$3)`, request.network, request.wallet, request.token, request.sourceType, request.source).Scan(&matches); err != nil {
				return err
			}
			if !matches {
				return apperror.NewConflict("reconciliation request already has another source target")
			}
		}
		return nil
	})
}
