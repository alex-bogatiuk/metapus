package portal_repo

import (
	"context"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/crypto/aml"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
)

// Only public custody/risk facts are exposed; Vault identifiers stay internal.
const amlWithdrawalHistoryColumns = `
 (SELECT risk_class FROM reg_crypto_aml_withdrawal_plans WHERE request_id=wr.id),
 COALESCE((SELECT risk_accepted FROM reg_crypto_aml_withdrawal_plans WHERE request_id=wr.id),FALSE),
 COALESCE((SELECT jsonb_agg(jsonb_build_object('cancellable',a.id IS NULL AND l.status IN ('reserved','held'),'id',l.id,'walletId',l.wallet_id,'address',l.source_address,'amount',l.amount::text,'status',l.status,'txHash',COALESCE(a.tx_hash,''),'requestedClass',p.risk_class,'dispatchedClass',CASE WHEN first.authorization_id IS NOT NULL THEN p.risk_class ELSE NULL END,'currentClass',risk.risk_class,'dispatchedAt',first.created_at) ORDER BY l.id)
 FROM reg_crypto_aml_withdrawal_lines l JOIN reg_crypto_aml_withdrawal_plans p ON p.request_id=l.request_id
 LEFT JOIN reg_crypto_signing_authorizations a ON a.basis_type='CryptoAMLWithdrawalLine' AND a.basis_id=l.id AND a.attempt=1 AND a.generation=1
 LEFT JOIN reg_crypto_aml_first_dispatches first ON first.authorization_id=a.id
 LEFT JOIN reg_crypto_wallet_risk_states risk ON risk.wallet_id=l.wallet_id AND risk.token_id=l.token_id
 WHERE l.request_id=wr.id),'[]'::jsonb)`

func (r *DashboardRepo) LockAMLWithdrawalScope(ctx context.Context, merchant, network, token id.ID) (bool, error) {
	return crypto_repo.NewAMLRepository().LockAMLWithdrawalScope(ctx, merchant, network, token)
}

func (r *DashboardRepo) CancelAMLWithdrawalRemainder(ctx context.Context, merchant, requestID id.ID, reason string) (types.RawAmount, error) {
	return crypto_repo.NewAMLRepository().CancelAMLWithdrawalRemainder(ctx, merchant, requestID, reason)
}

func (r *DashboardRepo) WithdrawalRequestStatus(ctx context.Context, merchant, requestID id.ID) (string, error) {
	var status string
	err := querier(ctx).QueryRow(ctx, `SELECT status FROM doc_withdrawal_requests WHERE id=$1 AND merchant_id=$2 AND _deleted_at IS NULL`, requestID, merchant).Scan(&status)
	return status, err
}
func (r *DashboardRepo) ReserveAMLWithdrawalSources(ctx context.Context, intent crypto.MoneyMovementIntent, request aml.WithdrawalRequest) (crypto.MoneyMovementIntent, error) {
	return crypto_repo.NewAMLRepository().ReserveAMLWithdrawalSources(ctx, intent, request)
}
func (r *DashboardRepo) CancelUnsignedAMLWithdrawal(ctx context.Context, requestID id.ID, reason, actor string) error {
	return crypto_repo.NewAMLRepository().CancelUnsignedAMLWithdrawal(ctx, requestID, reason, actor)
}
