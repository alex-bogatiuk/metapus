package crypto_repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"

	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/domain/documents/crypto_payment"
	"metapus/pkg/sdk/entity"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/json"
	"metapus/pkg/sdk/postgres"
	"metapus/pkg/sdk/posting"
)

type BlockIngestionRepo struct{}

func NewBlockIngestionRepo() *BlockIngestionRepo { return &BlockIngestionRepo{} }

// ValidatePosting admits a payment only as the attribution of its receipt; the
// receipt carries the independently proven custody effect.
func (r *BlockIngestionRepo) ValidatePosting(_ context.Context, doc posting.Postable) error {
	payment, ok := doc.(*crypto_payment.CryptoPayment)
	if !ok {
		return nil
	}
	if payment.ReceiptID == nil || payment.VerificationID == nil {
		return crypto.ErrVerificationPending
	}
	return nil
}

func (r *BlockIngestionRepo) Cutover(ctx context.Context, network id.ID) (crypto.IngestionCutover, error) {
	var result crypto.IngestionCutover
	err := pgxscan.Get(ctx, postgres.MustGetTxManager(ctx).GetQuerier(ctx), &result, `SELECT network_id,start_height,start_hash,start_timestamp,mode,decoder_version,activated_at,shadow_paused,COALESCE(last_error_code,'') AS last_error_code,COALESCE(last_error_stream,'') AS last_error_stream,last_error_height,last_error_at FROM sys_crypto_block_ingestion WHERE network_id=$1`, network)
	if errors.Is(err, pgx.ErrNoRows) {
		err = crypto.ErrIngestionCutoverNotFound
	}
	return result, err
}

// CaptureBlock locks registration before selecting targets. Network data is
// already decoded, so the transaction performs only bounded DB/CPU work.
func (r *BlockIngestionRepo) CaptureBlock(ctx context.Context, capture crypto.CapturedBlock) error {
	state := capture.Commit.Candidate
	txm := postgres.MustGetTxManager(ctx)
	return txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		if err := lockCaptureRecipients(txCtx, capture.Events); err != nil {
			return err
		}
		q := txm.GetQuerier(txCtx)
		var mode string
		var decoderVersion int
		var shadowPaused bool
		if err := q.QueryRow(txCtx, `SELECT mode,decoder_version,shadow_paused FROM sys_crypto_block_ingestion WHERE network_id=$1 FOR UPDATE`, state.NetworkID).Scan(&mode, &decoderVersion, &shadowPaused); err != nil {
			return err
		}
		version := capture.DecoderVersion
		if version != crypto.TRONTransferEvidenceSchema || version != decoderVersion {
			return crypto.ErrDecoderVersionConflict
		}
		if mode == "paused" || (!capture.Shadow && mode == "shadow") {
			return crypto.ErrVerificationPending
		}
		if capture.Shadow && shadowPaused {
			return crypto.ErrShadowPaused
		}
		var previous int64
		var previousHash string
		if err := q.QueryRow(txCtx, `SELECT last_block,COALESCE(fingerprint,'') FROM sys_chain_watcher_state
 WHERE network_id=$1 AND watcher_key=$2 AND lease_owner=$3 AND fencing_token=$4 AND lease_expires_at>NOW() FOR UPDATE`, state.NetworkID, state.WatcherKey, state.LeaseOwner, state.FencingToken).Scan(&previous, &previousHash); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return crypto.ErrWatcherLeaseLost
			}
			return err
		}
		if previous != capture.Commit.ExpectedBlock || previousHash != capture.Commit.ExpectedHash {
			return crypto.ErrWatcherCheckpointConflict
		}
		if capture.Sequential && (state.LastBlock != previous+1 || !strings.EqualFold(capture.ParentHash, previousHash)) {
			return crypto.ErrVerificationMismatch
		}
		var targets []struct {
			Kind   string `db:"target_kind"`
			Key    string `db:"target_key"`
			Height int64  `db:"start_height"`
		}
		if err := pgxscan.Select(txCtx, q, &targets, `SELECT target_kind,target_key,start_height FROM sys_crypto_watch_targets WHERE network_id=$1`, state.NetworkID); err != nil {
			return err
		}
		addresses, assets := map[string]int64{}, map[string]int64{}
		for _, target := range targets {
			if target.Kind == "address" {
				addresses[target.Key] = target.Height
			} else {
				assets[target.Key] = target.Height
			}
		}
		coverage := crypto.NewAMLCoverageIndex(capture.Events, capture.Sources, version)
		for _, event := range capture.Events {
			if event.EffectiveEvidenceSchema() != version {
				return crypto.ErrDecoderVersionConflict
			}
			if event.NetworkID != state.NetworkID || event.BlockNumber != capture.Commit.Block.BlockNumber || event.BlockHash != capture.Commit.Block.BlockHash {
				return fmt.Errorf("captured event/block mismatch")
			}
			fromStart, fromOwned := addresses[event.FromAddress]
			toStart, toOwned := addresses[event.ToAddress]
			fromOwned = fromOwned && event.BlockNumber >= fromStart
			toOwned = toOwned && event.BlockNumber >= toStart
			if !fromOwned && !toOwned {
				continue
			}
			if event.TokenContract != "" {
				start, supported := assets[event.TokenContract]
				if !supported || event.BlockNumber < start {
					continue
				}
			}
			switch {
			case fromOwned && toOwned:
				event.Direction = crypto.TransferDirectionInternal
			case fromOwned:
				event.Direction = crypto.TransferDirectionOutgoing
			default:
				event.Direction = crypto.TransferDirectionIncoming
			}
			if event.Finality == crypto.FinalityProvisional && event.Direction != crypto.TransferDirectionIncoming {
				continue
			}
			if len(capture.Sources) > 0 && !capture.Shadow {
				proof := crypto.TransferVerification{MatchingEffects: coverage.CoverageFor(event).MatchingEffects(), Event: event, Sources: capture.Sources, DecoderVersion: version, VerifiedAt: time.Now().UTC(), Outcome: "verified", ExecutionStatus: crypto.ReceiptSucceeded}
				if err := proof.BindTRONAccountingIdentity(); err != nil {
					return err
				}
				proofID, err := r.SaveVerification(txCtx, proof)
				if err != nil {
					return err
				}
				event.VerificationID = &proofID
			}
			if !capture.Shadow {
				if err := NewChainObservationRepo().Capture(txCtx, event); err != nil {
					return err
				}
			} else {
				event.ComparisonSources = capture.Sources
				if err := NewChainObservationRepo().CaptureShadow(txCtx, event); err != nil {
					return err
				}
			}
		}
		if capture.Sequential {
			return NewWatcherStateRepo().CommitBlock(txCtx, capture.Commit)
		}
		// Older latest-window blocks may capture evidence without advancing its
		// watermark. Every capture still requires the current live fence.
		if state.LastBlock > previous {
			updated, err := q.Exec(txCtx, `UPDATE sys_chain_watcher_state SET last_block=$5,last_timestamp=$6,fingerprint=$7,updated_at=NOW()
 WHERE network_id=$1 AND watcher_key=$2 AND lease_owner=$3 AND fencing_token=$4 AND lease_expires_at>NOW()`, state.NetworkID, state.WatcherKey, state.LeaseOwner, state.FencingToken, state.LastBlock, state.LastTimestamp, state.Fingerprint)
			if err != nil {
				return err
			}
			if updated.RowsAffected() != 1 {
				return crypto.ErrWatcherLeaseLost
			}
		}
		return NewWatcherStateRepo().SaveBlockFingerprint(txCtx, state.NetworkID, state.WatcherKey, capture.Commit.Block, capture.Commit.RetainFrom)
	})
}

func (r *BlockIngestionRepo) SaveVerification(ctx context.Context, proof crypto.TransferVerification) (id.ID, error) {
	var result id.ID
	err := postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		var err error
		result, err = r.saveVerification(ctx, proof)
		return err
	})
	return result, err
}

func (r *BlockIngestionRepo) saveVerification(ctx context.Context, proof crypto.TransferVerification) (id.ID, error) {
	if err := proof.Validate(); err != nil {
		return id.ID{}, err
	}
	if err := lockVerificationRecipient(ctx, proof.Event); err != nil {
		return id.ID{}, err
	}
	var version int
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT decoder_version FROM sys_crypto_block_ingestion WHERE network_id=$1 FOR SHARE`, proof.Event.NetworkID).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		version = crypto.TransferEvidenceSchemaV1
	} else if err != nil {
		return id.ID{}, err
	}
	if version != proof.DecoderVersion {
		return id.ID{}, crypto.ErrDecoderVersionConflict
	}
	if id.IsNil(proof.ID) {
		proof.ID = id.New()
	}
	event := proof.Event
	fingerprint := sha256.Sum256([]byte(proof.Sources[0].Digest + ":" + proof.Sources[0].HeadHash + ":" + string(event.Canonicality)))
	evidenceHash := hex.EncodeToString(fingerprint[:])
	proof.Event.VerificationID = nil
	raw, err := json.Marshal(proof)
	if err != nil {
		return id.ID{}, err
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var result id.ID
	err = q.QueryRow(ctx, `INSERT INTO reg_crypto_transfer_verifications(id,network_id,tx_hash,event_kind,event_index,block_number,block_hash,economics_hash,confirmations,evidence,verified_at,evidence_hash)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12) ON CONFLICT(network_id,tx_hash,event_kind,event_index,block_hash,economics_hash,confirmations,evidence_hash) DO NOTHING RETURNING id`,
		proof.ID, event.NetworkID, event.TxHash, event.EventKind, event.EventIndex, event.BlockNumber, event.BlockHash, event.EconomicsHash(), event.Confirmations, string(raw), proof.VerifiedAt, evidenceHash).Scan(&result)
	if errors.Is(err, pgx.ErrNoRows) {
		err = q.QueryRow(ctx, `SELECT id FROM reg_crypto_transfer_verifications WHERE network_id=$1 AND tx_hash=$2 AND event_kind=$3 AND event_index=$4 AND block_hash=$5 AND economics_hash=$6 AND confirmations=$7 AND evidence_hash=$8`, event.NetworkID, event.TxHash, event.EventKind, event.EventIndex, event.BlockHash, event.EconomicsHash(), event.Confirmations, evidenceHash).Scan(&result)
	}
	if err != nil {
		return result, err
	}
	// All principal writers acquire AML scope before wallet/effect locks.
	// Keep proof ingestion in that order; failures still roll back both facts.
	if err := NewAMLRepository().ObserveVerification(ctx, result); err != nil {
		return id.ID{}, err
	}
	if err := r.indexAccountingEvidence(ctx, result); err != nil {
		return id.ID{}, err
	}
	return result, nil
}

// Authorize returns the independent proof of the final canonical payment
// transfer. Without it the payment cannot attribute a receipt.
func (r *BlockIngestionRepo) Authorize(ctx context.Context, payment *crypto_payment.CryptoPayment, event crypto.BlockchainEvent) (id.ID, error) {
	return r.authorizeOutcome(ctx, accountingPaymentBasis(payment), event, crypto.CanonicalityCanonical, true)
}

// AuthorizeReorg returns the independent proof that the payment transfer is no
// longer canonical. For a posted payment it also reports the canonicality
// failure; the caller compensates the attribution and holds the network.
func (r *BlockIngestionRepo) AuthorizeReorg(ctx context.Context, payment *crypto_payment.CryptoPayment, event crypto.BlockchainEvent) (id.ID, error) {
	proofID, err := r.authorizeOutcome(ctx, accountingPaymentBasis(payment), event, crypto.CanonicalityOrphaned, payment.Posted)
	if err != nil || id.IsNil(proofID) || !payment.Posted {
		return proofID, err
	}
	if err := r.ReportIngestionFailure(ctx, payment.NetworkID, "canonicality", payment.BlockNumber, "solidified_payment_changed", true); err != nil {
		return id.ID{}, err
	}
	return proofID, nil
}

func (r *BlockIngestionRepo) ExpectedTransfer(ctx context.Context, payment *crypto_payment.CryptoPayment) (crypto.BlockchainEvent, error) {
	return r.ExpectedAccountingTransfer(ctx, accountingPaymentBasis(payment))
}

func accountingPaymentBasis(payment *crypto_payment.CryptoPayment) crypto.AccountingTransferBasis {
	return crypto.AccountingTransferBasis{
		NetworkID: payment.NetworkID, WalletID: payment.WalletID, TokenID: payment.TokenID,
		TxHash: payment.TxHash, EventKind: crypto.EventKind(payment.EventKind), EventIndex: payment.EventIndex,
		FromAddress: payment.FromAddress, Amount: payment.Amount, BlockNumber: payment.BlockNumber,
		BlockHash: payment.BlockHash, RequiredConfs: payment.RequiredConfs, VerificationID: payment.VerificationID,
	}
}

func (r *BlockIngestionRepo) ExpectedAccountingTransfer(ctx context.Context, basis crypto.AccountingTransferBasis) (crypto.BlockchainEvent, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var to, contract, family string
	if err := q.QueryRow(ctx, `SELECT w.address,t.contract_address,n.chain_family FROM cat_wallets w,cat_tokens t,cat_blockchain_networks n WHERE w.id=$1 AND t.id=$2 AND w.network_id=$3 AND t.network_id=$3 AND n.id=$3`, basis.WalletID, basis.TokenID, basis.NetworkID).Scan(&to, &contract, &family); err != nil {
		return crypto.BlockchainEvent{}, err
	}
	event := crypto.BlockchainEvent{NetworkID: basis.NetworkID, TxHash: basis.TxHash, EventKind: basis.EventKind, EventIndex: basis.EventIndex, FromAddress: basis.FromAddress, ToAddress: to, TokenContract: contract, Amount: basis.Amount, BlockNumber: basis.BlockNumber, BlockHash: basis.BlockHash, RequiredConfs: basis.RequiredConfs, EventType: crypto.EventTypeTransfer}
	if basis.RecordType == entity.RecordTypeExpense {
		event.FromAddress, event.ToAddress = to, basis.ToAddress
	}
	if basis.VerificationID != nil {
		var raw []byte
		if err := q.QueryRow(ctx, `SELECT evidence FROM reg_crypto_transfer_verifications WHERE id=$1 AND network_id=$2 AND tx_hash=$3 AND event_kind=$4 AND event_index=$5`, basis.VerificationID, basis.NetworkID, basis.TxHash, basis.EventKind, basis.EventIndex).Scan(&raw); err != nil {
			return crypto.BlockchainEvent{}, err
		}
		var proof crypto.TransferVerification
		if err := json.Unmarshal(raw, &proof); err != nil {
			return crypto.BlockchainEvent{}, err
		}
		if err := proof.Validate(); err != nil {
			return crypto.BlockchainEvent{}, err
		}
		// Catalog display casing is not an EVM address identity. Preserve the
		// original proof's byte contract after checking the same address/asset.
		if family == string(crypto.ChainFamilyEVM) {
			if strings.EqualFold(event.FromAddress, proof.Event.FromAddress) {
				event.FromAddress = proof.Event.FromAddress
			}
			if strings.EqualFold(event.ToAddress, proof.Event.ToAddress) {
				event.ToAddress = proof.Event.ToAddress
			}
			if strings.EqualFold(event.TokenContract, proof.Event.TokenContract) {
				event.TokenContract = proof.Event.TokenContract
			}
		}
		event.DecoderVersion = proof.Event.DecoderVersion
		event.Timestamp = proof.Event.Timestamp
		event.TraceHash = proof.Event.TraceHash
		event.TraceIndex = proof.Event.TraceIndex
		event.CallValueIndex = proof.Event.CallValueIndex
	}
	return event, nil
}

func (r *BlockIngestionRepo) authorizeOutcome(ctx context.Context, basis crypto.AccountingTransferBasis, event crypto.BlockchainEvent, outcome crypto.CanonicalityStatus, strict bool) (id.ID, error) {
	return r.authorizeOutcomeWithRecovery(ctx, basis, event, outcome, strict, false)
}

func (r *BlockIngestionRepo) authorizeOutcomeWithRecovery(ctx context.Context, basis crypto.AccountingTransferBasis, event crypto.BlockchainEvent, outcome crypto.CanonicalityStatus, strict, recovery bool) (id.ID, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var required bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sys_crypto_block_ingestion WHERE network_id=$1 AND activated_at IS NOT NULL)`, basis.NetworkID).Scan(&required); err != nil {
		return id.ID{}, err
	}
	if !required && !strict {
		return id.ID{}, nil
	}
	if outcome == crypto.CanonicalityCanonical && !recovery {
		var blocked bool
		if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sys_crypto_block_ingestion WHERE network_id=$1 AND mode='paused')
 OR EXISTS(SELECT 1 FROM reg_crypto_network_holds WHERE network_id=$1 AND owner_type='block_ingestion' AND status='open')`, basis.NetworkID).Scan(&blocked); err != nil {
			return id.ID{}, err
		}
		if blocked {
			return id.ID{}, crypto.ErrVerificationPending
		}
	}
	expected, err := r.ExpectedAccountingTransfer(ctx, basis)
	if err != nil {
		return id.ID{}, err
	}
	if event.FromAddress != "" && event.ToAddress != "" && event.EconomicsHash() != expected.EconomicsHash() {
		return id.ID{}, crypto.ErrVerificationMismatch
	}
	threshold := basis.RequiredConfs
	if outcome == crypto.CanonicalityOrphaned {
		threshold = 1
	}
	if event.BlockHash == "" || event.Finality != crypto.FinalitySolidified || event.Canonicality != outcome || event.Confirmations < threshold {
		return id.ID{}, crypto.ErrVerificationPending
	}
	var proofID id.ID
	var raw []byte
	err = q.QueryRow(ctx, `SELECT id,evidence FROM reg_crypto_transfer_verifications
 WHERE network_id=$1 AND tx_hash=$2 AND event_kind=$3 AND event_index=$4 AND block_hash=$5 AND economics_hash=$6 AND confirmations>=$7
 AND ($8::uuid IS NULL OR id=$8) AND evidence->'event'->>'canonicality'=$9 ORDER BY confirmations DESC LIMIT 1`, basis.NetworkID, basis.TxHash, basis.EventKind, basis.EventIndex, event.BlockHash, expected.EconomicsHash(), threshold, event.VerificationID, string(outcome)).Scan(&proofID, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return id.ID{}, crypto.ErrVerificationPending
	}
	if err != nil {
		return id.ID{}, err
	}
	var proof crypto.TransferVerification
	if err := json.Unmarshal(raw, &proof); err != nil {
		return id.ID{}, err
	}
	if err := proof.Validate(); err != nil {
		return id.ID{}, err
	}
	if proof.Event.EconomicsHash() != expected.EconomicsHash() || proof.Event.BlockNumber != event.BlockNumber || proof.Event.BlockHash != event.BlockHash || proof.Event.Confirmations < event.Confirmations {
		return id.ID{}, crypto.ErrVerificationMismatch
	}
	return proofID, nil
}

var _ crypto.BlockCaptureRepository = (*BlockIngestionRepo)(nil)
var _ crypto.FinancialEventGuard = (*BlockIngestionRepo)(nil)

func (r *BlockIngestionRepo) ReportIngestionFailure(ctx context.Context, network id.ID, stream string, height int64, category string, processed bool) error {
	txm := postgres.MustGetTxManager(ctx)
	return txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		q := txm.GetQuerier(txCtx)
		_, err := q.Exec(txCtx, `UPDATE sys_crypto_block_ingestion SET last_error_code=$2,last_error_stream=$3,last_error_height=$4,last_error_at=CASE WHEN $2='' THEN NULL ELSE NOW() END WHERE network_id=$1 AND (($2<>'' AND (last_error_code,last_error_stream,last_error_height) IS DISTINCT FROM ($2::text,$3::text,$4::bigint) AND ($3<>'tron:latest' OR COALESCE(last_error_code,'')='' OR last_error_stream='tron:latest')) OR ($2='' AND last_error_stream=$3 AND COALESCE(last_error_code,'')<>''))`, network, category, stream, height)
		if err != nil {
			return err
		}
		if !processed || category != "provider_disagreement" && category != "transfer_mismatch" && category != "solidified_payment_changed" {
			return nil
		}
		var active bool
		if err := q.QueryRow(txCtx, `SELECT EXISTS(SELECT 1 FROM sys_crypto_block_ingestion WHERE network_id=$1 AND activated_at IS NOT NULL)`, network).Scan(&active); err != nil {
			return err
		}
		if !active {
			return nil
		}
		return NewOperationalHoldRepo().AcquireOperationalHold(txCtx, cryptoapp.AcquireOperationalHoldCommand{
			Scope: cryptoapp.OperationalHoldScopeNetwork, ScopeID: network, Reason: "manual", OwnerType: "block_ingestion",
			OwnerID: fmt.Sprintf("%s:%s:%d", network, category, height), HeldBy: "crypto-worker",
			Details: map[string]any{"stream": stream, "height": height, "cause": category},
		})
	})
}
