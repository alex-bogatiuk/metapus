package crypto

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/domain/documents/crypto_payment"
)

var ErrVerificationPending = errors.New("chain transfer verification pending")
var ErrVerificationMismatch = errors.New("chain transfer evidence mismatch")
var ErrChainQuorumUnavailable = errors.New("independent chain RPC quorum unavailable")
var ErrIngestionCutoverNotFound = errors.New("block ingestion cutover not found")
var ErrIngestionRegistrationBusy = apperror.NewConflict("ingestion registration is busy; retry the operation")
var ErrDecoderVersionConflict = errors.New("transfer decoder version conflict")

// Transfer evidence schema identifiers are persisted and participate in hashes.
// Schema 1 remains the current EVM proof format and the historical event format.
const TransferEvidenceSchemaV1 = 1

// TRONTransferEvidenceSchema binds stable internal-transfer identity and the
// normalized/source digest pair. Its value and hash preimages must not change.
const TRONTransferEvidenceSchema = 2

func ValidTransferEvidenceSchema(version int) bool {
	return version == TransferEvidenceSchemaV1 || version == TRONTransferEvidenceSchema
}

// EffectiveEvidenceSchema interprets the omitted schema of historical events.
// It is not a default for newly captured TRON blocks.
func (e BlockchainEvent) EffectiveEvidenceSchema() int {
	if e.DecoderVersion == 0 {
		return TransferEvidenceSchemaV1
	}
	return e.DecoderVersion
}

type TraceExclusion struct {
	Note  string `json:"note"`
	Count int    `json:"count"`
}

// NetworkWatcherFactory owns all network streams. Families implementing it
// bypass per-asset factories so block processing has a single owner.
type NetworkWatcherFactory interface {
	NetworkWatchers(context.Context, NetworkRef, []AssetRef, []string) ([]ManagedWatcher, error)
}

type VerificationSource struct {
	ProviderID       string           `json:"providerId"`
	LatestProviderID string           `json:"latestProviderId"`
	TrustGroup       string           `json:"trustGroup"`
	Digest           string           `json:"digest"`
	HeadHeight       int64            `json:"headHeight"`
	HeadHash         string           `json:"headHash"`
	DecoderVersion   int              `json:"decoderVersion,omitempty"`
	SourceDigest     string           `json:"sourceDigest,omitempty"`
	ExcludedTraces   []TraceExclusion `json:"excludedTraces,omitempty"`
}

type TransferVerification struct {
	// Optional independent schema; historical transfer/economics bytes remain
	// unchanged when absent. Only family verifiers bind a new identity.
	AccountingIdentity *AccountingEffectIdentity `json:"accountingIdentity,omitempty"`
	// Zero means unknown coverage for AML; old proofs remain valid for money.
	MatchingEffects int                  `json:"matchingEffects,omitempty"`
	ID              id.ID                `json:"id"`
	Event           BlockchainEvent      `json:"event"`
	Sources         []VerificationSource `json:"sources"`
	DecoderVersion  int                  `json:"decoderVersion"`
	VerifiedAt      time.Time            `json:"verifiedAt"`
	Outcome         string               `json:"outcome"`
	ExecutionStatus ReceiptStatus        `json:"executionStatus"`
}

func (v TransferVerification) Validate() error {
	e := v.Event
	if v.DecoderVersion == TRONTransferEvidenceSchema && e.EffectiveEventKind() == EventKindInternalValue {
		if raw, err := hex.DecodeString(e.TraceHash); err != nil || len(raw) != 32 || e.CallValueIndex == nil || *e.CallValueIndex < 0 {
			return fmt.Errorf("invalid internal transfer identity")
		}
	}
	if v.MatchingEffects < 0 || id.IsNil(e.NetworkID) || e.TxHash == "" || !e.EventKind.Valid() || e.EventIndex < 0 ||
		e.BlockHash == "" || e.BlockNumber < 0 || !e.Amount.IsPositive() || e.FromAddress == "" || e.ToAddress == "" ||
		e.Finality != FinalitySolidified || (e.Canonicality != CanonicalityCanonical && (e.Canonicality != CanonicalityOrphaned || e.EventType != EventTypeReorg)) || !ValidTransferEvidenceSchema(v.DecoderVersion) || e.EffectiveEvidenceSchema() != v.DecoderVersion ||
		len(v.Sources) < 2 || v.VerifiedAt.IsZero() {
		return fmt.Errorf("invalid transfer verification")
	}
	groups := make(map[string]bool)
	providers := make(map[string]bool)
	first := v.Sources[0]
	if e.Canonicality == CanonicalityCanonical && (v.Outcome != "verified" || v.ExecutionStatus != ReceiptSucceeded) {
		return fmt.Errorf("transfer execution not verified")
	}
	if e.Canonicality == CanonicalityOrphaned && v.Outcome != "absent" {
		return fmt.Errorf("transfer absence not verified")
	}
	for _, s := range v.Sources {
		version := s.DecoderVersion
		if version == 0 {
			version = TransferEvidenceSchemaV1
		}
		if version != v.DecoderVersion || (version == TRONTransferEvidenceSchema && s.SourceDigest == "") {
			return ErrDecoderVersionConflict
		}
		if s.ProviderID == "" || s.LatestProviderID == "" || s.TrustGroup == "" || s.Digest == "" || s.HeadHash == "" || s.HeadHeight < e.BlockNumber ||
			groups[s.TrustGroup] || providers[s.ProviderID] || s.Digest != first.Digest || s.HeadHeight != first.HeadHeight || s.HeadHash != first.HeadHash {
			return fmt.Errorf("invalid independent transfer quorum")
		}
		groups[s.TrustGroup], providers[s.ProviderID] = true, true
	}
	if int64(e.Confirmations) != first.HeadHeight-e.BlockNumber+1 {
		return fmt.Errorf("transfer confirmation evidence mismatch")
	}
	if v.AccountingIdentity != nil {
		if err := v.AccountingIdentity.ValidateEvent(e); err != nil {
			return err
		}
		if _, err := v.AccountingIdentity.Occurrence(e); err != nil {
			return err
		}
	}
	return nil
}

// EconomicsHash excludes mutable confirmation metadata and transport JSON.
// Different observations of the same transfer must bind identical economics.
func (e BlockchainEvent) EconomicsHash() string {
	if e.EffectiveEvidenceSchema() == TRONTransferEvidenceSchema && e.EffectiveEventKind() == EventKindInternalValue {
		raw, _ := json.Marshal(struct {
			Version                    int
			Network                    id.ID
			Tx, Trace                  string
			CallIndex                  *int64
			Kind                       EventKind
			Index                      int64
			From, To, Contract, Amount string
		}{TRONTransferEvidenceSchema, e.NetworkID, strings.ToLower(e.TxHash), strings.ToLower(e.TraceHash), e.CallValueIndex, e.EffectiveEventKind(), e.EventIndex, e.FromAddress, e.ToAddress, e.TokenContract, e.Amount.String()})
		hash := sha256.Sum256(raw)
		return hex.EncodeToString(hash[:])
	}
	raw, _ := json.Marshal(struct {
		Network                    id.ID
		Tx                         string
		Kind                       EventKind
		Index                      int64
		From, To, Contract, Amount string
	}{e.NetworkID, strings.ToLower(e.TxHash), e.EffectiveEventKind(), e.EventIndex, e.FromAddress, e.ToAddress, e.TokenContract, e.Amount.String()})
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

type TransferVerifier interface {
	VerificationRequired() bool
	VerifyTransfer(context.Context, BlockchainEvent) (TransferVerification, error)
}

// ExpectedTransferVerifier locates one exact signed transfer by economics,
// independently of block-global observation indexes. Ambiguity is an error.
type ExpectedTransferVerifier interface {
	VerifyExpectedTransfer(context.Context, BlockchainEvent) (TransferVerification, error)
}

type IngestionFailureRecorder interface {
	ReportIngestionFailure(context.Context, id.ID, string, int64, string, bool) error
}

// FinancialEventGuard runs in the financial transaction, without RPC. It
// binds immutable evidence to the locked document and returns its audit ID.
type FinancialEventGuard interface {
	Authorize(context.Context, *crypto_payment.CryptoPayment, BlockchainEvent) (id.ID, error)
	AuthorizeReorg(context.Context, *crypto_payment.CryptoPayment, BlockchainEvent) (id.ID, error)
}

// IncomingFinancialLocker coordinates catalog and accounting locks before the
// event processor locks an incoming wallet or an existing payment row.
type IncomingFinancialLocker interface {
	LockIncomingEvent(context.Context, BlockchainEvent) error
}

type IngestionCutover struct {
	ShadowPaused    bool       `db:"shadow_paused" json:"shadowPaused"`
	NetworkID       id.ID      `db:"network_id" json:"networkId"`
	Height          int64      `db:"start_height" json:"height"`
	Hash            string     `db:"start_hash" json:"hash"`
	Timestamp       int64      `db:"start_timestamp" json:"timestamp"`
	Mode            string     `db:"mode" json:"mode"`
	DecoderVersion  int        `db:"decoder_version" json:"decoderVersion"`
	ActivatedAt     *time.Time `db:"activated_at" json:"activatedAt"`
	LastErrorCode   string     `db:"last_error_code" json:"lastErrorCode,omitempty"`
	LastErrorStream string     `db:"last_error_stream" json:"lastErrorStream,omitempty"`
	LastErrorHeight *int64     `db:"last_error_height" json:"lastErrorHeight,omitempty"`
	LastErrorAt     *time.Time `db:"last_error_at" json:"lastErrorAt,omitempty"`
}

// ErrShadowPaused is an operator-requested idle state, not an RPC failure.
var ErrShadowPaused = errors.New("shadow check paused by operator")

type CapturedBlock struct {
	DecoderVersion int
	Commit         WatcherBlockCommit
	ParentHash     string
	Events         []BlockchainEvent
	Sources        []VerificationSource
	Shadow         bool
	Sequential     bool
}

// Events are decoded before target matching. Persistence refreshes targets
// under the same network lock used by address registration, before capture.
type BlockCaptureRepository interface {
	Cutover(context.Context, id.ID) (IngestionCutover, error)
	EnsureCutover(context.Context, NetworkRef, WatcherBlockFingerprint, string, []AssetRef) (IngestionCutover, error)
	CaptureBlock(context.Context, CapturedBlock) error
	SaveVerification(context.Context, TransferVerification) (id.ID, error)
}

type StreamIngestionReader interface {
	ReadStreamHead(context.Context, string, []AssetRef) (IngestionHead, error)
	StreamRequired(string) bool
}
