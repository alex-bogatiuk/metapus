package crypto_repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"
	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_custody_receipt"
	"metapus/internal/infrastructure/storage/postgres"
)

// ReceiptAuditTask is one accounted receipt whose transfer is re-proven.
type ReceiptAuditTask struct {
	ReceiptID     id.ID              `db:"receipt_id"`
	NetworkID     id.ID              `db:"network_id"`
	WalletID      id.ID              `db:"wallet_id"`
	TokenID       id.ID              `db:"token_id"`
	Amount        types.CryptoAmount `db:"amount"`
	TxHash        string             `db:"tx_hash"`
	EventKind     string             `db:"event_kind"`
	EventIndex    int64              `db:"event_index"`
	BlockNumber   int64              `db:"block_number"`
	BlockHash     string             `db:"block_hash"`
	FromAddress   string             `db:"from_address"`
	RequiredConfs int                `db:"required_confs"`
}

func (t ReceiptAuditTask) Basis() crypto.AccountingTransferBasis {
	return crypto.AccountingTransferBasis{
		NetworkID: t.NetworkID, WalletID: t.WalletID, TokenID: t.TokenID, TxHash: t.TxHash,
		EventKind: crypto.EventKind(t.EventKind), EventIndex: t.EventIndex, FromAddress: t.FromAddress,
		RecordType: entity.RecordTypeReceipt, Amount: t.Amount, BlockNumber: t.BlockNumber, BlockHash: t.BlockHash,
		RequiredConfs: t.RequiredConfs,
	}
}

type ReceiptAuditRepo struct{}

func NewReceiptAuditRepo() *ReceiptAuditRepo { return &ReceiptAuditRepo{} }

func (*ReceiptAuditRepo) Claim(ctx context.Context, workerID string, limit int, lease time.Duration) ([]ReceiptAuditTask, error) {
	if workerID == "" || limit <= 0 || lease < time.Second {
		return nil, fmt.Errorf("receipt audit claim requires owner, limit and lease")
	}
	var tasks []ReceiptAuditTask
	err := pgxscan.Select(ctx, postgres.MustGetTxManager(ctx).GetQuerier(ctx), &tasks, `
		WITH due AS (
			SELECT receipt_id FROM reg_crypto_custody_receipt_audits
			WHERE status='canonical' AND next_check_at<=NOW()
			  AND (lease_expires_at IS NULL OR lease_expires_at<=NOW())
			ORDER BY next_check_at,receipt_id LIMIT $2 FOR UPDATE SKIP LOCKED
		)
		UPDATE reg_crypto_custody_receipt_audits a
		SET claimed_by=$1,lease_expires_at=NOW()+$3::BIGINT*INTERVAL '1 second'
		FROM due JOIN doc_crypto_custody_receipts d ON d.id=due.receipt_id
		JOIN reg_crypto_transfer_verifications v ON v.id=d.verification_id
		JOIN cat_blockchain_networks n ON n.id=d.network_id
		WHERE a.receipt_id=due.receipt_id
		RETURNING d.id AS receipt_id,d.network_id,d.wallet_id,d.token_id,d.amount,v.tx_hash,v.event_kind,v.event_index,
		          v.block_number,v.block_hash,COALESCE(v.evidence->'event'->>'fromAddress','') AS from_address,
		          GREATEST(COALESCE(n.confirmations_needed,1),1) AS required_confs`,
		workerID, limit, int64(lease/time.Second))
	if err != nil {
		return nil, fmt.Errorf("claim receipt canonicality audit: %w", err)
	}
	return tasks, nil
}

// Release schedules the next check: every 15 minutes while canonical, with a
// capped backoff after a failed read.
func (*ReceiptAuditRepo) Release(ctx context.Context, receiptID id.ID, workerID string, checkErr error) error {
	message := ""
	if checkErr != nil {
		message = checkErr.Error()
		if len(message) > 500 {
			message = message[:500]
		}
	}
	_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
		UPDATE reg_crypto_custody_receipt_audits
		SET claimed_by='',lease_expires_at=NULL,last_error=$3,
		    last_checked_at=CASE WHEN $3='' THEN NOW() ELSE last_checked_at END,
		    attempts=CASE WHEN $3='' THEN 0 ELSE attempts+1 END,
		    next_check_at=NOW()+(CASE WHEN $3='' THEN 900 ELSE LEAST(900,60*(1 << LEAST(attempts,4))) END
		        + FLOOR(RANDOM()*30)::INT)*INTERVAL '1 second'
		WHERE receipt_id=$1 AND claimed_by=$2 AND status='canonical'`, receiptID, workerID, message)
	if err != nil {
		return fmt.Errorf("release receipt canonicality audit: %w", err)
	}
	return nil
}

type receiptOrphanedEvent struct {
	EntityType     string `json:"entityType"`
	EntityID       string `json:"entityId"`
	Action         string `json:"action"`
	IncidentID     string `json:"incidentId"`
	VerificationID string `json:"verificationId"`
}

// MarkOrphaned records a proven orphaning of an accounted receipt: one
// incident, a network hold and a wallet hold. Ledger rows stay untouched; the
// receipt can no longer be attributed until the incident is investigated.
func (*ReceiptAuditRepo) MarkOrphaned(ctx context.Context, task ReceiptAuditTask, proofID id.ID) error {
	tm := postgres.MustGetTxManager(ctx)
	return tm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := tm.GetQuerier(ctx)
		var status string
		err := q.QueryRow(ctx, `SELECT status FROM reg_crypto_custody_receipt_audits WHERE receipt_id=$1 FOR UPDATE`, task.ReceiptID).Scan(&status)
		if err != nil {
			return fmt.Errorf("lock receipt audit: %w", err)
		}
		if status == "orphaned" {
			return nil
		}
		fields := map[string]any{
			"receiptId": task.ReceiptID.String(), "walletId": task.WalletID.String(), "tokenId": task.TokenID.String(),
			"amount": task.Amount.String(), "blockNumber": task.BlockNumber, "blockHash": task.BlockHash,
		}
		details, err := json.Marshal(fields)
		if err != nil {
			return err
		}
		var incident id.ID
		err = q.QueryRow(ctx, `INSERT INTO reg_crypto_security_incidents(id,network_id,incident_kind,tx_hash,event_kind,event_index,details)
			VALUES($1,$2,'deep_reorg',$3,$4,$5,$6::jsonb)
			ON CONFLICT(network_id,incident_kind,tx_hash,event_kind,event_index) DO NOTHING RETURNING id`,
			id.New(), task.NetworkID, task.TxHash, task.EventKind, task.EventIndex, string(details)).Scan(&incident)
		if errors.Is(err, pgx.ErrNoRows) {
			var sameReceipt bool
			err = q.QueryRow(ctx, `SELECT id,details->>'receiptId'=$5 FROM reg_crypto_security_incidents
				WHERE network_id=$1 AND incident_kind='deep_reorg' AND tx_hash=$2 AND event_kind=$3 AND event_index=$4`,
				task.NetworkID, task.TxHash, task.EventKind, task.EventIndex, task.ReceiptID.String()).Scan(&incident, &sameReceipt)
			if err == nil && !sameReceipt {
				return apperror.NewConflict("reorg incident of this transfer belongs to another owner")
			}
		}
		if err != nil {
			return fmt.Errorf("record receipt reorg incident: %w", err)
		}
		holds := cryptoapp.NewOperationalHoldService(tm, NewOperationalHoldRepo())
		for _, target := range []struct {
			scope cryptoapp.OperationalHoldScope
			id    id.ID
		}{{cryptoapp.OperationalHoldScopeNetwork, task.NetworkID}, {cryptoapp.OperationalHoldScopeWallet, task.WalletID}} {
			if err := holds.Acquire(ctx, cryptoapp.AcquireOperationalHoldCommand{
				Scope: target.scope, ScopeID: target.id, Reason: "deep_reorg",
				OwnerType: "security_incident", OwnerID: incident.String(), HeldBy: "crypto-worker",
				Details: map[string]any{"receiptId": task.ReceiptID.String(), "txHash": task.TxHash},
			}); err != nil {
				return fmt.Errorf("hold %s after receipt reorg: %w", target.scope, err)
			}
		}
		if _, err := q.Exec(ctx, `UPDATE reg_crypto_custody_receipt_audits
			SET status='orphaned',orphaned_at=NOW(),orphan_verification_id=$2,incident_id=$3,claimed_by='',lease_expires_at=NULL,last_error=''
			WHERE receipt_id=$1`, task.ReceiptID, proofID, incident); err != nil {
			return fmt.Errorf("mark receipt orphaned: %w", err)
		}
		return postgres.NewOutboxPublisher().Publish(ctx, domain.DomainEvent{
			AggregateType: crypto_custody_receipt.DocumentType, AggregateID: task.ReceiptID, EventType: "orphaned",
			Payload: receiptOrphanedEvent{EntityType: crypto_custody_receipt.DocumentType, EntityID: task.ReceiptID.String(),
				Action: "orphaned", IncidentID: incident.String(), VerificationID: proofID.String()},
		})
	})
}
