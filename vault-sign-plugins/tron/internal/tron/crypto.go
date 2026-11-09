package tron

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
)

// SignTRONHash signs a 32-byte transaction hash using ECDSA
// (secp256k1). The returned signature is r || s || recoveryId, where
// recoveryId is 0 or 1, matching java-tron/gotron transaction signing.
func SignTRONHash(privateKeyHex string, txIDHex string) (string, error) {
	privateKeyHex = strings.TrimPrefix(strings.ToLower(privateKeyHex), "0x")
	txIDHex = strings.TrimPrefix(strings.ToLower(txIDHex), "0x")

	if len(txIDHex) != 64 {
		return "", fmt.Errorf("invalid txID length: expected 64 hex chars (32 bytes), got %d", len(txIDHex))
	}

	privBytes, err := hex.DecodeString(privateKeyHex)
	if err != nil {
		return "", fmt.Errorf("failed to decode private key: %w", err)
	}

	if len(privBytes) != 32 {
		return "", fmt.Errorf("invalid private key length: expected 32, got %d", len(privBytes))
	}

	hashBytes, err := hex.DecodeString(txIDHex)
	if err != nil {
		return "", fmt.Errorf("failed to decode txID: %w", err)
	}

	privateKey, err := crypto.ToECDSA(privBytes)
	if err != nil {
		return "", fmt.Errorf("failed to create ECDSA key: %w", err)
	}

	// crypto.Sign returns 65 bytes [R(32) || S(32) || V(1)]
	// V is 0 or 1.
	signature, err := crypto.Sign(hashBytes, privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign hash: %w", err)
	}

	if len(signature) != 65 {
		return "", fmt.Errorf("invalid signature length: expected 65 bytes, got %d", len(signature))
	}

	return hex.EncodeToString(signature), nil
}
