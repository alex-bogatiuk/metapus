package crypto

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
)

var (
	ErrInvalidSigningKey = errors.New("invalid Ed25519 signing key")
	ErrInvalidJWS        = errors.New("invalid signing authorization JWS")
)

type signingJWSHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
	Kid string `json:"kid,omitempty"`
}

func decodeHexString(s string) ([]byte, error) {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	return hex.DecodeString(s)
}

// ParseEd25519PublicKey parses a base64url, base64, or hex-encoded Ed25519
// public key.
func ParseEd25519PublicKey(encoded string) (ed25519.PublicKey, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, fmt.Errorf("%w: empty public key", ErrInvalidSigningKey)
	}
	decoders := []func(string) ([]byte, error){
		base64.RawURLEncoding.DecodeString,
		base64.StdEncoding.DecodeString,
		base64.RawStdEncoding.DecodeString,
		decodeHexString,
	}
	for _, decode := range decoders {
		raw, err := decode(encoded)
		if err != nil {
			continue
		}
		if len(raw) == ed25519.PublicKeySize {
			return ed25519.PublicKey(raw), nil
		}
	}
	return nil, fmt.Errorf("%w: expected %d-byte public key", ErrInvalidSigningKey, ed25519.PublicKeySize)
}

// ParseEd25519PrivateKey parses a base64url, base64, or hex-encoded Ed25519
// private key. Both 64-byte private keys and 32-byte seeds are accepted.
func ParseEd25519PrivateKey(encoded string) (ed25519.PrivateKey, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, fmt.Errorf("%w: empty key", ErrInvalidSigningKey)
	}

	decoders := []func(string) ([]byte, error){
		base64.RawURLEncoding.DecodeString,
		base64.StdEncoding.DecodeString,
		base64.RawStdEncoding.DecodeString,
		decodeHexString,
	}
	for _, decode := range decoders {
		raw, err := decode(encoded)
		if err != nil {
			continue
		}
		switch len(raw) {
		case ed25519.PrivateKeySize:
			return ed25519.PrivateKey(raw), nil
		case ed25519.SeedSize:
			return ed25519.NewKeyFromSeed(raw), nil
		}
	}

	return nil, fmt.Errorf("%w: expected 32-byte seed or 64-byte private key", ErrInvalidSigningKey)
}

// SignEdDSAJWS returns a compact JWS with an EdDSA header and a JSON payload.
func SignEdDSAJWS(payload any, privateKey ed25519.PrivateKey) (string, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("%w: expected %d bytes", ErrInvalidSigningKey, ed25519.PrivateKeySize)
	}

	headerBytes, err := json.Marshal(signingJWSHeader{Alg: "EdDSA", Typ: "JWT"})
	if err != nil {
		return "", err
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	header := base64.RawURLEncoding.EncodeToString(headerBytes)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	signingInput := header + "." + encodedPayload
	signature := ed25519.Sign(privateKey, []byte(signingInput))

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

// BuildApprovalJWSSigningInput creates the deterministic compact-JWS input
// used by an external approval signing service.
func BuildApprovalJWSSigningInput(payload any, kid string) (string, error) {
	kid = strings.TrimSpace(kid)
	if kid == "" {
		return "", fmt.Errorf("%w: approval kid is required", ErrInvalidJWS)
	}
	headerBytes, err := json.Marshal(signingJWSHeader{Alg: "EdDSA", Typ: "JWT", Kid: kid})
	if err != nil {
		return "", fmt.Errorf("marshal approval JWS header: %w", err)
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal approval JWS payload: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(headerBytes) + "." + base64.RawURLEncoding.EncodeToString(payloadBytes), nil
}

// CompleteApprovalJWS appends an Ed25519 signature returned by an external
// signer to a previously built signing input.
func CompleteApprovalJWS(signingInput string, signature []byte) (string, error) {
	if len(strings.Split(signingInput, ".")) != 2 {
		return "", fmt.Errorf("%w: invalid approval signing input", ErrInvalidJWS)
	}
	if len(signature) != ed25519.SignatureSize {
		return "", fmt.Errorf("%w: expected %d-byte signature", ErrInvalidJWS, ed25519.SignatureSize)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

// BuildAuthorizationJWSSigningInput creates the deterministic compact-JWS
// header.payload bytes signed by Local or Vault Transit control-plane signers.
func BuildAuthorizationJWSSigningInput(auth SigningAuthorization, kid string) (string, error) {
	kid = strings.TrimSpace(kid)
	if kid == "" {
		return "", fmt.Errorf("%w: authorization kid is required", ErrInvalidJWS)
	}
	headerBytes, err := json.Marshal(signingJWSHeader{Alg: "EdDSA", Typ: "JWT", Kid: kid})
	if err != nil {
		return "", fmt.Errorf("marshal authorization JWS header: %w", err)
	}
	payloadBytes, err := json.Marshal(auth)
	if err != nil {
		return "", fmt.Errorf("marshal authorization JWS payload: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(headerBytes) + "." + base64.RawURLEncoding.EncodeToString(payloadBytes), nil
}

// CompleteAuthorizationJWS appends an Ed25519 signature to a signing input.
func CompleteAuthorizationJWS(signingInput string, signature []byte) (string, error) {
	if len(strings.Split(signingInput, ".")) != 2 {
		return "", fmt.Errorf("%w: invalid authorization signing input", ErrInvalidJWS)
	}
	if len(signature) != ed25519.SignatureSize {
		return "", fmt.Errorf("%w: expected %d-byte signature", ErrInvalidJWS, ed25519.SignatureSize)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

// AuthorizationJWSKeyID returns the verified-format kid header value. It does
// not verify the signature and must only be used to select a candidate key.
func AuthorizationJWSKeyID(jws string) (string, error) {
	parts := strings.Split(jws, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("%w: expected compact JWS", ErrInvalidJWS)
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", fmt.Errorf("%w: decode header: %v", ErrInvalidJWS, err)
	}
	var header signingJWSHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return "", fmt.Errorf("%w: unmarshal header: %v", ErrInvalidJWS, err)
	}
	if header.Alg != "EdDSA" || header.Typ != "JWT" || strings.TrimSpace(header.Kid) == "" {
		return "", fmt.Errorf("%w: unsupported authorization header", ErrInvalidJWS)
	}
	return header.Kid, nil
}

// DecodeJWSPayload returns the decoded payload bytes from a compact JWS without
// trusting or verifying its signature.
func DecodeJWSPayload(jws string) ([]byte, error) {
	parts := strings.Split(jws, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("%w: expected compact JWS", ErrInvalidJWS)
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: decode payload: %v", ErrInvalidJWS, err)
	}
	return payloadBytes, nil
}

// JWSPayloadDigest returns the SHA-256 hex digest of the exact compact-JWS
// payload bytes. Callers use this instead of re-marshalling JSON, which would
// make digests sensitive to serializer differences.
func JWSPayloadDigest(jws string) (string, error) {
	payloadBytes, err := DecodeJWSPayload(jws)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payloadBytes)
	return hex.EncodeToString(sum[:]), nil
}

// VerifyEdDSAJWS verifies and decodes a compact EdDSA JWS into out.
func VerifyEdDSAJWS(jws string, publicKey ed25519.PublicKey, out any) ([]byte, error) {
	if len(publicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: invalid public key", ErrInvalidJWS)
	}

	parts := strings.Split(jws, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("%w: expected compact JWS", ErrInvalidJWS)
	}

	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: decode header: %v", ErrInvalidJWS, err)
	}
	var header signingJWSHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, fmt.Errorf("%w: unmarshal header: %v", ErrInvalidJWS, err)
	}
	if header.Alg != "EdDSA" || header.Typ != "JWT" {
		return nil, fmt.Errorf("%w: unsupported header", ErrInvalidJWS)
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("%w: decode signature: %v", ErrInvalidJWS, err)
	}
	if !ed25519.Verify(publicKey, []byte(parts[0]+"."+parts[1]), sig) {
		return nil, fmt.Errorf("%w: signature verification failed", ErrInvalidJWS)
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: decode payload: %v", ErrInvalidJWS, err)
	}
	if out != nil {
		if err := json.Unmarshal(payloadBytes, out); err != nil {
			return nil, fmt.Errorf("%w: unmarshal payload: %v", ErrInvalidJWS, err)
		}
	}
	return payloadBytes, nil
}
