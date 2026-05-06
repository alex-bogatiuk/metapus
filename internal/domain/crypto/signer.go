package crypto

import "context"

// Signer abstracts blockchain transaction signing.
// Production: HashiCorp Vault / KMS adapter.
// Development: MockSigner (deterministic, no real signing).
type Signer interface {
	// GenerateAddress creates a new blockchain address for the given derivation path.
	// Returns the address in the network's native format (e.g., base58 for TRON).
	GenerateAddress(ctx context.Context, network string, derivationPath string) (address string, err error)

	// SignTransaction signs a raw transaction for the given address.
	// Returns the signed transaction bytes ready for broadcast.
	SignTransaction(ctx context.Context, network string, address string, rawTx []byte) (signedTx []byte, err error)
}
