// Package webhooksecret stores merchant signing secrets in Vault KV v2.
package webhooksecret

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/vaultclient"
)

var validRefSegment = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

type VaultStore struct {
	client *vaultclient.Client
	mount  string
}

func NewVaultStore(client *vaultclient.Client, mount string) (*VaultStore, error) {
	if client == nil {
		return nil, fmt.Errorf("vault client is required")
	}
	mount = strings.Trim(strings.TrimSpace(mount), "/")
	if !validRefSegment.MatchString(mount) {
		return nil, fmt.Errorf("invalid Vault KV mount")
	}
	return &VaultStore{client: client, mount: mount}, nil
}

func (s *VaultStore) Put(ctx context.Context, secretRef, secret string) error {
	if secret == "" {
		return fmt.Errorf("webhook secret cannot be empty")
	}
	return s.client.DoJSON(ctx, http.MethodPost, s.dataPath(secretRef), map[string]any{
		"data": map[string]string{"secret": secret},
	}, nil)
}

func (s *VaultStore) Get(ctx context.Context, secretRef string) (string, error) {
	var response struct {
		Data struct {
			Data map[string]string `json:"data"`
		} `json:"data"`
	}
	if err := s.client.DoJSON(ctx, http.MethodGet, s.dataPath(secretRef), nil, &response); err != nil {
		return "", err
	}
	secret := response.Data.Data["secret"]
	if secret == "" {
		return "", fmt.Errorf("vault webhook secret is empty")
	}
	return secret, nil
}

func (s *VaultStore) Delete(ctx context.Context, secretRef string) error {
	err := s.client.DoJSON(ctx, http.MethodDelete, s.metadataPath(secretRef), nil, nil)
	if vaultclient.IsHTTPStatus(err, http.StatusNotFound) {
		return nil
	}
	return err
}

func (s *VaultStore) dataPath(secretRef string) string {
	return "/v1/" + s.mount + "/data/" + safeRef(secretRef)
}

func (s *VaultStore) metadataPath(secretRef string) string {
	return "/v1/" + s.mount + "/metadata/" + safeRef(secretRef)
}

func safeRef(ref string) string {
	parts := strings.Split(strings.Trim(ref, "/"), "/")
	encoded := make([]string, 0, len(parts))
	for _, part := range parts {
		if !validRefSegment.MatchString(part) {
			// References are generated internally. Returning an impossible path is
			// safer than allowing path traversal if corrupted data reaches here.
			return "invalid"
		}
		encoded = append(encoded, url.PathEscape(part))
	}
	return strings.Join(encoded, "/")
}

var _ crypto.WebhookSecretStore = (*VaultStore)(nil)
