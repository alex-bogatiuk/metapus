package crypto_repo

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto/aml"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
	"github.com/jackc/pgx/v5"
)

func (r *AMLRepository) LockAMLWithdrawalScope(ctx context.Context, merchant, network, token id.ID) (bool, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	if _, err := q.Exec(ctx, `SELECT version FROM sys_crypto_aml_routing_scopes WHERE merchant_id=$1 AND network_id=$2 ORDER BY merchant_id,network_id,token_id FOR UPDATE`, merchant, network); err != nil {
		return false, err
	}
	var enabled bool
	err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes WHERE merchant_id=$1 AND network_id=$2 AND token_id=$3 AND enabled)`, merchant, network, token).Scan(&enabled)
	return enabled, err
}

func (r *AMLRepository) ReserveAMLWithdrawalSources(ctx context.Context, intent crypto.MoneyMovementIntent, request aml.WithdrawalRequest) (crypto.MoneyMovementIntent, error) {
	if intent.Purpose != crypto.SigningPurposeWithdrawal || intent.BasisType != "WithdrawalRequest" || id.IsNil(intent.BasisID) || intent.MerchantID == nil || *intent.MerchantID != request.MerchantID || intent.TokenID != request.TokenID || !intent.AmountMinorUnits.Equal(request.Amount) {
		return intent, apperror.NewValidation("withdrawal intent differs from requested custody scope")
	}
	if err := request.Validate(); err != nil {
		return intent, err
	}
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) == nil {
		return intent, apperror.NewConflict("withdrawal plan requires the debit transaction")
	}
	q := tm.GetQuerier(ctx)
	rows, err := q.Query(ctx, `SELECT w.id,w.address,w.vault_key_id,p.revision,r.revision,r.scope_version,r.evidence_hash,
  p.principal-COALESCE((SELECT SUM(principal) FROM reg_crypto_aml_internal_allocations WHERE source_wallet_id=w.id AND token_id=p.token_id AND status='reserved'),0)
   -COALESCE((SELECT SUM(amount) FROM reg_crypto_aml_withdrawal_lines WHERE wallet_id=w.id AND token_id=p.token_id AND status IN ('reserved','held','broadcast')),0) available
 FROM reg_crypto_aml_inventory_positions p JOIN cat_wallets w ON w.id=p.wallet_id
 JOIN reg_crypto_wallet_risk_states r ON r.wallet_id=p.wallet_id AND r.token_id=p.token_id
 JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=p.merchant_id AND s.network_id=p.network_id AND s.token_id=p.token_id AND s.enabled AND s.version=r.scope_version
 WHERE p.merchant_id=$1 AND p.network_id=$2 AND p.token_id=$3 AND r.risk_class=$4 AND p.principal>0
 AND w.wallet_role IN ('merchant_pool','liquidity_hot') AND w.is_active AND NOT w.deletion_mark AND w.chain_activation_status='active' AND w.vault_key_id<>''
 AND fn_crypto_aml_total_inventory_covered(w.id,p.token_id) AND ($4 IN ('high','unknown') OR (r.inventory_covered AND r.expires_at>NOW()))
 AND NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_allocation_holds WHERE wallet_id=w.id AND status='open' AND ($4 IN ('low','medium') OR reason<>'aml_pool_high_risk'))
 AND NOT EXISTS(SELECT 1 FROM reg_crypto_balance_reservations WHERE wallet_id=w.id AND token_id=p.token_id AND status='active' AND document_type<>'CryptoAMLWithdrawalLine')
 ORDER BY w.id LIMIT 1000 FOR UPDATE OF p`, request.MerchantID, request.NetworkID, request.TokenID, request.Class)
	if err != nil {
		return intent, err
	}
	type source struct {
		line              crypto.MoneyMovementSourceLine
		inventoryRevision int64
		available         types.RawAmount
	}
	sources := map[id.ID]source{}
	var candidates []aml.Source
	for rows.Next() {
		var s source
		if err := rows.Scan(&s.line.WalletID, &s.line.SourceAddress, &s.line.SourceVaultKeyID, &s.inventoryRevision, &s.line.RiskRevision, &s.line.ScopeVersion, &s.line.EvidenceHash, &s.available); err != nil {
			rows.Close()
			return intent, err
		}
		sources[s.line.WalletID] = s
		candidates = append(candidates, aml.Source{WalletID: s.line.WalletID, MerchantID: request.MerchantID, NetworkID: request.NetworkID, TokenID: request.TokenID, Class: request.Class, Revision: s.line.RiskRevision, Available: s.available})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return intent, err
	}
	selected, err := aml.SelectSources(request, candidates)
	if err != nil {
		return intent, err
	}
	intent.SchemaVersion = crypto.MoneyMovementIntentV2
	intent.RiskClass = string(request.Class)
	intent.RiskAccepted = request.RiskAccepted
	for _, line := range selected {
		s := sources[line.WalletID]
		s.line.LineID = id.New()
		s.line.AmountMinorUnits = line.Amount
		intent.SourceLines = append(intent.SourceLines, s.line)
	}
	if err := intent.ValidateSources(); err != nil {
		return intent, err
	}
	digest, err := crypto.DigestJSON(intent)
	if err != nil {
		return intent, err
	}
	_, err = q.Exec(ctx, `INSERT INTO reg_crypto_aml_withdrawal_plans(request_id,merchant_id,network_id,token_id,risk_class,risk_accepted,source_count,request_digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, intent.BasisID, request.MerchantID, request.NetworkID, request.TokenID, request.Class, request.RiskAccepted, len(intent.SourceLines), digest)
	if err != nil {
		return intent, err
	}
	for _, line := range intent.SourceLines {
		reservation := id.New()
		_, err = q.Exec(ctx, `INSERT INTO reg_crypto_balance_reservations(id,wallet_id,token_id,amount,purpose,document_type,document_id) VALUES($1,$2,$3,$4,'withdrawal','CryptoAMLWithdrawalLine',$5)`, reservation, line.WalletID, request.TokenID, line.AmountMinorUnits, line.LineID)
		if err != nil {
			return intent, err
		}
		_, err = q.Exec(ctx, `INSERT INTO reg_crypto_aml_withdrawal_lines(id,request_id,wallet_id,token_id,amount,source_revision,risk_revision,scope_version,source_address,source_vault_key_id,evidence_hash,reservation_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, line.LineID, intent.BasisID, line.WalletID, request.TokenID, line.AmountMinorUnits, sources[line.WalletID].inventoryRevision, line.RiskRevision, line.ScopeVersion, line.SourceAddress, line.SourceVaultKeyID, line.EvidenceHash, reservation)
		if err != nil {
			return intent, err
		}
	}
	evidence, err := json.Marshal(intent)
	if err != nil {
		return intent, err
	}
	_, err = q.Exec(ctx, `INSERT INTO reg_crypto_withdrawal_risk_snapshots(request_id,stage,generation,risk_class,intent_digest,risk_accepted,evidence) VALUES($1,'requested',1,$2,$3,$4,$5)`, intent.BasisID, request.Class, digest, request.RiskAccepted, evidence)
	return intent, err
}

// CancelUnsignedAMLWithdrawal releases only never-authorized lines. The caller
// owns the corresponding compensating merchant movement in this transaction.
func (r *AMLRepository) CancelUnsignedAMLWithdrawal(ctx context.Context, requestID id.ID, reason, actor string) error {
	if postgres.MustGetTxManager(ctx).GetTx(ctx) == nil {
		return apperror.NewValidation("withdrawal cancellation requires the compensation transaction")
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var merchant, network, token id.ID
	err := q.QueryRow(ctx, `SELECT merchant_id,network_id,token_id FROM reg_crypto_aml_withdrawal_plans WHERE request_id=$1`, requestID).Scan(&merchant, &network, &token)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if count := utf8.RuneCountInString(strings.TrimSpace(reason)); count < 3 || count > 500 {
		return apperror.NewValidation("cancellation reason must contain 3 to 500 characters")
	}
	if strings.TrimSpace(actor) == "" {
		return apperror.NewValidation("withdrawal cancellation requires an actor")
	}
	if err = r.LockAMLWithdrawalRequest(ctx, requestID); err != nil {
		return err
	}
	var authorized bool
	if err = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_lines l JOIN reg_crypto_signing_authorizations a ON a.basis_type='CryptoAMLWithdrawalLine' AND a.basis_id=l.id WHERE l.request_id=$1)`, requestID).Scan(&authorized); err != nil {
		return err
	}
	if authorized {
		return apperror.NewConflict("withdrawal has potential signing exposure; cancel only its unsigned remainder")
	}
	_, err = q.Exec(ctx, `INSERT INTO reg_crypto_aml_withdrawal_cancellations(line_id,reason,actor) SELECT id,$2,$3 FROM reg_crypto_aml_withdrawal_lines WHERE request_id=$1 AND status IN ('reserved','held') ORDER BY id ON CONFLICT DO NOTHING`, requestID, reason, actor)
	if err != nil {
		return err
	}
	if err := checkWithdrawalCancellationReplay(ctx, requestID, reason, actor); err != nil {
		return err
	}
	if err := r.cancelUnsignedWithdrawalFees(ctx, requestID, nil, reason, actor); err != nil {
		return err
	}
	_, err = q.Exec(ctx, `UPDATE reg_crypto_aml_withdrawal_lines SET status='cancelled',lease_token=NULL,lease_expires_at=NULL WHERE request_id=$1 AND status IN ('reserved','held')`, requestID)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `UPDATE reg_crypto_balance_reservations r SET status='released',updated_at=NOW() FROM reg_crypto_aml_withdrawal_lines l WHERE l.request_id=$1 AND l.reservation_id=r.id AND l.status='cancelled' AND r.status='active'`, requestID)
	return err
}

func (r *AMLRepository) CancelAMLWithdrawalRemainder(ctx context.Context, merchant, requestID id.ID, reason string) (types.RawAmount, error) {
	amount := types.ZeroRawAmount()
	if postgres.MustGetTxManager(ctx).GetTx(ctx) == nil {
		return amount, apperror.NewValidation("withdrawal cancellation requires the compensation transaction")
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var network, token id.ID
	var postedVersion int
	err := q.QueryRow(ctx, `SELECT p.network_id,p.token_id,r.posted_version FROM reg_crypto_aml_withdrawal_plans p JOIN doc_withdrawal_requests r ON r.id=p.request_id WHERE p.request_id=$1 AND p.merchant_id=$2 AND r.status IN ('approved','signing','broadcast','held')`, requestID, merchant).Scan(&network, &token, &postedVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		var status string
		var planned bool
		if lookupErr := q.QueryRow(ctx, `SELECT r.status,EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_plans p WHERE p.request_id=r.id)
 FROM doc_withdrawal_requests r WHERE r.id=$1 AND r.merchant_id=$2`, requestID, merchant).Scan(&status, &planned); lookupErr == nil && !planned {
			// A request without an AML plan has no remainder to release: its
			// whole amount is already in execution and cannot be cancelled.
			return amount, apperror.NewConflict("withdrawal request can no longer be cancelled: it is already "+status).
				WithDetail("status", status)
		}
		return amount, apperror.NewConflict("withdrawal has no cancellable AML remainder")
	}
	if err != nil {
		return amount, err
	}
	if count := utf8.RuneCountInString(strings.TrimSpace(reason)); count < 3 || count > 500 {
		return amount, apperror.NewValidation("cancellation reason must contain 3 to 500 characters")
	}
	if err = r.LockAMLWithdrawalRequest(ctx, requestID); err != nil {
		return amount, err
	}
	var eligible int
	var pendingEvidence bool
	if err := q.QueryRow(ctx, `SELECT count(*),COALESCE(bool_or(EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_cancellations c WHERE c.line_id=l.id)),FALSE)
 FROM reg_crypto_aml_withdrawal_lines l WHERE l.request_id=$1 AND l.status IN ('reserved','held')
 AND NOT EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations a WHERE a.basis_type='CryptoAMLWithdrawalLine' AND a.basis_id=l.id)`, requestID).Scan(&eligible, &pendingEvidence); err != nil {
		return amount, err
	}
	if pendingEvidence {
		return amount, apperror.NewConflict("unsigned withdrawal line already has cancellation evidence; reconciliation required")
	}
	rows, err := q.Query(ctx, `INSERT INTO reg_crypto_aml_withdrawal_cancellations(line_id,reason,actor)
 SELECT l.id,$2,'merchant' FROM reg_crypto_aml_withdrawal_lines l WHERE l.request_id=$1 AND l.status IN ('reserved','held') AND NOT EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations a WHERE a.basis_type='CryptoAMLWithdrawalLine' AND a.basis_id=l.id)
 ORDER BY l.id ON CONFLICT DO NOTHING RETURNING line_id`, requestID, reason)
	if err != nil {
		return amount, err
	}
	var lines []id.ID
	for rows.Next() {
		var line id.ID
		if err := rows.Scan(&line); err != nil {
			rows.Close()
			return amount, err
		}
		lines = append(lines, line)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return amount, err
	}
	if len(lines) == 0 {
		return amount, apperror.NewConflict("all remaining lines have potential signing exposure; recovery must determine their outcome")
	}
	if len(lines) != eligible {
		return amount, apperror.NewConflict("withdrawal cancellation evidence changed; reconciliation required")
	}
	if err := r.cancelUnsignedWithdrawalFees(ctx, requestID, lines, reason, "merchant"); err != nil {
		return amount, err
	}
	if _, err = q.Exec(ctx, `INSERT INTO reg_crypto_merchant_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,amount)
 SELECT id,request_id,'WithdrawalRequest',$2,NOW(),'receipt',$3,token_id,amount FROM reg_crypto_aml_withdrawal_lines WHERE id=ANY($1)`, lines, postedVersion+1, merchant); err != nil {
		return amount, err
	}
	if err = q.QueryRow(ctx, `SELECT SUM(amount) FROM reg_crypto_aml_withdrawal_lines WHERE id=ANY($1)`, lines).Scan(&amount); err != nil {
		return amount, err
	}
	if _, err = q.Exec(ctx, `UPDATE reg_crypto_aml_withdrawal_lines SET status='cancelled',lease_token=NULL,lease_expires_at=NULL WHERE id=ANY($1)`, lines); err != nil {
		return amount, err
	}
	if _, err = q.Exec(ctx, `UPDATE reg_crypto_balance_reservations r SET status='released',updated_at=NOW() FROM reg_crypto_aml_withdrawal_lines l WHERE l.id=ANY($1) AND l.reservation_id=r.id AND r.status='active'`, lines); err != nil {
		return amount, err
	}
	_, err = q.Exec(ctx, `SELECT fn_crypto_aml_refresh_withdrawal($1)`, requestID)
	return amount, err
}

func (r *AMLRepository) cancelUnsignedWithdrawalFees(ctx context.Context, requestID id.ID, lines []id.ID, reason, actor string) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	rows, err := q.Query(ctx, `SELECT b.id FROM reg_crypto_aml_resource_budgets b JOIN reg_crypto_fee_reservations f ON f.id=b.fee_reservation_id JOIN reg_crypto_aml_withdrawal_lines l ON l.id=f.basis_id AND f.basis_type='CryptoAMLWithdrawalLine' WHERE l.request_id=$1 AND ($2::uuid[] IS NULL OR l.id=ANY($2)) AND b.status='reserved' AND NOT EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations WHERE basis_type='CryptoAMLWithdrawalLine' AND basis_id=l.id) ORDER BY b.id`, requestID, lines)
	if err != nil {
		return err
	}
	var budgets []id.ID
	for rows.Next() {
		var budget id.ID
		if err := rows.Scan(&budget); err != nil {
			rows.Close()
			return err
		}
		budgets = append(budgets, budget)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, budget := range budgets {
		if err := r.CancelAMLResourceBudget(ctx, budget, reason, actor); err != nil {
			return err
		}
	}
	return nil
}
