package tron

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/fbsobreira/gotron-sdk/pkg/keys/hd"
	"github.com/mr-tron/base58"
)

// WalletData represents the basic structure of a TRON wallet.
type WalletData struct {
	Address    string
	PrivateKey string
	PublicKey  string
}

// GenerateWallet generates a new secure TRON wallet.
func GenerateWallet() (*WalletData, error) {
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	address, err := addressFromPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to derive address: %w", err)
	}

	privBytes := crypto.FromECDSA(privateKey)
	pubBytes := crypto.FromECDSAPub(&privateKey.PublicKey)

	return &WalletData{
		Address:    address,
		PrivateKey: hex.EncodeToString(privBytes),
		PublicKey:  hex.EncodeToString(pubBytes),
	}, nil
}

// ValidateTRONHDPath enforces the production derivation allowlist:
// m/44'/195'/{account}'/0/{index}.
func ValidateTRONHDPath(path string) error {
	normalized := strings.TrimSpace(path)
	if !strings.HasPrefix(normalized, "m/") {
		return fmt.Errorf("derivation path must start with m/")
	}
	params, err := hd.NewParamsFromPath(normalized)
	if err != nil {
		return err
	}
	if params.Purpose != 44 {
		return fmt.Errorf("purpose must be 44'")
	}
	if params.CoinType != 195 {
		return fmt.Errorf("coin type must be 195'")
	}
	if params.Change {
		return fmt.Errorf("change must be 0")
	}
	if normalized != "m/"+params.String() {
		return fmt.Errorf("derivation path is not canonical")
	}
	return nil
}

// DeriveWalletFromSeed derives a TRON wallet from a BIP-39 seed and allowed
// TRON BIP-44 derivation path.
func DeriveWalletFromSeed(seed []byte, derivationPath string) (*WalletData, error) {
	if err := ValidateTRONHDPath(derivationPath); err != nil {
		return nil, err
	}
	if len(seed) < 32 {
		return nil, fmt.Errorf("HD seed must be at least 32 bytes")
	}

	master, chainCode := hd.ComputeMastersFromSeed(seed, []byte("Bitcoin seed"))
	derived, err := hd.DerivePrivateKeyForPath(btcec.S256(), master, chainCode, derivationPath)
	if err != nil {
		return nil, err
	}

	privateKey, err := crypto.ToECDSA(derived[:])
	if err != nil {
		return nil, fmt.Errorf("failed to create derived private key: %w", err)
	}
	address, err := addressFromPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to derive address: %w", err)
	}

	return &WalletData{
		Address:    address,
		PrivateKey: hex.EncodeToString(crypto.FromECDSA(privateKey)),
		PublicKey:  hex.EncodeToString(crypto.FromECDSAPub(&privateKey.PublicKey)),
	}, nil
}

// addressFromPublicKey converts an ECDSA public key to a TRON Base58Check address.
func addressFromPublicKey(pubKey *ecdsa.PublicKey) (string, error) {
	// 1. Get the uncompressed public key bytes (65 bytes, prefix 0x04)
	pubBytes := crypto.FromECDSAPub(pubKey)
	if len(pubBytes) == 0 {
		return "", fmt.Errorf("invalid public key")
	}

	// 2. Hash the public key (excluding the 0x04 prefix) using Keccak256
	keccakHash := crypto.Keccak256(pubBytes[1:])

	// 3. Take the last 20 bytes
	if len(keccakHash) < 20 {
		return "", fmt.Errorf("invalid keccak hash length")
	}
	addressPart := keccakHash[len(keccakHash)-20:]

	// 4. Prepend TRON prefix (0x41)
	addressBytes := append([]byte{0x41}, addressPart...)

	// 5. Calculate double SHA256 checksum
	sum1 := sha256.Sum256(addressBytes)
	sum2 := sha256.Sum256(sum1[:])
	checksum := sum2[:4]

	// 6. Base58Encode (Address + Checksum)
	finalBytes := append(addressBytes, checksum...)
	return base58.Encode(finalBytes), nil
}

// ValidateAddress checks if the given string is a valid TRON Base58Check address.
func ValidateAddress(address string) bool {
	decoded, err := base58.Decode(address)
	if err != nil {
		return false
	}

	// Length must be 25 bytes (1 prefix + 20 address + 4 checksum)
	if len(decoded) != 25 {
		return false
	}

	// Prefix must be 0x41 (Mainnet)
	if decoded[0] != 0x41 {
		return false
	}

	addressBytes := decoded[:21]
	expectedChecksum := decoded[21:]

	sum1 := sha256.Sum256(addressBytes)
	sum2 := sha256.Sum256(sum1[:])
	actualChecksum := sum2[:4]

	for i := range 4 {
		if expectedChecksum[i] != actualChecksum[i] {
			return false
		}
	}

	return true
}

// ValidateWalletKeyPair ensures the private key matches the given address.
func ValidateWalletKeyPair(address string, privateKeyHex string) (bool, error) {
	privateKeyHex = strings.TrimPrefix(strings.ToLower(privateKeyHex), "0x")

	privBytes, err := hex.DecodeString(privateKeyHex)
	if err != nil {
		return false, fmt.Errorf("invalid private key hex: %w", err)
	}

	if len(privBytes) != 32 {
		return false, fmt.Errorf("invalid private key length: expected 32, got %d", len(privBytes))
	}

	privateKey, err := crypto.ToECDSA(privBytes)
	if err != nil {
		return false, fmt.Errorf("failed to create ECDSA key: %w", err)
	}

	derivedAddress, err := addressFromPublicKey(&privateKey.PublicKey)
	if err != nil {
		return false, fmt.Errorf("failed to derive address: %w", err)
	}

	return derivedAddress == address, nil
}
