package tron

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"
	"vault-plugin-secrets-tron/internal/json"

	"github.com/fbsobreira/gotron-sdk/pkg/address"
	trx "github.com/fbsobreira/gotron-sdk/pkg/client/transaction"
	"github.com/fbsobreira/gotron-sdk/pkg/proto/core"
	"google.golang.org/protobuf/proto"
)

const (
	AssetStandardNative = "native"
	AssetStandardTRC20  = "TRC-20"
	TransferSelector    = "a9059cbb"
)

type UnsignedTransaction struct {
	TxID       string          `json:"txID"`
	RawDataHex string          `json:"raw_data_hex"`
	RawData    json.RawMessage `json:"raw_data,omitempty"`
	Visible    bool            `json:"visible,omitzero"`
}

type SignedTransaction struct {
	TxID       string          `json:"txID"`
	RawDataHex string          `json:"raw_data_hex"`
	RawData    json.RawMessage `json:"raw_data,omitempty"`
	Signature  []string        `json:"signature"`
	Visible    bool            `json:"visible,omitzero"`
}

type UnsignedEnvelope struct {
	Family              string              `json:"family"`
	SchemaVersion       int                 `json:"schemaVersion"`
	UnsignedFingerprint string              `json:"unsignedFingerprint"`
	Generation          int                 `json:"generation"`
	NetworkSequence     string              `json:"networkSequence,omitempty"`
	Payload             UnsignedTransaction `json:"payload"`
}

type SignedEnvelope struct {
	Family              string            `json:"family"`
	SchemaVersion       int               `json:"schemaVersion"`
	UnsignedFingerprint string            `json:"unsignedFingerprint"`
	Generation          int               `json:"generation"`
	NetworkSequence     string            `json:"networkSequence,omitempty"`
	TxHash              string            `json:"txHash"`
	Payload             SignedTransaction `json:"payload"`
}

func ValidateUnsignedEnvelope(envelope UnsignedEnvelope) error {
	if envelope.Family != "tron" || envelope.SchemaVersion != 1 || envelope.UnsignedFingerprint == "" || envelope.Generation != 1 || envelope.NetworkSequence != "" {
		return fmt.Errorf("unsupported TRON unsigned envelope")
	}
	fingerprint, err := UnsignedFingerprint(envelope.Payload)
	if err != nil {
		return err
	}
	if fingerprint != envelope.UnsignedFingerprint {
		return fmt.Errorf("unsigned envelope fingerprint mismatch")
	}
	return nil
}

func NewSignedEnvelope(auth SigningAuthorization, signed SignedTransaction) SignedEnvelope {
	return SignedEnvelope{
		Family: "tron", SchemaVersion: 1, UnsignedFingerprint: auth.UnsignedFingerprint,
		Generation: auth.Generation, NetworkSequence: auth.NetworkSequence,
		TxHash: signed.TxID, Payload: signed,
	}
}

func SignAuthorizedTransaction(
	privateKeyHex string,
	publicKey ed25519.PublicKey,
	authJWS string,
	approvalBundle ApprovalBundle,
	approvalKeys ApprovalPublicKeys,
	unsigned UnsignedTransaction,
	now time.Time,
	walletVaultKeyID string,
	expectedTenantID string,
) (SignedTransaction, SigningAuthorization, error) {
	auth, err := VerifyAuthorizationJWS(authJWS, publicKey)
	if err != nil {
		return SignedTransaction{}, auth, err
	}
	if err := validateAuthorization(auth, now, walletVaultKeyID, expectedTenantID); err != nil {
		return SignedTransaction{}, auth, err
	}
	if err := ValidateApprovalBundle(approvalBundle, approvalKeys, auth, now); err != nil {
		return SignedTransaction{}, auth, err
	}

	tx, txID, err := parseAndValidateTxID(unsigned)
	if err != nil {
		return SignedTransaction{}, auth, err
	}
	if err := validateTransactionIntent(tx, auth, now); err != nil {
		return SignedTransaction{}, auth, err
	}
	fingerprint, err := UnsignedFingerprint(unsigned)
	if err != nil {
		return SignedTransaction{}, auth, err
	}
	if fingerprint != auth.UnsignedFingerprint {
		return SignedTransaction{}, auth, fmt.Errorf("unsigned fingerprint does not match authorization")
	}

	signature, err := SignTRONHash(privateKeyHex, txID)
	if err != nil {
		return SignedTransaction{}, auth, err
	}

	return SignedTransaction{
		TxID:       txID,
		RawDataHex: strings.TrimPrefix(strings.ToLower(unsigned.RawDataHex), "0x"),
		RawData:    unsigned.RawData,
		Signature:  []string{signature},
		Visible:    unsigned.Visible,
	}, auth, nil
}

func validateAuthorization(auth SigningAuthorization, now time.Time, walletVaultKeyID, expectedTenantID string) error {
	if auth.SchemaVersion != SigningAuthorizationSchemaVersion {
		return fmt.Errorf("unsupported schema_version: %d", auth.SchemaVersion)
	}
	if auth.AuthorizationID == "" {
		return fmt.Errorf("missing authorization_id")
	}
	if auth.TenantID == "" {
		return fmt.Errorf("missing tenant_id")
	}
	if expectedTenantID != "" && auth.TenantID != expectedTenantID {
		return fmt.Errorf("tenant_id does not match path")
	}
	if auth.PolicyVersion != SigningAuthorizationPolicyVersion {
		return fmt.Errorf("unsupported policy_version: %s", auth.PolicyVersion)
	}
	if auth.NetworkID == "" || auth.ChainFamily != "tron" {
		return fmt.Errorf("invalid TRON network identity")
	}
	if PluginNetwork(auth) == "" {
		return fmt.Errorf("unsupported network identity: %s/%s", auth.NetworkCode, auth.NetworkIdentifier)
	}
	if auth.Purpose != "withdrawal" && auth.Purpose != "sweep" && auth.Purpose != "treasury_withdrawal" && auth.Purpose != "activation" && auth.Purpose != "native_funding" {
		return fmt.Errorf("unsupported purpose: %s", auth.Purpose)
	}
	if auth.Purpose == "withdrawal" && auth.MerchantID == nil {
		return fmt.Errorf("withdrawal authorization requires merchant_id")
	}
	if auth.SourceAddress == "" || !ValidateAddress(auth.SourceAddress) {
		return fmt.Errorf("invalid source_address")
	}
	if auth.FeePayerAddress != auth.SourceAddress {
		return fmt.Errorf("fee_payer_address must match TRON transaction owner")
	}
	if auth.DestinationAddress == "" || !ValidateAddress(auth.DestinationAddress) {
		return fmt.Errorf("invalid destination_address")
	}
	if auth.VaultKeyID == "" || auth.VaultKeyID != walletVaultKeyID {
		return fmt.Errorf("vault_key_id does not match wallet")
	}
	if auth.Purpose == "activation" && !auth.AmountMinorUnits.IsZero() {
		return fmt.Errorf("activation authorization amount must be zero")
	}
	if auth.Purpose != "activation" && !auth.AmountMinorUnits.IsPositive() {
		return fmt.Errorf("amount_minor_units must be positive")
	}
	if auth.Purpose == "native_funding" && auth.AssetStandard != AssetStandardNative {
		return fmt.Errorf("native_funding requires native asset")
	}
	if !auth.ExpiresAt.After(now) {
		return fmt.Errorf("authorization expired")
	}
	if auth.Attempt <= 0 {
		return fmt.Errorf("attempt must be positive")
	}
	if auth.Generation != 1 || auth.NetworkSequence != "" {
		return fmt.Errorf("TRON authorization requires generation 1 without network sequence")
	}
	if auth.UnsignedFingerprint == "" {
		return fmt.Errorf("unsigned_fingerprint is required")
	}
	return nil
}

func PluginNetwork(auth SigningAuthorization) string {
	code := strings.ToUpper(strings.TrimSpace(auth.NetworkCode))
	switch strings.ToLower(strings.TrimSpace(auth.NetworkIdentifier)) {
	case "nile":
		if code == "TRON-NILE" {
			return "tron_nile"
		}
	case "mainnet":
		if code == "TRON-MAINNET" {
			return "tron_mainnet"
		}
	}
	return ""
}

func UnsignedFingerprint(unsigned UnsignedTransaction) (string, error) {
	payload, err := json.Marshal(unsigned)
	if err != nil {
		return "", fmt.Errorf("marshal unsigned envelope: %w", err)
	}
	var canonical any
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&canonical); err != nil {
		return "", fmt.Errorf("canonicalize unsigned envelope: %w", err)
	}
	canonicalPayload, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("canonicalize unsigned envelope: %w", err)
	}
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "tron\n1\n")
	_, _ = h.Write(canonicalPayload)
	return hex.EncodeToString(h.Sum(nil)), nil
}

func parseAndValidateTxID(unsigned UnsignedTransaction) (*core.Transaction, string, error) {
	if unsigned.TxID == "" {
		return nil, "", fmt.Errorf("missing txID")
	}
	if unsigned.RawDataHex == "" {
		return nil, "", fmt.Errorf("missing raw_data_hex")
	}
	if !unsigned.Visible {
		return nil, "", fmt.Errorf("visible=true is required")
	}
	rawData := bytes.TrimSpace(unsigned.RawData)
	if len(rawData) == 0 || bytes.Equal(rawData, []byte("null")) {
		return nil, "", fmt.Errorf("missing raw_data")
	}
	if rawData[0] != '{' {
		return nil, "", fmt.Errorf("raw_data must be a JSON object")
	}

	tx, err := trx.FromRawDataHex(unsigned.RawDataHex)
	if err != nil {
		return nil, "", err
	}
	rawBytes, err := proto.Marshal(tx.GetRawData())
	if err != nil {
		return nil, "", fmt.Errorf("marshal raw_data: %w", err)
	}
	hash := sha256.Sum256(rawBytes)
	txID := hex.EncodeToString(hash[:])
	if !strings.EqualFold(txID, unsigned.TxID) {
		return nil, "", fmt.Errorf("txID does not match raw_data_hex")
	}
	return tx, txID, nil
}

func validateTransactionIntent(tx *core.Transaction, auth SigningAuthorization, now time.Time) error {
	raw := tx.GetRawData()
	if raw == nil {
		return fmt.Errorf("missing raw_data")
	}
	if raw.GetExpiration() <= 0 {
		return fmt.Errorf("missing transaction expiration")
	}
	txExpiresAt := time.UnixMilli(raw.GetExpiration())
	if !txExpiresAt.After(now) {
		return fmt.Errorf("transaction expired")
	}
	if txExpiresAt.After(auth.ExpiresAt) {
		return fmt.Errorf("transaction expiration exceeds authorization")
	}

	contracts := raw.GetContract()
	if len(contracts) != 1 {
		return fmt.Errorf("expected exactly one contract")
	}
	contract := contracts[0]
	if contract.GetParameter() == nil {
		return fmt.Errorf("missing contract parameter")
	}

	if auth.Purpose == "activation" {
		if contract.GetType() != core.Transaction_Contract_AccountCreateContract {
			return fmt.Errorf("activation must use AccountCreateContract")
		}
		return validateAccountCreate(contract, auth)
	}

	switch auth.AssetStandard {
	case AssetStandardNative:
		if contract.GetType() != core.Transaction_Contract_TransferContract {
			return fmt.Errorf("native transfer must use TransferContract")
		}
		return validateNativeTransfer(contract, auth)
	case AssetStandardTRC20:
		if contract.GetType() != core.Transaction_Contract_TriggerSmartContract {
			return fmt.Errorf("TRC-20 transfer must use TriggerSmartContract")
		}
		maxNetworkFee, err := auth.MaxNetworkFeeMinor.Int64Exact()
		if err != nil || maxNetworkFee <= 0 || raw.GetFeeLimit() <= 0 || raw.GetFeeLimit() > maxNetworkFee {
			return fmt.Errorf("fee_limit exceeds authorization")
		}
		return validateTRC20Transfer(contract, auth)
	default:
		return fmt.Errorf("unsupported asset_standard: %s", auth.AssetStandard)
	}
}

func validateAccountCreate(contract *core.Transaction_Contract, auth SigningAuthorization) error {
	var c core.AccountCreateContract
	if err := proto.Unmarshal(contract.GetParameter().GetValue(), &c); err != nil {
		return fmt.Errorf("unmarshal AccountCreateContract: %w", err)
	}
	if address.Address(c.GetOwnerAddress()).String() != auth.SourceAddress {
		return fmt.Errorf("owner_address does not match authorization")
	}
	if address.Address(c.GetAccountAddress()).String() != auth.DestinationAddress {
		return fmt.Errorf("account_address does not match authorization")
	}
	return nil
}

func validateNativeTransfer(contract *core.Transaction_Contract, auth SigningAuthorization) error {
	var c core.TransferContract
	if err := proto.Unmarshal(contract.GetParameter().GetValue(), &c); err != nil {
		return fmt.Errorf("unmarshal TransferContract: %w", err)
	}
	if address.Address(c.GetOwnerAddress()).String() != auth.SourceAddress {
		return fmt.Errorf("owner_address does not match authorization")
	}
	if address.Address(c.GetToAddress()).String() != auth.DestinationAddress {
		return fmt.Errorf("to_address does not match authorization")
	}
	authorizedAmount, err := auth.AmountMinorUnits.Int64Exact()
	if err != nil || c.GetAmount() != authorizedAmount {
		return fmt.Errorf("amount does not match authorization")
	}
	return nil
}

func validateTRC20Transfer(contract *core.Transaction_Contract, auth SigningAuthorization) error {
	var c core.TriggerSmartContract
	if err := proto.Unmarshal(contract.GetParameter().GetValue(), &c); err != nil {
		return fmt.Errorf("unmarshal TriggerSmartContract: %w", err)
	}
	if address.Address(c.GetOwnerAddress()).String() != auth.SourceAddress {
		return fmt.Errorf("owner_address does not match authorization")
	}
	if address.Address(c.GetContractAddress()).String() != auth.ContractAddress {
		return fmt.Errorf("contract_address does not match authorization")
	}
	if c.GetCallValue() != 0 || c.GetCallTokenValue() != 0 || c.GetTokenId() != 0 {
		return fmt.Errorf("TRC-20 transfer must not include call value")
	}

	data := c.GetData()
	if len(data) != 68 {
		return fmt.Errorf("invalid TRC-20 transfer data length")
	}
	if hex.EncodeToString(data[:4]) != TransferSelector {
		return fmt.Errorf("unsupported TRC-20 function selector")
	}

	toAddr, err := address.Base58ToAddress(auth.DestinationAddress)
	if err != nil {
		return fmt.Errorf("decode destination_address: %w", err)
	}
	addressWord := data[4:36]
	if !bytes.Equal(addressWord[12:], toAddr.Bytes()[1:]) && !bytes.Equal(addressWord[11:], toAddr.Bytes()) {
		return fmt.Errorf("TRC-20 destination does not match authorization")
	}

	amount := new(big.Int).SetBytes(data[36:68])
	authorizedAmount, err := auth.AmountMinorUnits.BigInt()
	if err != nil || amount.Cmp(authorizedAmount) != 0 {
		return fmt.Errorf("TRC-20 amount does not match authorization")
	}
	return nil
}
