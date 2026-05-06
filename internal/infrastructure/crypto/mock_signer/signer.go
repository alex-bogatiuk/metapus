// Package mock_signer provides a development-only signer implementation.
// Generates deterministic addresses from derivation paths (SHA-256 based).
// MUST NOT be used in production — enforced by APP_ENV check.
package mock_signer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sync"

	"metapus/internal/domain/crypto"
	"metapus/pkg/logger"
)

func init() {
	if os.Getenv("APP_ENV") == "production" {
		panic("mock_signer: MUST NOT be used in production! Configure a real Signer (Vault/KMS)")
	}
}

// MockSigner is a development-only signer that generates deterministic addresses.
type MockSigner struct {
	mu      sync.Mutex
	counter int // HD-like counter for unique addresses
}

// New creates a new MockSigner.
func New() *MockSigner {
	return &MockSigner{}
}

// GenerateAddress implements crypto.Signer.
// Generates a deterministic address from the derivation path using SHA-256.
func (s *MockSigner) GenerateAddress(_ context.Context, network string, derivationPath string) (string, error) {
	s.mu.Lock()
	s.counter++
	counter := s.counter
	s.mu.Unlock()

	// Create deterministic address: SHA-256(network + path + counter)
	input := fmt.Sprintf("%s:%s:%d", network, derivationPath, counter)
	hash := sha256.Sum256([]byte(input))
	hexAddr := hex.EncodeToString(hash[:])

	// Format address based on network
	var address string
	switch network {
	case "tron_mainnet", "tron_testnet":
		// TRON addresses start with "T" (41 in hex)
		address = "T" + hexAddr[:33] // TRON address is 34 chars
	case "ethereum_mainnet", "ethereum_goerli":
		// Ethereum addresses start with "0x"
		address = "0x" + hexAddr[:40]
	default:
		address = "MOCK-" + hexAddr[:32]
	}

	logger.Warn(context.Background(), "mock signer: generated address",
		"network", network,
		"path", derivationPath,
		"address", address,
	)

	return address, nil
}

// SignTransaction implements crypto.Signer.
// Returns the rawTx unchanged (passthrough — no real signing).
func (s *MockSigner) SignTransaction(_ context.Context, network string, address string, rawTx []byte) ([]byte, error) {
	logger.Warn(context.Background(), "mock signer: passthrough sign",
		"network", network,
		"address", address,
		"tx_size", len(rawTx),
	)

	// Passthrough — no real signing
	return rawTx, nil
}

// Compile-time interface check.
var _ crypto.Signer = (*MockSigner)(nil)
