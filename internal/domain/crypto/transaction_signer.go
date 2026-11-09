package crypto

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

type PolicySigningRequest struct {
	AuthorizationJWS string             `json:"authorization_jws"`
	ApprovalBundle   ApprovalBundle     `json:"approval_bundle"`
	UnsignedEnvelope UnsignedTxEnvelope `json:"unsigned_envelope"`
}

// PolicyAwareTransactionSigner is implemented by a dedicated Vault mount or
// plugin for exactly one family. It never exposes a generic digest-sign API.
type PolicyAwareTransactionSigner interface {
	Family() ChainFamily
	CheckReady(ctx context.Context, network NetworkRef, authorizationKID string) error
	Sign(ctx context.Context, request PolicySigningRequest) (SignedTxEnvelope, error)
}

// MerchantCustodySigner is required when a network enables isolated custody.
// Readiness must be checked against the signer's independent owner registry.
type MerchantCustodySigner interface {
	CheckMerchantCustodyReady(ctx context.Context, network NetworkRef) error
}

var ErrFamilySignerNotFound = errors.New("policy-aware family signer not found")

type FamilySignerRegistry struct {
	mu      sync.RWMutex
	signers map[ChainFamily]PolicyAwareTransactionSigner
}

func NewFamilySignerRegistry() *FamilySignerRegistry {
	return &FamilySignerRegistry{signers: make(map[ChainFamily]PolicyAwareTransactionSigner)}
}

func (r *FamilySignerRegistry) Register(signer PolicyAwareTransactionSigner) error {
	if signer == nil {
		return errors.New("policy-aware signer with valid family is required")
	}
	family := signer.Family()
	if !family.Valid() {
		return errors.New("policy-aware signer with valid family is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.signers[family]; exists {
		return fmt.Errorf("family signer already registered: %s", family)
	}
	r.signers[family] = signer
	return nil
}

func (r *FamilySignerRegistry) Resolve(family ChainFamily) (PolicyAwareTransactionSigner, error) {
	r.mu.RLock()
	signer := r.signers[family]
	r.mu.RUnlock()
	if signer == nil {
		return nil, fmt.Errorf("%w: %s", ErrFamilySignerNotFound, family)
	}
	return signer, nil
}

func (r *FamilySignerRegistry) Len() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.signers)
}
