package crypto

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/json"
)

// AccountingEffectSchemaV1 is independent of frozen transfer/signing schemas.
const AccountingEffectSchemaV1 = 1

// AccountingEffectIdentity identifies a logical transfer across block
// occurrences. Ordinal is transaction-local, never an EVM block log index.
// TRON internal transfers use the full trace hash and call-value ordinal,
// rather than the truncated synthetic index carried by legacy observations.
type AccountingEffectIdentity struct {
	SchemaVersion int         `json:"schemaVersion"`
	Family        ChainFamily `json:"family"`
	NetworkID     id.ID       `json:"networkId"`
	TxHash        string      `json:"txHash"`
	EventKind     EventKind   `json:"eventKind"`
	Ordinal       int64       `json:"ordinal"`
	TraceHash     string      `json:"traceHash,omitempty"`
}

// NewAccountingEffectIdentity is called by a family verifier after locating
// the transfer in a complete, independently verified receipt/block. Callers
// must not supply an ordinal inferred from amount, address or log.Index.
func NewAccountingEffectIdentity(event BlockchainEvent, family ChainFamily, ordinal int64) (AccountingEffectIdentity, error) {
	result := AccountingEffectIdentity{
		SchemaVersion: AccountingEffectSchemaV1, Family: family, NetworkID: event.NetworkID,
		TxHash: canonicalAccountingHash(event.TxHash), EventKind: event.EventKind, Ordinal: ordinal,
	}
	if event.EventKind == EventKindInternalValue {
		result.TraceHash = canonicalAccountingHash(event.TraceHash)
	}
	return result, result.ValidateEvent(event)
}

func (a AccountingEffectIdentity) Validate() error {
	if a.SchemaVersion != AccountingEffectSchemaV1 || id.IsNil(a.NetworkID) ||
		(a.Family != ChainFamilyTRON && a.Family != ChainFamilyEVM) ||
		!a.EventKind.Valid() || a.Ordinal < 0 || !validAccountingHash(a.TxHash) {
		return apperror.NewValidation("invalid accounting effect identity")
	}
	if a.EventKind == EventKindInternalValue {
		if a.Family != ChainFamilyTRON || !validAccountingHash(a.TraceHash) {
			return apperror.NewValidation("invalid internal accounting effect identity")
		}
	} else if a.TraceHash != "" {
		return apperror.NewValidation("unexpected accounting trace identity")
	}
	if a.Family == ChainFamilyEVM && a.EventKind == EventKindNativeValue && a.Ordinal != 0 {
		return apperror.NewValidation("invalid native accounting ordinal")
	}
	return nil
}

func (a AccountingEffectIdentity) ValidateEvent(event BlockchainEvent) error {
	if err := a.Validate(); err != nil {
		return err
	}
	if a.NetworkID != event.NetworkID || a.TxHash != canonicalAccountingHash(event.TxHash) ||
		a.EventKind != event.EventKind || event.EventIndex < 0 {
		return apperror.NewConflict("accounting effect does not match transfer evidence")
	}
	switch a.Family {
	case ChainFamilyTRON:
		if event.EffectiveEvidenceSchema() != TRONTransferEvidenceSchema {
			return apperror.NewConflict("accounting effect requires current TRON evidence")
		}
		if a.EventKind == EventKindInternalValue {
			if event.CallValueIndex == nil || a.Ordinal != *event.CallValueIndex ||
				a.TraceHash != canonicalAccountingHash(event.TraceHash) {
				return apperror.NewConflict("accounting effect does not match trace evidence")
			}
		} else if a.Ordinal != event.EventIndex {
			return apperror.NewConflict("accounting effect does not match TRON ordinal")
		}
	case ChainFamilyEVM:
		if event.EffectiveEvidenceSchema() != TransferEvidenceSchemaV1 ||
			(a.EventKind == EventKindNativeValue && event.EventIndex != 0) ||
			(a.EventKind == EventKindContractLog && event.EventIndex < a.Ordinal) {
			return apperror.NewConflict("accounting effect does not match EVM evidence")
		}
	}
	return nil
}

// Key is a versioned, content-addressed identity. It excludes block location,
// confirmation counts, amounts and the observer's event index.
func (a AccountingEffectIdentity) Key() (string, error) {
	if err := a.Validate(); err != nil {
		return "", err
	}
	raw, err := json.MarshalDeterministic(a)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return "accounting-effect.v1:" + hex.EncodeToString(digest[:]), nil
}

// AccountingEffectOccurrence binds a logical effect to one inclusion and its
// economics. Different economics in one block must be treated as a conflict;
// re-inclusion may legitimately change economics after verified orphaning.
type AccountingEffectOccurrence struct {
	Identity      AccountingEffectIdentity `json:"identity"`
	BlockNumber   int64                    `json:"blockNumber"`
	BlockHash     string                   `json:"blockHash"`
	EconomicsHash string                   `json:"economicsHash"`
}

func (a AccountingEffectIdentity) Occurrence(event BlockchainEvent) (AccountingEffectOccurrence, error) {
	if err := a.ValidateEvent(event); err != nil {
		return AccountingEffectOccurrence{}, err
	}
	blockHash := canonicalAccountingHash(event.BlockHash)
	if event.BlockNumber < 0 || !validAccountingHash(blockHash) || !event.Amount.IsPositive() ||
		event.FromAddress == "" || event.ToAddress == "" ||
		(event.EventKind == EventKindContractLog && event.TokenContract == "") ||
		(event.EventKind != EventKindContractLog && event.TokenContract != "") {
		return AccountingEffectOccurrence{}, apperror.NewValidation("invalid accounting occurrence evidence")
	}
	from, to, contract := event.FromAddress, event.ToAddress, event.TokenContract
	if a.Family == ChainFamilyEVM {
		from, to, contract = strings.ToLower(from), strings.ToLower(to), strings.ToLower(contract)
	}
	raw, err := json.MarshalDeterministic(struct {
		SchemaVersion int                      `json:"schemaVersion"`
		Identity      AccountingEffectIdentity `json:"identity"`
		From          string                   `json:"from"`
		To            string                   `json:"to"`
		Contract      string                   `json:"contract"`
		Amount        string                   `json:"amount"`
	}{AccountingEffectSchemaV1, a, from, to, contract, event.Amount.String()})
	if err != nil {
		return AccountingEffectOccurrence{}, err
	}
	digest := sha256.Sum256(raw)
	return AccountingEffectOccurrence{
		Identity: a, BlockNumber: event.BlockNumber, BlockHash: blockHash,
		EconomicsHash: hex.EncodeToString(digest[:]),
	}, nil
}

// BindTRONAccountingIdentity only admits positive, final canonical evidence.
// Absence/reorg proofs retain their original bytes and use the saved identity
// of the occurrence they invalidate.
func (v *TransferVerification) BindTRONAccountingIdentity() error {
	if err := v.Validate(); err != nil {
		return err
	}
	if v.DecoderVersion != TRONTransferEvidenceSchema || v.Outcome != "verified" ||
		v.ExecutionStatus != ReceiptSucceeded || v.Event.Canonicality != CanonicalityCanonical {
		return apperror.NewValidation("accounting admission requires successful canonical evidence")
	}
	ordinal := v.Event.EventIndex
	if v.Event.EventKind == EventKindInternalValue {
		if v.Event.CallValueIndex == nil {
			return apperror.NewValidation("accounting admission requires call-value identity")
		}
		ordinal = *v.Event.CallValueIndex
	}
	identity, err := NewAccountingEffectIdentity(v.Event, ChainFamilyTRON, ordinal)
	if err != nil {
		return err
	}
	v.AccountingIdentity = &identity
	return nil
}

func canonicalAccountingHash(value string) string {
	return strings.TrimPrefix(strings.ToLower(value), "0x")
}

func validAccountingHash(value string) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == 32 && value == strings.ToLower(value)
}
