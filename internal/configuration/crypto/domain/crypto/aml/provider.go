package aml

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

type SubjectKind string

const (
	AddressSubject  SubjectKind = "address"
	TransferSubject SubjectKind = "transfer"
)

// CheckRequest is already bound to verified chain evidence by ingestion.
// MatchingEffects describes coverage, not a guess from the vendor score.
type CheckRequest struct {
	MerchantID      id.ID       `json:"merchantId"`
	NetworkID       id.ID       `json:"networkId"`
	TokenID         id.ID       `json:"tokenId"`
	Kind            SubjectKind `json:"kind"`
	ContractAddress string      `json:"contractAddress"`
	Address         string      `json:"address"`
	TxHash          string      `json:"txHash"`
	EventKind       string      `json:"eventKind"`
	EventIndex      int64       `json:"eventIndex"`
	BlockHash       string      `json:"blockHash"`
	EconomicsHash   string      `json:"economicsHash"`
	MatchingEffects int         `json:"matchingEffects"` // zero means unproven transaction coverage
}

func (r CheckRequest) Validate() error {
	if id.IsNil(r.MerchantID) || id.IsNil(r.NetworkID) || id.IsNil(r.TokenID) || r.Address == "" || strings.ContainsAny(r.Address, "\x00\r\n\t ") {
		return apperror.NewValidation("merchant, network, token and checked address are required")
	}
	if r.Kind == AddressSubject {
		if r.TxHash != "" || r.BlockHash != "" || r.EconomicsHash != "" || r.EventKind != "" || r.EventIndex != 0 || r.MatchingEffects != 0 {
			return apperror.NewValidation("address AML subject must not contain transfer evidence")
		}
		return nil
	}
	if r.Kind != TransferSubject || r.TxHash == "" || r.BlockHash == "" || r.EconomicsHash == "" || r.EventIndex < 0 || r.MatchingEffects < 0 ||
		(r.EventKind != "native_value" && r.EventKind != "contract_log" && r.EventKind != "internal_value") {
		return apperror.NewValidation("complete verified transfer identity is required for transaction AML check")
	}
	return nil
}

// Digest uses a fixed ordered, length-prefixed v1 encoding. Token, merchant,
// effect and mapping changes cannot reuse another request's vendor report.
func (r CheckRequest) Digest(mappingVersion string) string {
	parts := []string{"aml-check.v1", r.MerchantID.String(), r.NetworkID.String(), r.TokenID.String(), string(r.Kind),
		r.ContractAddress, r.Address, r.TxHash, r.EventKind, strconv.FormatInt(r.EventIndex, 10), r.BlockHash, r.EconomicsHash,
		strconv.Itoa(r.MatchingEffects), mappingVersion}
	hash := sha256.New()
	for _, part := range parts {
		_, _ = hash.Write(binary.BigEndian.AppendUint64(nil, uint64(len(part))))
		_, _ = hash.Write([]byte(part))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

type Ticket struct {
	Provider          string       `json:"provider"`
	ProviderAccountID id.ID        `json:"providerAccountId"`
	CheckHash         string       `json:"checkHash"`
	SubmitRequestID   string       `json:"submitRequestId"`
	RequestDigest     string       `json:"requestDigest"`
	MappingVersion    string       `json:"mappingVersion"`
	Request           CheckRequest `json:"request"`
	SubmittedAt       time.Time    `json:"submittedAt"`
}

type CheckState string

const (
	CheckPending   CheckState = "pending"
	CheckSucceeded CheckState = "succeeded"
	CheckFailed    CheckState = "failed"
)

type CheckResult struct {
	State          CheckState
	ProviderStatus string
	Report         *Report
}

type Provider interface {
	Name() string
	AccountID() id.ID
	Submit(context.Context, CheckRequest, string) (Ticket, error)
	// requestID is persisted by orchestration before this logical read.
	GetResult(context.Context, Ticket, string) (CheckResult, error)
}

// ProviderFailure is implemented by adapters without exposing credentials or
// coupling the application orchestrator to a particular HTTP implementation.
type ProviderFailure interface {
	error
	OutcomeUnknown() bool
	Temporary() bool
}
