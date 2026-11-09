package crypto_repo

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
)

type FinancialAdjustmentRepo struct{}

func NewFinancialAdjustmentRepo() *FinancialAdjustmentRepo { return &FinancialAdjustmentRepo{} }

var _ cryptoapp.FinancialAdjustmentRepository = (*FinancialAdjustmentRepo)(nil)

const adjustmentSelect = `SELECT id,COALESCE(payment_id,'00000000-0000-0000-0000-000000000000'::uuid) AS payment_id,
    COALESCE(posted_version,0) AS posted_version,mode,wallet_id,token_id,
    COALESCE(direction,'') AS direction,COALESCE(amount::text,'') AS amount,decision_mode,
    basis_kind,source_posting_id,
    reason_code,evidence,maker_id,checker_id,status,decision_reason,created_at,decided_at
    FROM doc_crypto_adjustments`

func (r *FinancialAdjustmentRepo) List(ctx context.Context, status string, limit int) ([]cryptoapp.FinancialAdjustment, error) {
	var result []cryptoapp.FinancialAdjustment
	err := pgxscan.Select(ctx, postgres.MustGetTxManager(ctx).GetQuerier(ctx), &result,
		adjustmentSelect+` WHERE ($1='' OR status=$1) ORDER BY created_at DESC,id DESC LIMIT $2`,
		status, limit)
	if err != nil {
		return nil, fmt.Errorf("list crypto adjustments: %w", err)
	}
	return result, nil
}

func (r *FinancialAdjustmentRepo) ListForPayment(ctx context.Context, paymentID id.ID, status string, limit int) ([]cryptoapp.FinancialAdjustment, error) {
	var result []cryptoapp.FinancialAdjustment
	err := pgxscan.Select(ctx, postgres.MustGetTxManager(ctx).GetQuerier(ctx), &result,
		adjustmentSelect+` WHERE payment_id=$1 AND ($2='' OR status=$2)
		ORDER BY created_at DESC,id DESC LIMIT $3`, paymentID, status, limit)
	if err != nil {
		return nil, fmt.Errorf("list payment crypto adjustments: %w", err)
	}
	return result, nil
}

func (r *FinancialAdjustmentRepo) Get(ctx context.Context, adjustmentID id.ID) (*cryptoapp.FinancialAdjustment, error) {
	var result cryptoapp.FinancialAdjustment
	err := pgxscan.Get(ctx, postgres.MustGetTxManager(ctx).GetQuerier(ctx), &result,
		adjustmentSelect+` WHERE id=$1`, adjustmentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperror.NewNotFound("crypto_adjustment", adjustmentID)
	}
	if err != nil {
		return nil, fmt.Errorf("get crypto adjustment: %w", err)
	}
	return &result, nil
}

func (r *FinancialAdjustmentRepo) Propose(ctx context.Context, cmd cryptoapp.ProposeFinancialAdjustmentCommand) (*cryptoapp.FinancialAdjustment, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	version := cmd.PostedVersion
	sort.Slice(cmd.SourceLines, func(i, j int) bool {
		left := cmd.SourceLines[i].Register + cmd.SourceLines[i].LineID.String()
		right := cmd.SourceLines[j].Register + cmd.SourceLines[j].LineID.String()
		return left < right
	})
	fingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d|%s|%s|%s|%s|%s|%v|%s|%t",
		cmd.Mode, cmd.PaymentID, version, cmd.WalletID, cmd.TokenID, cmd.Direction,
		cmd.Amount, cmd.ReasonCode, cmd.SourceLines, cmd.Evidence, cmd.Direct))))
	adjustmentID := id.New()
	decisionMode := "two_person"
	if cmd.Direct {
		decisionMode = "direct"
	}
	var paymentID, walletID, tokenID any
	var postedVersion any
	var amount any
	var direction any
	var sourcePostingID *id.ID
	basisKind := "wallet_variance"
	if cmd.Mode == "payment_reversal" {
		paymentID, postedVersion = cmd.PaymentID, version
		var requiresRecognition bool
		err := q.QueryRow(ctx, `SELECT (SELECT p.id FROM reg_crypto_custody_effect_postings p
			JOIN reg_crypto_payment_custody_bindings b ON b.posting_id=p.id AND b.payment_id=d.id
			WHERE p.recorder_type='CryptoPayment' AND p.recorder_id=d.id AND p.recorder_version=$2),
			fn_crypto_payment_requires_recognition(d.id,d.network_id,d.tx_hash,d.event_kind,d.event_index)
			FROM doc_crypto_payments d WHERE d.id=$1`, cmd.PaymentID, version).Scan(&sourcePostingID, &requiresRecognition)
		if err != nil {
			return nil, fmt.Errorf("load adjustment accounting basis: %w", err)
		}
		basisKind = "legacy_payment"
		if sourcePostingID != nil {
			basisKind = "payment_compensation"
		} else if requiresRecognition {
			return nil, apperror.NewConflict("indexed payment adjustment requires its source custody posting")
		}
	} else {
		walletID, tokenID, direction, amount = cmd.WalletID, cmd.TokenID, cmd.Direction, cmd.Amount
	}
	err := q.QueryRow(ctx, `INSERT INTO doc_crypto_adjustments
		(id,payment_id,posted_version,mode,wallet_id,token_id,direction,amount,decision_mode,
		 reason_code,evidence,idempotency_key,request_hash,maker_id,basis_kind,source_posting_id)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8::numeric,$9,$10,$11::jsonb,$12,$13,$14,$15,$16)
		ON CONFLICT (idempotency_key) DO NOTHING RETURNING id`, adjustmentID,
		paymentID, postedVersion, cmd.Mode, walletID, tokenID, direction, amount,
		decisionMode, cmd.ReasonCode, string(cmd.Evidence), cmd.IdempotencyKey,
		fingerprint, cmd.MakerID, basisKind, sourcePostingID).Scan(&adjustmentID)
	if errors.Is(err, pgx.ErrNoRows) {
		var existingHash string
		var makerID id.ID
		err = q.QueryRow(ctx, `SELECT id,request_hash,maker_id FROM doc_crypto_adjustments
			WHERE idempotency_key=$1`, cmd.IdempotencyKey).Scan(&adjustmentID, &existingHash, &makerID)
		if err != nil {
			return nil, err
		}
		if existingHash != fingerprint || makerID != cmd.MakerID {
			return nil, apperror.NewConflict("adjustment idempotency key was used for different evidence")
		}
		return r.Get(ctx, adjustmentID)
	}
	if err != nil {
		return nil, fmt.Errorf("create crypto adjustment: %w", err)
	}
	if cmd.Mode == "payment_reversal" {
		if err := r.snapshotSources(ctx, adjustmentID, cmd.PaymentID, version, cmd.SourceLines); err != nil {
			return nil, err
		}
	}
	return r.Get(ctx, adjustmentID)
}

func (r *FinancialAdjustmentRepo) snapshotSources(ctx context.Context, adjustmentID, paymentID id.ID, version int,
	selected []cryptoapp.FinancialAdjustmentSourceSelection) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	if len(selected) == 0 {
		for _, source := range adjustmentRegisters {
			query := fmt.Sprintf(`INSERT INTO reg_crypto_adjustment_sources
				(adjustment_id,source_register,source_line_id,amount)
				SELECT $1,$2,m.line_id,m.amount-COALESCE((SELECT SUM(r.amount) FROM %[1]s r
				  WHERE r.reversal_of_line_id=m.line_id),0)
				FROM %[1]s m WHERE m.recorder_type='CryptoPayment' AND m.recorder_id=$3
				  AND m.recorder_version=$4 AND m.reversal_of_line_id IS NULL
				  AND m.amount>COALESCE((SELECT SUM(r.amount) FROM %[1]s r
				    WHERE r.reversal_of_line_id=m.line_id),0)`, source.table)
			if _, err := q.Exec(ctx, query, adjustmentID, source.kind, paymentID, version); err != nil {
				return fmt.Errorf("snapshot %s adjustment sources: %w", source.kind, err)
			}
		}
	} else {
		seen := make(map[string]bool, len(selected))
		for _, line := range selected {
			key := line.Register + line.LineID.String()
			if seen[key] {
				return apperror.NewValidation("payment movement selected twice")
			}
			seen[key] = true
			source, ok := findAdjustmentRegister(line.Register)
			if !ok {
				return apperror.NewValidation("invalid payment movement register")
			}
			query := fmt.Sprintf(`INSERT INTO reg_crypto_adjustment_sources
				(adjustment_id,source_register,source_line_id,amount)
				SELECT $1,$2,m.line_id,$3::numeric FROM %[1]s m
				WHERE m.line_id=$4 AND m.recorder_type='CryptoPayment' AND m.recorder_id=$5
				 AND m.recorder_version=$6 AND m.reversal_of_line_id IS NULL
				 AND $3::numeric<=m.amount-COALESCE((SELECT SUM(r.amount) FROM %[1]s r
				   WHERE r.reversal_of_line_id=m.line_id),0)`, source.table)
			command, err := q.Exec(ctx, query, adjustmentID, line.Register, line.Amount,
				line.LineID, paymentID, version)
			if err != nil {
				return err
			}
			if command.RowsAffected() != 1 {
				return apperror.NewConflict("selected payment movement changed or exceeds available amount")
			}
		}
	}
	return r.validateSourceBalance(ctx, adjustmentID)
}

func (r *FinancialAdjustmentRepo) validateSourceBalance(ctx context.Context, adjustmentID id.ID) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var custody, merchant, fee string
	err := q.QueryRow(ctx, `SELECT
		COALESCE(SUM(CASE WHEN source_register='custody' THEN amount ELSE 0 END),0)::text,
		COALESCE(SUM(CASE WHEN source_register='merchant' THEN amount ELSE 0 END),0)::text,
		COALESCE(SUM(CASE WHEN source_register='fee' THEN amount ELSE 0 END),0)::text
		FROM reg_crypto_adjustment_sources WHERE adjustment_id=$1`, adjustmentID).
		Scan(&custody, &merchant, &fee)
	if err != nil {
		return err
	}
	c, ok := new(big.Int).SetString(custody, 10)
	if !ok || c.Sign() <= 0 {
		return apperror.NewValidation("select at least one custody movement")
	}
	m, _ := new(big.Int).SetString(merchant, 10)
	f, _ := new(big.Int).SetString(fee, 10)
	if c.Cmp(new(big.Int).Add(m, f)) != 0 {
		return apperror.NewValidation("selected custody amount must equal merchant amount plus fee amount")
	}
	return nil
}

func findAdjustmentRegister(kind string) (adjustmentRegister, bool) {
	for _, source := range adjustmentRegisters {
		if source.kind == kind {
			return source, true
		}
	}
	return adjustmentRegister{}, false
}

type adjustmentRegister struct {
	kind, table, dimensions string
}

var adjustmentRegisters = []adjustmentRegister{
	{"custody", "reg_crypto_balance_movements", "wallet_id,token_id"},
	{"merchant", "reg_crypto_merchant_balance_movements", "merchant_id,token_id"},
	{"fee", "reg_crypto_fee_movements", "merchant_id,token_id,fee_type"},
}

func (r *FinancialAdjustmentRepo) PaymentSources(ctx context.Context, paymentID id.ID) (*cryptoapp.FinancialAdjustmentPaymentSources, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var status string
	var posted bool
	var version int
	if err := q.QueryRow(ctx, `SELECT status,posted,posted_version FROM doc_crypto_payments WHERE id=$1`, paymentID).
		Scan(&status, &posted, &version); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperror.NewNotFound("crypto_payment", paymentID)
		}
		return nil, err
	}
	if status != "settled" || !posted || version < 1 {
		return nil, apperror.NewConflict("payment is not settled and posted")
	}
	result := &cryptoapp.FinancialAdjustmentPaymentSources{PaymentID: paymentID, PostedVersion: version,
		Lines: []cryptoapp.FinancialAdjustmentSourceLine{}}
	for _, source := range adjustmentRegisters {
		query := fmt.Sprintf(`SELECT m.line_id,m.amount::text,
			COALESCE((SELECT SUM(r.amount) FROM %[1]s r WHERE r.reversal_of_line_id=m.line_id),0)::text,
			(m.amount-COALESCE((SELECT SUM(r.amount) FROM %[1]s r WHERE r.reversal_of_line_id=m.line_id),0))::text,
			m.token_id, %[2]s FROM %[1]s m
			WHERE m.recorder_type='CryptoPayment' AND m.recorder_id=$1 AND m.recorder_version=$2
			AND m.reversal_of_line_id IS NULL ORDER BY m.line_id`, source.table,
			map[string]string{"custody": "m.wallet_id::text", "merchant": "m.merchant_id::text", "fee": "m.merchant_id::text || ' / ' || m.fee_type"}[source.kind])
		rows, err := q.Query(ctx, query, paymentID, version)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			line := cryptoapp.FinancialAdjustmentSourceLine{Register: source.kind}
			if err := rows.Scan(&line.LineID, &line.Amount, &line.Reversed,
				&line.Available, &line.TokenID, &line.Dimension); err != nil {
				rows.Close()
				return nil, err
			}
			result.Lines = append(result.Lines, line)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return result, nil
}

func (r *FinancialAdjustmentRepo) Sources(ctx context.Context, adjustmentID id.ID) ([]cryptoapp.FinancialAdjustmentSourceSelection, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	result := []cryptoapp.FinancialAdjustmentSourceSelection{}
	for _, source := range adjustmentRegisters {
		query := fmt.Sprintf(`SELECT s.source_line_id,COALESCE(s.amount,m.amount)::text
			FROM reg_crypto_adjustment_sources s JOIN %s m ON m.line_id=s.source_line_id
			WHERE s.adjustment_id=$1 AND s.source_register=$2 ORDER BY s.source_line_id`, source.table)
		rows, err := q.Query(ctx, query, adjustmentID, source.kind)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			line := cryptoapp.FinancialAdjustmentSourceSelection{Register: source.kind}
			if err := rows.Scan(&line.LineID, &line.Amount); err != nil {
				rows.Close()
				return nil, err
			}
			result = append(result, line)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return result, nil
}

func (r *FinancialAdjustmentRepo) LockForDecision(ctx context.Context, adjustmentID id.ID) (*cryptoapp.FinancialAdjustment, error) {
	// Proposal and decision both take the source document/wallet before the
	// adjustment row. Adoption and payment posting use the same document lock.
	current, err := r.Get(ctx, adjustmentID)
	if err != nil {
		return nil, err
	}
	if current.Mode == "payment_reversal" {
		if _, err := r.PaymentBasis(ctx, current.PaymentID); err != nil {
			return nil, err
		}
	} else if err := NewCustodyRecognitionRepo().LockWallet(ctx, *current.WalletID, *current.TokenID); err != nil {
		return nil, err
	}
	var result cryptoapp.FinancialAdjustment
	err = pgxscan.Get(ctx, postgres.MustGetTxManager(ctx).GetQuerier(ctx), &result,
		adjustmentSelect+` WHERE id=$1 FOR UPDATE`, adjustmentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperror.NewNotFound("crypto_adjustment", adjustmentID)
	}
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (r *FinancialAdjustmentRepo) PostingBasis(ctx context.Context, adjustment *cryptoapp.FinancialAdjustment) (crypto.AdjustmentPaymentBasis, error) {
	var basis crypto.AdjustmentPaymentBasis
	if adjustment.Mode != "payment_reversal" {
		return basis, nil
	}
	return r.PaymentBasis(ctx, adjustment.PaymentID)
}

func (r *FinancialAdjustmentRepo) ApplyPosting(ctx context.Context, plan crypto.AdjustmentPostingPlan) (*cryptoapp.FinancialAdjustment, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	adjustment := plan.Adjustment
	adjustmentID, actorID, reason := adjustment.ID, plan.ActorID, plan.Reason
	var err error
	if plan.CompensatePayment {
		selections, err := r.Sources(ctx, adjustmentID)
		if err != nil {
			return nil, err
		}
		for _, source := range adjustmentRegisters {
			columns := strings.Split(source.dimensions, ",")
			selectedColumns := make([]string, len(columns))
			for i, column := range columns {
				selectedColumns[i] = "m." + column
			}
			query := fmt.Sprintf(`INSERT INTO %[1]s
				(line_id,recorder_id,recorder_type,recorder_version,period,record_type,
				 %[2]s,amount,reversal_of_line_id,reversal_reason)
				SELECT $5,$1,'CryptoAdjustment',1,NOW(),
				 CASE WHEN m.record_type='receipt' THEN 'expense' ELSE 'receipt' END,
				 %[3]s,COALESCE(s.amount,m.amount),m.line_id,'manual_financial_correction'
				FROM reg_crypto_adjustment_sources s JOIN %[1]s m ON m.line_id=s.source_line_id
				WHERE s.adjustment_id=$1 AND s.source_register=$2
				AND m.recorder_type='CryptoPayment' AND m.recorder_id=$3 AND m.recorder_version=$4
				AND s.source_line_id=$6`,
				source.table, source.dimensions, strings.Join(selectedColumns, ","))
			for _, selection := range selections {
				if selection.Register != source.kind {
					continue
				}
				command, err := q.Exec(ctx, query, adjustmentID, source.kind, adjustment.PaymentID, adjustment.PostedVersion, id.New(), selection.LineID)
				if err != nil {
					return nil, fmt.Errorf("post %s adjustment reversals: %w", source.kind, err)
				}
				if command.RowsAffected() != 1 {
					return nil, apperror.NewConflict("adjustment source movement changed")
				}
			}
		}
	} else {
		_, err = q.Exec(ctx, `INSERT INTO reg_crypto_balance_movements
			(line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
			VALUES($6,$1,'CryptoAdjustment',1,NOW(),$2,$3,$4,$5::numeric)`,
			adjustmentID, adjustment.Direction, *adjustment.WalletID, *adjustment.TokenID, adjustment.Amount, id.New())
		if err != nil {
			return nil, err
		}
		_, err = q.Exec(ctx, `INSERT INTO reg_crypto_wallet_variance_movements
			(adjustment_id,wallet_id,token_id,variance_type,amount,line_id)
			VALUES($1,$2,$3,$4,$5::numeric,$6)`, adjustmentID,
			*adjustment.WalletID, *adjustment.TokenID, plan.VarianceType, adjustment.Amount, id.New())
		if err != nil {
			return nil, err
		}
	}
	_, err = q.Exec(ctx, `UPDATE doc_crypto_adjustments
		SET status='posted',checker_id=$2,decision_reason=$3,decided_at=NOW()
		WHERE id=$1 AND status='pending_approval'`, adjustmentID, actorID, reason)
	if err != nil {
		return nil, err
	}
	if err := postgres.NewOutboxPublisher().Publish(ctx, domain.DomainEvent{
		AggregateType: "CryptoAdjustment", AggregateID: adjustmentID, EventType: "financial_correction_posted",
		Payload: map[string]any{"entityType": "CryptoAdjustment", "entityId": adjustmentID, "action": "financial_correction_posted",
			"basisKind": adjustment.BasisKind, "sourcePostingId": adjustment.SourcePostingID,
			"postedVersion": adjustment.PostedVersion},
	}); err != nil {
		return nil, err
	}
	return r.Get(ctx, adjustmentID)
}

func (r *FinancialAdjustmentRepo) RejectPending(ctx context.Context, adjustmentID, checkerID id.ID, reason string) (*cryptoapp.FinancialAdjustment, error) {
	_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
		UPDATE doc_crypto_adjustments
		SET status='rejected',checker_id=$2,decision_reason=$3,decided_at=NOW()
		WHERE id=$1 AND status='pending_approval'`, adjustmentID, checkerID, reason)
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, adjustmentID)
}

func (r *FinancialAdjustmentRepo) PaymentBasis(ctx context.Context, paymentID id.ID) (crypto.AdjustmentPaymentBasis, error) {
	var basis crypto.AdjustmentPaymentBasis
	if err := postgres.NewDocLocker().LockDocument(ctx, "CryptoPayment", paymentID); err != nil {
		return basis, err
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var walletID, tokenID id.ID
	err := q.QueryRow(ctx, `SELECT wallet_id,token_id FROM doc_crypto_payments WHERE id=$1`, paymentID).Scan(&walletID, &tokenID)
	if errors.Is(err, pgx.ErrNoRows) {
		return basis, apperror.NewNotFound("crypto_payment", paymentID)
	}
	if err != nil {
		return basis, err
	}
	if _, err := q.Exec(ctx, `SELECT fn_crypto_lock_custody_wallets(ARRAY(
		SELECT $2::uuid UNION SELECT wallet_id FROM reg_crypto_balance_movements WHERE recorder_id=$1 AND recorder_type='CryptoPayment'))`, paymentID, walletID); err != nil {
		return basis, err
	}
	err = q.QueryRow(ctx, `SELECT status,posted,posted_version FROM doc_crypto_payments WHERE id=$1 FOR UPDATE`, paymentID).Scan(&basis.Status, &basis.Posted, &basis.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return basis, apperror.NewNotFound("crypto_payment", paymentID)
	}
	return basis, err
}
func (r *FinancialAdjustmentRepo) WalletTokenCompatible(ctx context.Context, walletID, tokenID id.ID) (bool, error) {
	var compatible bool
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT EXISTS (
 SELECT 1 FROM cat_wallets w JOIN cat_tokens t ON t.network_id=w.network_id
 WHERE w.id=$1 AND t.id=$2 AND NOT w.deletion_mark AND NOT t.deletion_mark
 AND NOT w.is_folder AND NOT t.is_folder)`, walletID, tokenID).Scan(&compatible)
	if err == nil && compatible {
		err = NewCustodyRecognitionRepo().LockWallet(ctx, walletID, tokenID)
	}
	return compatible, err
}
