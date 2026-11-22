package crypto

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/types"
)

const NetworkFeeProofSchemaV1 = 1

var feeComponentName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
var feeTRONPayer = regexp.MustCompile(`^T[1-9A-HJ-NP-Za-km-z]{33}$`)

// NetworkFeeProof is a transaction fee role, independent of transfer identities,
// event ordinals and whether any principal was transferred successfully.
type NetworkFeeProof struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Family        ChainFamily          `json:"family"`
	NetworkID     id.ID                `json:"networkId"`
	TxHash        string               `json:"txHash"`
	PayerAddress  string               `json:"payerAddress"`
	NativeTokenID id.ID                `json:"nativeTokenId"`
	BlockHash     string               `json:"blockHash"`
	BlockNumber   int64                `json:"blockNumber"`
	Status        ReceiptStatus        `json:"status"`
	TotalFee      types.RawAmount      `json:"totalFee"`
	Components    []FeeComponent       `json:"components"`
	Confirmations int                  `json:"confirmations"`
	Finality      FinalityStatus       `json:"finality"`
	Canonicality  CanonicalityStatus   `json:"canonicality"`
	Sources       []VerificationSource `json:"sources"`
	VerifiedAt    time.Time            `json:"verifiedAt"`
}

func (p NetworkFeeProof) EffectKey() string {
	sum := sha256.Sum256([]byte(strings.Join([]string{"network-fee.v1", string(p.Family), p.NetworkID.String(), canonicalAccountingHash(p.TxHash)}, "\n")))
	return hex.EncodeToString(sum[:])
}
func (p NetworkFeeProof) PayerKey() string {
	if p.Family == ChainFamilyEVM {
		return strings.ToLower(p.PayerAddress)
	}
	return p.PayerAddress
}

// Digest covers normalized fee economics, excluding mutable observation time,
// RPC metadata and the provider attestations themselves.
func (p NetworkFeeProof) Digest() string {
	components := slices.Clone(p.Components)
	slices.SortFunc(components, func(a, b FeeComponent) int { return strings.Compare(a.Name, b.Name) })
	lines := []string{"network-fee-proof.v1", string(p.Family), p.NetworkID.String(), canonicalAccountingHash(p.TxHash), p.PayerKey(), p.NativeTokenID.String(), string(p.Status), strconv.FormatInt(p.BlockNumber, 10), canonicalAccountingHash(p.BlockHash), p.TotalFee.String()}
	for _, c := range components {
		lines = append(lines, c.Name+":"+c.Amount.String()+":"+strconv.FormatInt(c.Quantity, 10))
	}
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}
func (p NetworkFeeProof) Validate() error {
	invalid := func() error { return apperror.NewValidation("invalid independent network fee proof") }
	if p.SchemaVersion != NetworkFeeProofSchemaV1 || (p.Family != ChainFamilyEVM && p.Family != ChainFamilyTRON) || id.IsNil(p.NetworkID) || id.IsNil(p.NativeTokenID) ||
		!validAccountingHash(canonicalAccountingHash(p.TxHash)) || !validAccountingHash(canonicalAccountingHash(p.BlockHash)) || p.BlockNumber < 0 || p.Confirmations < 1 || p.VerifiedAt.IsZero() ||
		(p.Status != ReceiptSucceeded && p.Status != ReceiptReverted) || p.Canonicality != CanonicalityCanonical || (p.Finality != FinalitySolidified && p.Finality != FinalityFinalized) || len(p.Sources) != 2 || len(p.Components) == 0 || len(p.Components) > 8 {
		return invalid()
	}
	if p.PayerKey() == "" || strings.TrimSpace(p.PayerAddress) != p.PayerAddress {
		return invalid()
	}
	if p.Family == ChainFamilyEVM {
		if len(p.PayerAddress) != 42 || !strings.HasPrefix(strings.ToLower(p.PayerAddress), "0x") {
			return invalid()
		}
		if _, err := hex.DecodeString(p.PayerAddress[2:]); err != nil {
			return invalid()
		}
	} else if !feeTRONPayer.MatchString(p.PayerAddress) {
		return invalid()
	}
	total := types.ZeroRawAmount()
	seen := map[string]bool{}
	for _, c := range p.Components {
		if !feeComponentName.MatchString(c.Name) || seen[c.Name] || c.Quantity < 0 {
			return invalid()
		}
		seen[c.Name] = true
		if p.Family == ChainFamilyEVM && c.Name != "execution_gas" && c.Name != "blob_gas" {
			return invalid()
		}
		if p.Family == ChainFamilyEVM && c.Quantity == 0 {
			return invalid()
		}
		if p.Family == ChainFamilyTRON && c.Name != "energy_fee" && c.Name != "bandwidth_fee" && c.Name != "other_chain_fee" {
			return invalid()
		}
		total = total.Add(c.Amount)
	}
	if !total.Equal(p.TotalFee) {
		return invalid()
	}
	if (p.Family == ChainFamilyEVM && !seen["execution_gas"]) || (p.Family == ChainFamilyTRON && (!seen["energy_fee"] || !seen["bandwidth_fee"])) {
		return invalid()
	}
	digest := p.Digest()
	first, second := p.Sources[0], p.Sources[1]
	if first.ProviderID == second.ProviderID || first.TrustGroup == second.TrustGroup || canonicalAccountingHash(first.HeadHash) != canonicalAccountingHash(second.HeadHash) || first.HeadHeight != second.HeadHeight {
		return invalid()
	}
	for _, s := range p.Sources {
		if strings.TrimSpace(s.ProviderID) == "" || strings.TrimSpace(s.TrustGroup) == "" || s.Digest != digest || !validAccountingHash(s.SourceDigest) || !validAccountingHash(canonicalAccountingHash(s.HeadHash)) || s.HeadHeight < p.BlockNumber || s.HeadHeight-p.BlockNumber+1 != int64(p.Confirmations) {
			return invalid()
		}
	}
	return nil
}
func (p NetworkFeeProof) ValidateReceipt(r ChainReceipt) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if !r.FinanciallyFinal() || p.NetworkID != r.NetworkID || canonicalAccountingHash(p.TxHash) != canonicalAccountingHash(r.TxHash) || p.NativeTokenID != r.Fee.NativeAsset.TokenID ||
		canonicalAccountingHash(p.BlockHash) != canonicalAccountingHash(r.BlockHash) || p.BlockNumber != r.BlockHeight || p.Status != r.Status || p.Confirmations != r.Confirmations || p.Finality != r.Finality || p.Canonicality != r.Canonicality || !p.TotalFee.Equal(r.Fee.TotalFee) {
		return ErrVerificationMismatch
	}
	other := p
	other.Components = r.Fee.Components
	if other.Digest() != p.Digest() {
		return ErrVerificationMismatch
	}
	return nil
}
