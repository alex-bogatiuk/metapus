package tron

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"strings"
	"time"
	"vault-plugin-secrets-tron/internal/json"
)

const (
	SigningAuthorizationSchemaVersion = 1
	SigningAuthorizationPolicyVersion = "tron-v1"
)

type SigningAuthorization struct {
	SchemaVersion        int       `json:"schema_version"`
	AuthorizationID      string    `json:"authorization_id"`
	TenantID             string    `json:"tenant_id"`
	MerchantID           *string   `json:"merchant_id,omitempty"`
	Purpose              string    `json:"purpose"`
	NetworkID            string    `json:"network_id"`
	NetworkCode          string    `json:"network_code"`
	ChainFamily          string    `json:"chain_family"`
	NetworkIdentifier    string    `json:"network_identifier"`
	SourceAddress        string    `json:"source_address"`
	FeePayerAddress      string    `json:"fee_payer_address"`
	VaultKeyID           string    `json:"vault_key_id"`
	DestinationAddress   string    `json:"destination_address"`
	AssetStandard        string    `json:"asset_standard"`
	ContractAddress      string    `json:"contract_address"`
	AmountMinorUnits     RawAmount `json:"amount_minor_units"`
	MaxNetworkFeeMinor   RawAmount `json:"max_network_fee_minor"`
	UnsignedFingerprint  string    `json:"unsigned_fingerprint"`
	BasisType            string    `json:"basis_type"`
	BasisID              string    `json:"basis_id"`
	Attempt              int       `json:"attempt"`
	Generation           int       `json:"generation"`
	NetworkSequence      string    `json:"network_sequence,omitempty"`
	ExpiresAt            time.Time `json:"expires_at"`
	PolicyVersion        string    `json:"policy_version"`
	ApprovalBundleDigest string    `json:"approval_bundle_digest,omitempty"`
}

type jwsHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
	Kid string `json:"kid,omitempty"`
}

func ParseEd25519PublicKey(encoded string) (ed25519.PublicKey, error) {
	if block, _ := pem.Decode([]byte(strings.TrimSpace(encoded))); block != nil {
		parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse PEM public key: %w", err)
		}
		key, ok := parsed.(ed25519.PublicKey)
		if !ok {
			return nil, fmt.Errorf("PEM public key is not Ed25519")
		}
		return key, nil
	}
	raw, err := decodeKeyBytes(encoded)
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid public key length: expected %d, got %d", ed25519.PublicKeySize, len(raw))
	}
	return ed25519.PublicKey(raw), nil
}

func VerifyAuthorizationJWS(jws string, publicKey ed25519.PublicKey) (SigningAuthorization, error) {
	if _, err := AuthorizationJWSKeyID(jws); err != nil {
		return SigningAuthorization{}, err
	}
	var auth SigningAuthorization
	if _, err := verifyEdDSAJWS(jws, publicKey, &auth); err != nil {
		return auth, err
	}
	return auth, nil
}

// AuthorizationJWSKeyID parses the untrusted header only to select a candidate
// key. VerifyAuthorizationJWS must still be called with that key afterwards.
func AuthorizationJWSKeyID(jws string) (string, error) {
	parts := strings.Split(jws, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid authorization JWS")
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", fmt.Errorf("decode JWS header: %w", err)
	}
	var header jwsHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return "", fmt.Errorf("unmarshal JWS header: %w", err)
	}
	if header.Alg != "EdDSA" || header.Typ != "JWT" || strings.TrimSpace(header.Kid) == "" {
		return "", fmt.Errorf("unsupported authorization JWS header")
	}
	return header.Kid, nil
}

func verifyEdDSAJWS(jws string, publicKey ed25519.PublicKey, out any) ([]byte, error) {
	parts := strings.Split(jws, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid authorization JWS")
	}

	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("decode JWS header: %w", err)
	}
	var header jwsHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, fmt.Errorf("unmarshal JWS header: %w", err)
	}
	if header.Alg != "EdDSA" || header.Typ != "JWT" {
		return nil, fmt.Errorf("unsupported JWS header")
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("decode JWS signature: %w", err)
	}
	if !ed25519.Verify(publicKey, []byte(parts[0]+"."+parts[1]), sig) {
		return nil, fmt.Errorf("authorization signature verification failed")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode JWS payload: %w", err)
	}
	if out != nil {
		if err := json.Unmarshal(payload, out); err != nil {
			return nil, fmt.Errorf("unmarshal JWS payload: %w", err)
		}
	}
	return payload, nil
}

func decodeKeyBytes(encoded string) ([]byte, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, fmt.Errorf("empty key")
	}
	decoders := []func(string) ([]byte, error){
		base64.RawURLEncoding.DecodeString,
		base64.StdEncoding.DecodeString,
		base64.RawStdEncoding.DecodeString,
		func(s string) ([]byte, error) {
			s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
			return hex.DecodeString(s)
		},
	}
	for _, decode := range decoders {
		if raw, err := decode(encoded); err == nil {
			return raw, nil
		}
	}
	return nil, fmt.Errorf("failed to decode key")
}
