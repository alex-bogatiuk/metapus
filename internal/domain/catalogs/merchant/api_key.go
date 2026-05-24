package merchant

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
)

// APIKeyScope defines what operations a key is allowed to perform.
type APIKeyScope string

const (
	ScopeInvoiceCreate APIKeyScope = "invoice:create"
	ScopeInvoiceRead   APIKeyScope = "invoice:read"
	ScopeAddressCreate APIKeyScope = "address:create"
)

// _allowedScopes is the authoritative whitelist of valid API key scopes.
// Any scope not present here will be rejected by Validate().
// When adding a new scope: (1) declare a const above, (2) add it here.
var _allowedScopes = map[APIKeyScope]struct{}{
	ScopeInvoiceCreate: {},
	ScopeInvoiceRead:   {},
	ScopeAddressCreate: {},
}

// AllowedScopes returns a copy of all currently valid scopes (e.g. for docs/UI).
func AllowedScopes() []APIKeyScope {
	scopes := make([]APIKeyScope, 0, len(_allowedScopes))
	for s := range _allowedScopes {
		scopes = append(scopes, s)
	}
	return scopes
}

// DefaultScopes returns the minimal set of scopes for a new key.
func DefaultScopes() []APIKeyScope {
	return []APIKeyScope{ScopeInvoiceCreate, ScopeInvoiceRead}
}

// _keyPrefix is the common prefix for all merchant API keys.
// Visible prefix helps identify key type in logs/UI without exposing the secret.
const _keyPrefix = "mk_"

// _keyRandomBytes is the number of random bytes for key material (256 bits of entropy).
const _keyRandomBytes = 32

// MerchantAPIKey stores the hashed API key and its metadata.
// The plaintext key is generated once, shown to the merchant, and never stored.
//
// Audit chain:
//
//	CreatedByUserID → platform user who issued this key
//	(from the key) ← doc_crypto_invoices.api_key_id → which key created the invoice
type MerchantAPIKey struct {
	ID         id.ID         `db:"id"                  json:"id"`
	MerchantID id.ID         `db:"merchant_id"         json:"merchantId"`
	Name       string        `db:"name"                json:"name"`
	KeyPrefix  string        `db:"key_prefix"          json:"keyPrefix"` // "mk_" + first 8 chars
	KeyHash    string        `db:"key_hash"            json:"-"`         // SHA-256 hex, never sent
	Scopes     []APIKeyScope `db:"scopes"              json:"scopes"`
	IsActive   bool          `db:"is_active"           json:"isActive"`
	LastUsedAt *time.Time    `db:"last_used_at"        json:"lastUsedAt"`
	ExpiresAt  *time.Time    `db:"expires_at"          json:"expiresAt"`
	// CreatedByUserID is the platform user who issued this key.
	// NULL when created via automated processes or before this field was introduced.
	CreatedByUserID *id.ID    `db:"created_by_user_id"  json:"createdByUserId,omitempty"`
	CreatedAt       time.Time `db:"created_at"          json:"createdAt"`
	UpdatedAt       time.Time `db:"updated_at"          json:"updatedAt"`
}

// Validate checks that the API key model is internally consistent.
// Pure function — no DB access.
func (k *MerchantAPIKey) Validate(_ context.Context) error {
	if strings.TrimSpace(k.Name) == "" {
		return apperror.NewValidation("api key name is required").WithDetail("field", "name")
	}
	if len(k.Name) > 100 {
		return apperror.NewValidation("api key name must be at most 100 characters").WithDetail("field", "name")
	}
	if len(k.Scopes) == 0 {
		return apperror.NewValidation("api key must have at least one scope").WithDetail("field", "scopes")
	}
	// CWE-20: whitelist — reject any scope not in _allowedScopes.
	for _, s := range k.Scopes {
		if _, ok := _allowedScopes[s]; !ok {
			return apperror.NewValidation(
				fmt.Sprintf("unknown scope: %s", s),
			).WithDetail("field", "scopes")
		}
	}
	if k.ExpiresAt != nil && !k.ExpiresAt.After(time.Now()) {
		return apperror.NewValidation("expires_at must be in the future").WithDetail("field", "expiresAt")
	}
	return nil
}

// HasScope returns true if the key has the required scope.
func (k *MerchantAPIKey) HasScope(scope APIKeyScope) bool {
	return slices.Contains(k.Scopes, scope)
}

// IsExpired returns true if the key has an expiry date that has passed.
func (k *MerchantAPIKey) IsExpired() bool {
	return k.ExpiresAt != nil && time.Now().After(*k.ExpiresAt)
}

// GenerateKey creates a new cryptographically random API key.
//
// Returns:
//   - plaintext: the full key to show to the merchant (shown only once)
//   - key: the MerchantAPIKey with populated KeyHash and KeyPrefix (ready to store)
//
// Key format: mk_<base32(random_32_bytes)> (no padding, lowercase)
// Example:    mk_6xq4f7k2n9j3m5p8r1t2v4w6y8b0d2f4h6j8k0
// GenerateKey creates a new cryptographically random API key.
//
// Parameters:
//   - merchantID:       merchant the key belongs to
//   - name:            human-readable label
//   - scopes:          nil → DefaultScopes()
//   - expiresAt:       nil → never expires
//   - createdByUserID: platform user who issued the key (audit trail)
//
// Returns:
//   - plaintext: the full key to show once and never store
//   - key:       MerchantAPIKey ready to persist
func GenerateKey(
	merchantID id.ID,
	name string,
	scopes []APIKeyScope,
	expiresAt *time.Time,
	createdByUserID *id.ID,
) (plaintext string, key *MerchantAPIKey, err error) {
	raw := make([]byte, _keyRandomBytes)
	if _, err = rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate api key: %w", err)
	}

	// Encode as lowercase base32 (no padding) for URL-safe, case-insensitive key
	encoded := strings.ToLower(
		strings.TrimRight(base32.StdEncoding.EncodeToString(raw), "="),
	)
	plaintext = _keyPrefix + encoded

	// SHA-256 hash for storage
	hash := sha256.Sum256([]byte(plaintext))
	hashHex := hex.EncodeToString(hash[:])

	// Key prefix: "mk_" + first 8 chars of encoded (safe to show in UI)
	prefix := _keyPrefix + encoded[:8]

	if scopes == nil {
		scopes = DefaultScopes()
	}

	key = &MerchantAPIKey{
		MerchantID:      merchantID,
		Name:            name,
		KeyPrefix:       prefix,
		KeyHash:         hashHex,
		Scopes:          scopes,
		IsActive:        true,
		ExpiresAt:       expiresAt,
		CreatedByUserID: createdByUserID,
	}

	return plaintext, key, nil
}

// HashKey computes the SHA-256 hex hash of a plaintext key for lookup.
func HashKey(plaintext string) string {
	h := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(h[:])
}

// APIKeyRepository defines persistence operations for merchant API keys.
// Implementation must use TxManager from context (tenant-aware).
type APIKeyRepository interface {
	// Create stores a new API key. ID is assigned by the repository.
	Create(ctx context.Context, key *MerchantAPIKey) error

	// GetByHash looks up an active key by its SHA-256 hash.
	// Returns apperror.NotFound if no active key matches.
	// Hot-path: must use the partial index on key_hash WHERE is_active.
	GetByHash(ctx context.Context, keyHash string) (*MerchantAPIKey, error)

	// ListByMerchant returns all keys for a merchant (active and inactive), ordered by created_at DESC.
	ListByMerchant(ctx context.Context, merchantID id.ID) ([]*MerchantAPIKey, error)

	// Revoke marks a key as inactive. Returns NotFound if key does not belong to merchantID.
	Revoke(ctx context.Context, keyID, merchantID id.ID) error

	// UpdateLastUsed records the last usage time. Best-effort — caller may run this in a goroutine.
	UpdateLastUsed(ctx context.Context, keyID id.ID) error
}
