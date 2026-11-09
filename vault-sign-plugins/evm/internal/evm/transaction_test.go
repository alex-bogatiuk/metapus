package evm

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"testing"
	"time"
	"vault-plugin-secrets-evm/internal/json"

	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

func TestDeriveWalletUsesEthereumBIP44Path(t *testing.T) {
	seed := make([]byte, 64)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	wallet, err := DeriveWalletFromSeed(seed, "m/44'/60'/0'/0/7")
	require.NoError(t, err)
	require.NotEmpty(t, wallet.PrivateKey)
	require.Equal(t, wallet.Address, mustNormalize(t, wallet.Address))
	require.Error(t, ValidateHDPath("m/44'/195'/0'/0/7"))
	require.Error(t, ValidateHDPath("m/44'/60'/0'/1/7"))
}

func TestSignAuthorizedNativeEIP1559Transaction(t *testing.T) {
	seed := make([]byte, 64)
	for i := range seed {
		seed[i] = byte(64 - i)
	}
	wallet, err := DeriveWalletFromSeed(seed, "m/44'/60'/0'/0/1")
	require.NoError(t, err)
	destinationWallet, err := DeriveWalletFromSeed(seed, "m/44'/60'/0'/0/2")
	require.NoError(t, err)

	unsigned := UnsignedTransaction{ChainID: SepoliaChainID, Nonce: "7", MaxPriorityFeePerGasWei: "1000000000",
		MaxFeePerGasWei: "3000000000", GasLimit: 21000, To: destinationWallet.Address, ValueWei: "1000000000000000", Data: "0x", AccessList: gethtypes.AccessList{}}
	fingerprint, err := UnsignedFingerprint(unsigned)
	require.NoError(t, err)
	envelope := UnsignedEnvelope{Family: "evm", SchemaVersion: 1, UnsignedFingerprint: fingerprint, Generation: 1, NetworkSequence: "7", Payload: unsigned}
	auth := SigningAuthorization{SchemaVersion: 2, AuthorizationID: "auth-1", TenantID: "00000000-0000-0000-0000-000000000001",
		Purpose: "sweep", NetworkID: "11155111-0000-4000-8000-000000000001", NetworkCode: "ETHEREUM-SEPOLIA", ChainFamily: "evm", NetworkIdentifier: SepoliaChainID,
		SourceAddress: wallet.Address, FeePayerAddress: wallet.Address, VaultKeyID: "vault-key", DestinationAddress: destinationWallet.Address,
		AssetStandard: AssetStandardNative, AmountMinorUnits: RawAmount("1000000000000000"), MaxNetworkFeeMinor: RawAmount("63000000000000"),
		UnsignedFingerprint: fingerprint, BasisType: "CryptoSweepAttempt", BasisID: "00000000-0000-0000-0000-000000000002", Attempt: 1, Generation: 1,
		NetworkSequence: "7", ExpiresAt: time.Now().UTC().Add(time.Minute), PolicyVersion: SigningAuthorizationPolicyVersion}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	jws := signTestJWS(t, auth, privateKey)

	signed, verified, err := SignAuthorizedTransaction(wallet.PrivateKey, publicKey, jws, ApprovalBundle{}, ApprovalPublicKeys{}, envelope,
		time.Now().UTC(), "vault-key", auth.TenantID, map[string]bool{SepoliaChainID: true})
	require.NoError(t, err)
	require.Equal(t, auth.AuthorizationID, verified.AuthorizationID)
	require.Equal(t, fingerprint, signed.UnsignedFingerprint)
	require.Equal(t, "7", signed.NetworkSequence)
	require.NotEmpty(t, signed.TxHash)

	encodedRaw := signed.Payload.RawTransaction[2:]
	raw, err := hex.DecodeString(encodedRaw)
	require.NoError(t, err)
	var transaction gethtypes.Transaction
	require.NoError(t, transaction.UnmarshalBinary(raw))
	require.Equal(t, uint8(gethtypes.DynamicFeeTxType), transaction.Type())
	require.Equal(t, uint64(7), transaction.Nonce())
	require.Equal(t, destinationWallet.Address, transaction.To().Hex())
}

func TestAuthorizedTransactionRejectsFeeOrCalldataMutation(t *testing.T) {
	auth := SigningAuthorization{MaxNetworkFeeMinor: RawAmount("100"), AssetStandard: AssetStandardERC20,
		ContractAddress: SepoliaPYUSDContract, DestinationAddress: "0x1111111111111111111111111111111111111111", AmountMinorUnits: RawAmount("1"),
		NetworkIdentifier: SepoliaChainID, NetworkSequence: "0"}
	unsigned := UnsignedTransaction{ChainID: SepoliaChainID, Nonce: "0", MaxPriorityFeePerGasWei: "2", MaxFeePerGasWei: "3", GasLimit: 100,
		To: SepoliaPYUSDContract, ValueWei: "0", Data: "0xdeadbeef", AccessList: gethtypes.AccessList{}}
	_, err := reconstructAndValidate(unsigned, auth)
	require.Error(t, err)
}

func signTestJWS(t *testing.T, value any, key ed25519.PrivateKey) string {
	t.Helper()
	header, _ := json.Marshal(map[string]string{"alg": "EdDSA", "typ": "JWT", "kid": "test"})
	payload, _ := json.Marshal(value)
	encodedHeader := base64.RawURLEncoding.EncodeToString(header)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	signingInput := encodedHeader + "." + encodedPayload
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, []byte(signingInput)))
}

func mustNormalize(t *testing.T, value string) string {
	t.Helper()
	normalized, err := NormalizeAddress(value)
	require.NoError(t, err)
	return normalized
}
