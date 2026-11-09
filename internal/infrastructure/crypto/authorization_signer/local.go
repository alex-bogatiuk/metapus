package authorization_signer

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"strings"

	"metapus/internal/domain/crypto"
)

// LocalAuthorizationSigner is an explicit development/test implementation.
// Product deployments must use TransitAuthorizationSigner.
type LocalAuthorizationSigner struct {
	privateKey ed25519.PrivateKey
	kid        string
}

func NewLocal(encodedPrivateKey, kid, appEnv string) (*LocalAuthorizationSigner, error) {
	if strings.EqualFold(strings.TrimSpace(appEnv), "production") {
		return nil, fmt.Errorf("local authorization signer is forbidden in production")
	}
	privateKey, err := crypto.ParseEd25519PrivateKey(encodedPrivateKey)
	if err != nil {
		return nil, err
	}
	kid = strings.TrimSpace(kid)
	if kid == "" {
		return nil, fmt.Errorf("local authorization signer kid is required")
	}
	return &LocalAuthorizationSigner{privateKey: privateKey, kid: kid}, nil
}

func (s *LocalAuthorizationSigner) SignAuthorization(_ context.Context, tenantID string, auth crypto.SigningAuthorization) (string, error) {
	if strings.TrimSpace(tenantID) == "" || auth.TenantID != tenantID {
		return "", fmt.Errorf("authorization tenant does not match signer tenant")
	}
	input, err := crypto.BuildAuthorizationJWSSigningInput(auth, s.kid)
	if err != nil {
		return "", err
	}
	return crypto.CompleteAuthorizationJWS(input, ed25519.Sign(s.privateKey, []byte(input)))
}

func (s *LocalAuthorizationSigner) CheckReady(_ context.Context, tenantID string) error {
	if strings.TrimSpace(tenantID) == "" {
		return fmt.Errorf("tenant_id is required")
	}
	if len(s.privateKey) != ed25519.PrivateKeySize || s.kid == "" {
		return fmt.Errorf("local authorization signer is not configured")
	}
	return nil
}

func (s *LocalAuthorizationSigner) AuthorizationKeyID(_ context.Context, tenantID string) (string, error) {
	if err := s.CheckReady(context.Background(), tenantID); err != nil {
		return "", err
	}
	return s.kid, nil
}

var _ crypto.SigningAuthorizationSigner = (*LocalAuthorizationSigner)(nil)
var _ crypto.SigningAuthorizationKeyIDProvider = (*LocalAuthorizationSigner)(nil)
