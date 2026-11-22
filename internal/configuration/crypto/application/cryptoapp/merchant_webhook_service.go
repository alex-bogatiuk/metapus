package cryptoapp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/tx"
)

type MerchantWebhookService struct {
	repo        crypto.MerchantWebhookRepository
	secrets     crypto.WebhookSecretStore
	txm         tx.Manager
	now         func() time.Time
	validateURL func(string) error
}

func NewMerchantWebhookService(repo crypto.MerchantWebhookRepository, secrets crypto.WebhookSecretStore, txm tx.Manager, validateURL func(string) error) (*MerchantWebhookService, error) {
	if repo == nil || secrets == nil || txm == nil || validateURL == nil {
		return nil, fmt.Errorf("merchant webhook repository, secret store and transaction manager are required")
	}
	return &MerchantWebhookService{repo: repo, secrets: secrets, txm: txm, now: time.Now, validateURL: validateURL}, nil
}

type CreateWebhookEndpointInput struct {
	MerchantID    id.ID
	Name          string
	URL           string
	EnabledEvents []crypto.WebhookEventType
	IsDefault     bool
}

func (s *MerchantWebhookService) CreateEndpoint(ctx context.Context, in CreateWebhookEndpointInput) (*crypto.MerchantWebhookEndpoint, string, error) {
	endpoint := &crypto.MerchantWebhookEndpoint{
		ID: id.New(), MerchantID: in.MerchantID, Name: strings.TrimSpace(in.Name),
		URL: strings.TrimSpace(in.URL), EnabledEvents: in.EnabledEvents,
		IsDefault: in.IsDefault, IsActive: true, SecretVersion: 1, Version: 1,
	}
	if err := validateWebhookEndpoint(endpoint, s.validateURL); err != nil {
		return nil, "", err
	}
	secret, err := generateWebhookSecret()
	if err != nil {
		return nil, "", err
	}
	endpoint.SecretRef = webhookSecretRef(endpoint.MerchantID, endpoint.ID, 1)
	if err := s.secrets.Put(ctx, endpoint.SecretRef, secret); err != nil {
		return nil, "", fmt.Errorf("store webhook secret: %w", err)
	}
	if err := s.txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		return s.repo.CreateEndpoint(txCtx, endpoint)
	}); err != nil {
		_ = s.secrets.Delete(ctx, endpoint.SecretRef)
		return nil, "", err
	}
	return endpoint, secret, nil
}

func (s *MerchantWebhookService) UpdateEndpoint(ctx context.Context, endpoint *crypto.MerchantWebhookEndpoint) error {
	if err := validateWebhookEndpoint(endpoint, s.validateURL); err != nil {
		return err
	}
	return s.txm.RunInTransaction(ctx, func(txCtx context.Context) error { return s.repo.UpdateEndpoint(txCtx, endpoint) })
}

func (s *MerchantWebhookService) DeleteEndpoint(ctx context.Context, merchantID, endpointID id.ID) error {
	return s.txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		return s.repo.DeleteEndpoint(txCtx, merchantID, endpointID)
	})
}

func (s *MerchantWebhookService) RotateSecret(ctx context.Context, merchantID, endpointID id.ID) (*crypto.MerchantWebhookEndpoint, string, error) {
	secret, err := generateWebhookSecret()
	if err != nil {
		return nil, "", err
	}
	// The Vault ref is unique per request rather than derived from a version
	// read outside the database transaction. Concurrent rotations therefore
	// cannot overwrite each other's key material; PostgreSQL serializes the
	// visible secretVersion on the endpoint row.
	newRef := webhookRotatedSecretRef(merchantID, endpointID)
	if err := s.secrets.Put(ctx, newRef, secret); err != nil {
		return nil, "", fmt.Errorf("store rotated webhook secret: %w", err)
	}
	var rotated *crypto.MerchantWebhookEndpoint
	now := s.now().UTC()
	if err := s.txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		var rotateErr error
		rotated, rotateErr = s.repo.RotateEndpointSecret(txCtx, merchantID, endpointID, newRef, now)
		return rotateErr
	}); err != nil {
		_ = s.secrets.Delete(ctx, newRef)
		return nil, "", err
	}
	return rotated, secret, nil
}

func (s *MerchantWebhookService) Redeliver(ctx context.Context, merchantID id.ID, eventID string, endpointID id.ID) error {
	now := s.now().UTC()
	return s.txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		return s.repo.CreateManualJob(txCtx, merchantID, eventID, endpointID, now)
	})
}

func validateWebhookEndpoint(endpoint *crypto.MerchantWebhookEndpoint, validateURL func(string) error) error {
	if id.IsNil(endpoint.MerchantID) {
		return apperror.NewValidation("merchant is required")
	}
	if endpoint.Name == "" || len(endpoint.Name) > 100 {
		return apperror.NewValidation("endpoint name must contain 1 to 100 characters").WithDetail("field", "name")
	}
	if err := validateURL(endpoint.URL); err != nil {
		return err
	}
	if len(endpoint.EnabledEvents) == 0 {
		return apperror.NewValidation("at least one event type is required").WithDetail("field", "enabledEvents")
	}
	if endpoint.Version < 1 {
		return apperror.NewValidation("webhook endpoint version must be positive").WithDetail("field", "version")
	}
	seen := make(map[crypto.WebhookEventType]struct{}, len(endpoint.EnabledEvents))
	for _, event := range endpoint.EnabledEvents {
		if !crypto.IsKnownWebhookEventType(event) {
			return apperror.NewValidation("unknown webhook event type: "+string(event)).WithDetail("field", "enabledEvents")
		}
		if _, duplicate := seen[event]; duplicate {
			return apperror.NewValidation("duplicate webhook event type: "+string(event)).WithDetail("field", "enabledEvents")
		}
		seen[event] = struct{}{}
	}
	return nil
}

func generateWebhookSecret() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate webhook secret: %w", err)
	}
	return "whsec_" + hex.EncodeToString(buffer), nil
}

func webhookSecretRef(merchantID, endpointID id.ID, version int) string {
	return fmt.Sprintf("merchant-webhooks/%s/%s/v%d", merchantID, endpointID, version)
}

func webhookRotatedSecretRef(merchantID, endpointID id.ID) string {
	return fmt.Sprintf("merchant-webhooks/%s/%s/rot_%s", merchantID, endpointID, id.Compact(id.New()))
}
