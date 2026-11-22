package authorization_signer

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/vaultclient"
	"metapus/pkg/sdk/id"
)

var vaultMountPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type TransitConfig struct {
	Client *vaultclient.Client
	Mount  string
}

// TransitAuthorizationSigner signs authorization JWS inputs with a distinct,
// non-exportable Ed25519 Transit key for each tenant.
type TransitAuthorizationSigner struct {
	client *vaultclient.Client
	mount  string
}

func NewTransit(cfg TransitConfig) (*TransitAuthorizationSigner, error) {
	if cfg.Client == nil {
		return nil, fmt.Errorf("vault client is required")
	}
	mount := strings.TrimSpace(cfg.Mount)
	if mount == "" {
		mount = "transit"
	}
	if !vaultMountPattern.MatchString(mount) {
		return nil, fmt.Errorf("invalid Transit mount")
	}
	return &TransitAuthorizationSigner{client: cfg.Client, mount: mount}, nil
}

func (s *TransitAuthorizationSigner) SignAuthorization(ctx context.Context, tenantID string, auth crypto.SigningAuthorization) (string, error) {
	return s.sign(ctx, tenantID, auth.TenantID, func(kid string) (string, error) {
		return crypto.BuildAuthorizationJWSSigningInput(auth, kid)
	})
}

func (s *TransitAuthorizationSigner) sign(ctx context.Context, tenantID, authorizationTenantID string, buildInput func(string) (string, error)) (string, error) {
	keyName, err := transitTenantKeyName(tenantID)
	if err != nil {
		return "", err
	}
	if authorizationTenantID != tenantID {
		return "", fmt.Errorf("authorization tenant does not match signer tenant")
	}
	metadata, err := s.readMetadata(ctx, keyName)
	if err != nil {
		return "", err
	}
	version, err := metadata.readyVersion()
	if err != nil {
		return "", err
	}
	kid := transitKeyID(s.mount, keyName, version)
	input, err := buildInput(kid)
	if err != nil {
		return "", err
	}

	request := map[string]any{
		"input":       base64.StdEncoding.EncodeToString([]byte(input)),
		"key_version": version,
	}
	var response struct {
		Data struct {
			Signature string `json:"signature"`
		} `json:"data"`
	}
	if err := s.client.DoJSON(ctx, http.MethodPost, "/v1/"+s.mount+"/sign/"+keyName, request, &response); err != nil {
		return "", fmt.Errorf("transit sign authorization: %w", err)
	}
	returnedVersion, signature, err := parseTransitSignature(response.Data.Signature)
	if err != nil {
		return "", err
	}
	if returnedVersion != version {
		return "", fmt.Errorf("transit returned key version %d, requested %d", returnedVersion, version)
	}
	return crypto.CompleteAuthorizationJWS(input, signature)
}

func (s *TransitAuthorizationSigner) CheckReady(ctx context.Context, tenantID string) error {
	_, err := s.AuthorizationKeyID(ctx, tenantID)
	return err
}

func (s *TransitAuthorizationSigner) AuthorizationKeyID(ctx context.Context, tenantID string) (string, error) {
	keyName, err := transitTenantKeyName(tenantID)
	if err != nil {
		return "", err
	}
	metadata, err := s.readMetadata(ctx, keyName)
	if err != nil {
		return "", err
	}
	version, err := metadata.readyVersion()
	if err != nil {
		return "", err
	}
	return transitKeyID(s.mount, keyName, version), nil
}

type transitKeyMetadata struct {
	Data struct {
		Type            string `json:"type"`
		Exportable      bool   `json:"exportable"`
		DeletionAllowed bool   `json:"deletion_allowed"`
		LatestVersion   int    `json:"latest_version"`
		Keys            map[string]struct {
			PublicKey string `json:"public_key"`
		} `json:"keys"`
	} `json:"data"`
}

func (s *TransitAuthorizationSigner) readMetadata(ctx context.Context, keyName string) (transitKeyMetadata, error) {
	var metadata transitKeyMetadata
	if err := s.client.DoJSON(ctx, http.MethodGet, "/v1/"+s.mount+"/keys/"+keyName, nil, &metadata); err != nil {
		return metadata, fmt.Errorf("read Transit authorization key metadata: %w", err)
	}
	return metadata, nil
}

func (m transitKeyMetadata) readyVersion() (int, error) {
	if m.Data.Type != "ed25519" {
		return 0, fmt.Errorf("transit authorization key must be ed25519")
	}
	if m.Data.Exportable {
		return 0, fmt.Errorf("transit authorization key must be non-exportable")
	}
	if m.Data.DeletionAllowed {
		return 0, fmt.Errorf("transit authorization key deletion must be disabled")
	}
	version := m.Data.LatestVersion
	if version < 1 {
		for rawVersion := range m.Data.Keys {
			candidate, err := strconv.Atoi(rawVersion)
			if err == nil && candidate > version {
				version = candidate
			}
		}
	}
	if version < 1 {
		return 0, fmt.Errorf("transit authorization key has no active version")
	}
	key, ok := m.Data.Keys[strconv.Itoa(version)]
	if !ok || strings.TrimSpace(key.PublicKey) == "" {
		return 0, fmt.Errorf("transit authorization key current public key is missing")
	}
	return version, nil
}

func transitTenantKeyName(tenantID string) (string, error) {
	parsed, err := id.Parse(strings.TrimSpace(tenantID))
	if err != nil || parsed.String() != strings.ToLower(strings.TrimSpace(tenantID)) {
		return "", fmt.Errorf("tenant_id must be a canonical UUID")
	}
	return "metapus-signing-auth-" + parsed.String(), nil
}

func transitKeyID(mount, keyName string, version int) string {
	return mount + "/" + keyName + "/v" + strconv.Itoa(version)
}

func parseTransitSignature(value string) (int, []byte, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 3 || parts[0] != "vault" || !strings.HasPrefix(parts[1], "v") {
		return 0, nil, fmt.Errorf("invalid Transit signature envelope")
	}
	version, err := strconv.Atoi(strings.TrimPrefix(parts[1], "v"))
	if err != nil || version < 1 {
		return 0, nil, fmt.Errorf("invalid Transit signature key version")
	}
	var signature []byte
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding} {
		signature, err = encoding.DecodeString(parts[2])
		if err == nil {
			break
		}
	}
	if err != nil || len(signature) != 64 {
		return 0, nil, fmt.Errorf("invalid Transit Ed25519 signature")
	}
	return version, signature, nil
}

var _ crypto.SigningAuthorizationSigner = (*TransitAuthorizationSigner)(nil)
var _ crypto.SigningAuthorizationKeyIDProvider = (*TransitAuthorizationSigner)(nil)
