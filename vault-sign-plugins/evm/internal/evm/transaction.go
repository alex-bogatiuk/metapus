package evm

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"
	"vault-plugin-secrets-evm/internal/json"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
)

const (
	EnvelopeSchemaVersion = 1
	AssetStandardNative   = "native"
	AssetStandardERC20    = "ERC-20"
	SepoliaChainID        = "11155111"
	SepoliaPYUSDContract  = "0xCaC524BcA292aaade2DF8A05cC58F0a65B1B3bB9"
)

var transferABI = mustABI(`[{"type":"function","name":"transfer","stateMutability":"nonpayable","inputs":[{"name":"to","type":"address"},{"name":"value","type":"uint256"}],"outputs":[{"name":"","type":"bool"}]}]`)

func mustABI(raw string) abi.ABI {
	parsed, err := abi.JSON(strings.NewReader(raw))
	if err != nil {
		panic(err)
	}
	return parsed
}

type UnsignedTransaction struct {
	ChainID                 string               `json:"chainId"`
	Nonce                   string               `json:"nonce"`
	MaxPriorityFeePerGasWei string               `json:"maxPriorityFeePerGasWei"`
	MaxFeePerGasWei         string               `json:"maxFeePerGasWei"`
	GasLimit                uint64               `json:"gasLimit"`
	To                      string               `json:"to"`
	ValueWei                string               `json:"valueWei"`
	Data                    string               `json:"data"`
	AccessList              gethtypes.AccessList `json:"accessList"`
}

type UnsignedEnvelope struct {
	Family              string              `json:"family"`
	SchemaVersion       int                 `json:"schemaVersion"`
	UnsignedFingerprint string              `json:"unsignedFingerprint"`
	Generation          int                 `json:"generation"`
	NetworkSequence     string              `json:"networkSequence"`
	Payload             UnsignedTransaction `json:"payload"`
}

type signedPayload struct {
	RawTransaction string `json:"rawTransaction"`
}

type SignedEnvelope struct {
	Family              string        `json:"family"`
	SchemaVersion       int           `json:"schemaVersion"`
	UnsignedFingerprint string        `json:"unsignedFingerprint"`
	Generation          int           `json:"generation"`
	NetworkSequence     string        `json:"networkSequence"`
	TxHash              string        `json:"txHash"`
	Payload             signedPayload `json:"payload"`
}

func ValidateUnsignedEnvelope(envelope UnsignedEnvelope) error {
	if envelope.Family != "evm" || envelope.SchemaVersion != EnvelopeSchemaVersion || envelope.Generation <= 0 || envelope.Generation > 3 || envelope.NetworkSequence == "" {
		return fmt.Errorf("unsupported EVM unsigned envelope")
	}
	if envelope.NetworkSequence != envelope.Payload.Nonce {
		return fmt.Errorf("network sequence does not match nonce")
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

func UnsignedFingerprint(unsigned UnsignedTransaction) (string, error) {
	payload, err := json.Marshal(unsigned)
	if err != nil {
		return "", err
	}
	var canonical any
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&canonical); err != nil {
		return "", err
	}
	canonicalPayload, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, _ = fmt.Fprintf(hash, "evm\n%d\n", EnvelopeSchemaVersion)
	_, _ = hash.Write(canonicalPayload)
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func SignAuthorizedTransaction(privateKeyHex string, authorizationKey ed25519.PublicKey, authorizationJWS string, approval ApprovalBundle, approvalKeys ApprovalPublicKeys, envelope UnsignedEnvelope, now time.Time, walletVaultKeyID, expectedTenantID string, allowedChainIDs map[string]bool) (SignedEnvelope, SigningAuthorization, error) {
	var empty SignedEnvelope
	auth, err := VerifyAuthorizationJWS(authorizationJWS, authorizationKey)
	if err != nil {
		return empty, auth, err
	}
	if err := validateAuthorization(auth, envelope, now, walletVaultKeyID, expectedTenantID, allowedChainIDs); err != nil {
		return empty, auth, err
	}
	if err := ValidateApprovalBundle(approval, approvalKeys, auth, now); err != nil {
		return empty, auth, err
	}
	if err := ValidateUnsignedEnvelope(envelope); err != nil {
		return empty, auth, err
	}
	if auth.UnsignedFingerprint != envelope.UnsignedFingerprint {
		return empty, auth, fmt.Errorf("unsigned fingerprint does not match authorization")
	}
	tx, err := reconstructAndValidate(envelope.Payload, auth)
	if err != nil {
		return empty, auth, err
	}
	privateKey, err := gethcrypto.HexToECDSA(strings.TrimPrefix(privateKeyHex, "0x"))
	if err != nil {
		return empty, auth, fmt.Errorf("decode EVM private key: %w", err)
	}
	if gethcrypto.PubkeyToAddress(privateKey.PublicKey).Hex() != auth.SourceAddress {
		return empty, auth, fmt.Errorf("private key does not own authorized source address")
	}
	chainID, _ := new(big.Int).SetString(auth.NetworkIdentifier, 10)
	signed, err := gethtypes.SignTx(tx, gethtypes.LatestSignerForChainID(chainID), privateKey)
	if err != nil {
		return empty, auth, fmt.Errorf("sign EVM transaction: %w", err)
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		return empty, auth, fmt.Errorf("encode signed EVM transaction: %w", err)
	}
	return SignedEnvelope{Family: "evm", SchemaVersion: EnvelopeSchemaVersion,
		UnsignedFingerprint: auth.UnsignedFingerprint, Generation: auth.Generation, NetworkSequence: auth.NetworkSequence,
		TxHash: signed.Hash().Hex(), Payload: signedPayload{RawTransaction: "0x" + hex.EncodeToString(raw)}}, auth, nil
}

func validateAuthorization(auth SigningAuthorization, envelope UnsignedEnvelope, now time.Time, walletVaultKeyID, expectedTenantID string, allowedChainIDs map[string]bool) error {
	if auth.SchemaVersion != SigningAuthorizationSchemaVersion || auth.PolicyVersion != SigningAuthorizationPolicyVersion {
		return fmt.Errorf("unsupported signing authorization schema or policy")
	}
	if auth.AuthorizationID == "" || auth.TenantID != expectedTenantID || auth.NetworkID == "" || auth.ChainFamily != "evm" || auth.NetworkCode != "ETHEREUM-SEPOLIA" || auth.NetworkIdentifier != SepoliaChainID || !allowedChainIDs[auth.NetworkIdentifier] {
		return fmt.Errorf("invalid EVM authorization scope")
	}
	if auth.Purpose != "withdrawal" && auth.Purpose != "sweep" && auth.Purpose != "treasury_withdrawal" && auth.Purpose != "native_funding" {
		return fmt.Errorf("unsupported purpose: %s", auth.Purpose)
	}
	if auth.Purpose == "withdrawal" && auth.MerchantID == nil {
		return fmt.Errorf("withdrawal authorization requires merchant_id")
	}
	if auth.Generation <= 0 || auth.Generation > 3 || auth.Generation != envelope.Generation || auth.NetworkSequence != envelope.NetworkSequence {
		return fmt.Errorf("authorization generation or network sequence mismatch")
	}
	sequence, err := strconv.ParseUint(auth.NetworkSequence, 10, 64)
	if err != nil || strconv.FormatUint(sequence, 10) != auth.NetworkSequence {
		return fmt.Errorf("network_sequence must be a canonical uint64")
	}
	source, err := NormalizeAddress(auth.SourceAddress)
	if err != nil || source != auth.SourceAddress || auth.FeePayerAddress != source {
		return fmt.Errorf("invalid source or fee payer address")
	}
	destination, err := NormalizeAddress(auth.DestinationAddress)
	if err != nil || destination != auth.DestinationAddress {
		return fmt.Errorf("invalid destination address")
	}
	if auth.VaultKeyID == "" || auth.VaultKeyID != walletVaultKeyID || auth.Attempt <= 0 || auth.BasisType == "" || auth.BasisID == "" {
		return fmt.Errorf("invalid key ownership or business identity")
	}
	if !auth.AmountMinorUnits.IsPositive() || !auth.MaxNetworkFeeMinor.IsPositive() || auth.UnsignedFingerprint == "" || !auth.ExpiresAt.After(now) {
		return fmt.Errorf("invalid amount, fee cap, fingerprint, or expiry")
	}
	if auth.Purpose == "native_funding" && auth.AssetStandard != AssetStandardNative {
		return fmt.Errorf("native funding requires native asset")
	}
	return nil
}

func reconstructAndValidate(unsigned UnsignedTransaction, auth SigningAuthorization) (*gethtypes.Transaction, error) {
	chainID, chainOK := new(big.Int).SetString(unsigned.ChainID, 10)
	nonce, nonceErr := strconv.ParseUint(unsigned.Nonce, 10, 64)
	tip, tipOK := new(big.Int).SetString(unsigned.MaxPriorityFeePerGasWei, 10)
	feeCap, feeOK := new(big.Int).SetString(unsigned.MaxFeePerGasWei, 10)
	value, valueOK := new(big.Int).SetString(unsigned.ValueWei, 10)
	to, toErr := NormalizeAddress(unsigned.To)
	data, dataErr := hex.DecodeString(strings.TrimPrefix(unsigned.Data, "0x"))
	if !chainOK || chainID.String() != auth.NetworkIdentifier || nonceErr != nil || unsigned.Nonce != auth.NetworkSequence || !tipOK || !feeOK || !valueOK || toErr != nil || dataErr != nil || unsigned.GasLimit == 0 || tip.Sign() <= 0 || feeCap.Sign() <= 0 || tip.Cmp(feeCap) > 0 || value.Sign() < 0 || len(unsigned.AccessList) != 0 {
		return nil, fmt.Errorf("invalid EIP-1559 envelope")
	}
	maxFee := new(big.Int).Mul(new(big.Int).SetUint64(unsigned.GasLimit), feeCap)
	authorizedMax, err := auth.MaxNetworkFeeMinor.BigInt()
	if err != nil || maxFee.Cmp(authorizedMax) > 0 {
		return nil, fmt.Errorf("transaction fee cap exceeds authorization")
	}
	switch auth.AssetStandard {
	case AssetStandardNative:
		amount, _ := auth.AmountMinorUnits.BigInt()
		if auth.ContractAddress != "" || to != auth.DestinationAddress || value.Cmp(amount) != 0 || len(data) != 0 {
			return nil, fmt.Errorf("native transaction semantics do not match authorization")
		}
	case AssetStandardERC20:
		contract, err := NormalizeAddress(auth.ContractAddress)
		if err != nil || contract != auth.ContractAddress || to != contract || value.Sign() != 0 || len(data) != 68 || !bytes.Equal(data[:4], transferABI.Methods["transfer"].ID) {
			return nil, fmt.Errorf("ERC-20 transaction contract or selector denied")
		}
		values, err := transferABI.Methods["transfer"].Inputs.Unpack(data[4:])
		if err != nil || len(values) != 2 {
			return nil, fmt.Errorf("decode ERC-20 transfer calldata")
		}
		destination, destOK := values[0].(common.Address)
		amount, amountOK := values[1].(*big.Int)
		authorizedAmount, _ := auth.AmountMinorUnits.BigInt()
		if !destOK || !amountOK || destination.Hex() != auth.DestinationAddress || amount.Cmp(authorizedAmount) != 0 {
			return nil, fmt.Errorf("ERC-20 transfer calldata does not match authorization")
		}
	default:
		return nil, fmt.Errorf("unsupported asset_standard: %s", auth.AssetStandard)
	}
	return gethtypes.NewTx(&gethtypes.DynamicFeeTx{ChainID: chainID, Nonce: nonce, GasTipCap: tip, GasFeeCap: feeCap,
		Gas: unsigned.GasLimit, To: new(common.HexToAddress(to)), Value: value, Data: data, AccessList: unsigned.AccessList}), nil
}
