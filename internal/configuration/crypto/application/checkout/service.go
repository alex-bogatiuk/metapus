// Package checkout owns public checkout references and URL construction.
package checkout

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto/publicref"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/tenant"
)

var ErrReferenceCollision = errors.New("checkout public reference collision")

type ResourceType string

const (
	ResourceTypeInvoice     ResourceType = "crypto_invoice"
	ResourceTypePaymentLink ResourceType = "payment_link"

	StatusReserved = "reserved"
	StatusActive   = "active"
	StatusDisabled = "disabled"
)

type Reference struct {
	RefHash      string
	Prefix       string
	TenantID     string
	MerchantID   id.ID
	ResourceType ResourceType
	ResourceID   id.ID
	Status       string
	ExpiresAt    *time.Time
}

type Domain struct {
	Host       string
	TenantID   string
	MerchantID *id.ID
	Status     string
	VerifiedAt *time.Time
}

type ReserveInput struct {
	Prefix       string
	PublicRef    string
	ResourceType ResourceType
	ResourceID   id.ID
	MerchantID   id.ID
	ExpiresAt    *time.Time
}

type EnsureInput struct {
	PublicRef    string
	ResourceType ResourceType
	ResourceID   id.ID
	MerchantID   id.ID
	ExpiresAt    *time.Time
}

type Repository interface {
	Reserve(ctx context.Context, ref Reference) error
	Activate(ctx context.Context, refHash string) error
	Disable(ctx context.Context, refHash string) error
	EnsureActive(ctx context.Context, ref Reference) error
	ResolveActive(ctx context.Context, refHash string) (*Reference, error)
	ResolveDomain(ctx context.Context, host string) (*Domain, error)
}

type Config struct {
	Repo              Repository
	Registry          tenant.Registry
	PublicBaseURL     string
	TenancyMode       string
	DedicatedTenantID string
	Now               func() time.Time
}

type Service struct {
	repo              Repository
	registry          tenant.Registry
	publicBaseURL     string
	tenancyMode       string
	dedicatedTenantID string
	now               func() time.Time
}

func NewService(cfg Config) (*Service, error) {
	if cfg.Repo == nil {
		return nil, fmt.Errorf("checkout repository is required")
	}
	if cfg.Registry == nil {
		return nil, fmt.Errorf("tenant registry is required")
	}
	baseURL, err := normalizePublicBaseURL(cfg.PublicBaseURL)
	if err != nil {
		return nil, err
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Service{
		repo:              cfg.Repo,
		registry:          cfg.Registry,
		publicBaseURL:     baseURL,
		tenancyMode:       strings.ToLower(strings.TrimSpace(cfg.TenancyMode)),
		dedicatedTenantID: strings.TrimSpace(cfg.DedicatedTenantID),
		now:               cfg.Now,
	}, nil
}

func (s *Service) Reserve(ctx context.Context, in ReserveInput) (string, error) {
	if err := validateReserveInput(in); err != nil {
		return "", err
	}
	t, err := currentTenant(ctx)
	if err != nil {
		return "", err
	}
	if in.PublicRef != "" {
		refHash, err := publicref.Hash(in.PublicRef)
		if err != nil {
			return "", apperror.NewValidation("invalid checkout reference")
		}
		if err := s.repo.Reserve(ctx, Reference{
			RefHash:      refHash,
			Prefix:       in.Prefix,
			TenantID:     t.ID,
			MerchantID:   in.MerchantID,
			ResourceType: in.ResourceType,
			ResourceID:   in.ResourceID,
			Status:       StatusReserved,
			ExpiresAt:    in.ExpiresAt,
		}); err != nil {
			return "", err
		}
		return in.PublicRef, nil
	}

	var lastErr error
	for range 3 {
		ref, err := publicref.New(in.Prefix)
		if err != nil {
			return "", err
		}
		refHash, err := publicref.Hash(ref)
		if err != nil {
			return "", err
		}
		if err := s.repo.Reserve(ctx, Reference{
			RefHash:      refHash,
			Prefix:       in.Prefix,
			TenantID:     t.ID,
			MerchantID:   in.MerchantID,
			ResourceType: in.ResourceType,
			ResourceID:   in.ResourceID,
			Status:       StatusReserved,
			ExpiresAt:    in.ExpiresAt,
		}); err != nil {
			if !errors.Is(err, ErrReferenceCollision) {
				return "", err
			}
			lastErr = err
			continue
		}
		return ref, nil
	}
	return "", fmt.Errorf("reserve public checkout ref: %w", lastErr)
}

func (s *Service) Activate(ctx context.Context, publicRef string) error {
	refHash, err := publicref.Hash(publicRef)
	if err != nil {
		return apperror.NewValidation("invalid checkout reference")
	}
	return s.repo.Activate(ctx, refHash)
}

func (s *Service) Disable(ctx context.Context, publicRef string) error {
	refHash, err := publicref.Hash(publicRef)
	if err != nil {
		return nil
	}
	return s.repo.Disable(ctx, refHash)
}

func (s *Service) EnsureActive(ctx context.Context, in EnsureInput) error {
	if err := publicref.Validate(in.PublicRef); err != nil {
		return apperror.NewValidation("invalid checkout reference")
	}
	if err := validateReserveInput(ReserveInput{
		Prefix:       strings.SplitN(in.PublicRef, "_", 2)[0],
		ResourceType: in.ResourceType,
		ResourceID:   in.ResourceID,
		MerchantID:   in.MerchantID,
		ExpiresAt:    in.ExpiresAt,
	}); err != nil {
		return err
	}
	t, err := currentTenant(ctx)
	if err != nil {
		return err
	}
	refHash, err := publicref.Hash(in.PublicRef)
	if err != nil {
		return err
	}
	return s.repo.EnsureActive(ctx, Reference{
		RefHash:      refHash,
		Prefix:       strings.SplitN(in.PublicRef, "_", 2)[0],
		TenantID:     t.ID,
		MerchantID:   in.MerchantID,
		ResourceType: in.ResourceType,
		ResourceID:   in.ResourceID,
		Status:       StatusActive,
		ExpiresAt:    in.ExpiresAt,
	})
}

func (s *Service) ResolveTenant(ctx context.Context, publicRef, host string) (*Reference, string, error) {
	if err := publicref.Validate(publicRef); err != nil {
		return nil, "", apperror.NewNotFound("checkout", "reference")
	}

	host = normalizeHost(host)
	domain, err := s.repo.ResolveDomain(ctx, host)
	if err != nil {
		return nil, "", err
	}

	refHash, err := publicref.Hash(publicRef)
	if err != nil {
		return nil, "", apperror.NewNotFound("checkout", "reference")
	}
	ref, err := s.repo.ResolveActive(ctx, refHash)
	if err != nil {
		return nil, "", err
	}
	if ref != nil {
		if domain != nil && domain.TenantID != ref.TenantID {
			return nil, "", apperror.NewNotFound("checkout", "reference")
		}
		return ref, ref.TenantID, nil
	}

	if domain != nil {
		return nil, "", apperror.NewNotFound("checkout", "reference")
	}
	if s.tenancyMode == "dedicated" {
		tenantID, err := s.resolveDedicatedTenant(ctx)
		if err != nil {
			return nil, "", err
		}
		return nil, tenantID, nil
	}
	return nil, "", apperror.NewNotFound("checkout", "reference")
}

func (s *Service) InvoicePath(publicRef string) string {
	return "/pay/i/" + publicRef
}

func (s *Service) PaymentLinkPath(publicRef string) string {
	return "/pay/l/" + publicRef
}

func (s *Service) InvoicePayURL(publicRef string) string {
	return s.publicBaseURL + s.InvoicePath(publicRef)
}

func (s *Service) PaymentLinkPayURL(publicRef string) string {
	return s.publicBaseURL + s.PaymentLinkPath(publicRef)
}

func (s *Service) resolveDedicatedTenant(ctx context.Context) (string, error) {
	if s.dedicatedTenantID != "" {
		return s.dedicatedTenantID, nil
	}
	tenants, err := s.registry.ListActive(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve dedicated tenant: %w", err)
	}
	if len(tenants) != 1 {
		return "", fmt.Errorf("dedicated checkout requires exactly one active tenant, got %d", len(tenants))
	}
	return tenants[0].ID, nil
}

func validateReserveInput(in ReserveInput) error {
	switch in.Prefix {
	case publicref.InvoicePrefix, publicref.PaymentLinkPrefix:
	default:
		return fmt.Errorf("unsupported checkout ref prefix %q", in.Prefix)
	}
	if in.PublicRef != "" {
		if err := publicref.Validate(in.PublicRef); err != nil {
			return apperror.NewValidation("invalid checkout reference")
		}
		prefix, _ := publicref.Prefix(in.PublicRef)
		if prefix != in.Prefix {
			return apperror.NewValidation("checkout reference prefix mismatch")
		}
	}
	switch in.ResourceType {
	case ResourceTypeInvoice, ResourceTypePaymentLink:
	default:
		return fmt.Errorf("unsupported checkout resource type %q", in.ResourceType)
	}
	if id.IsNil(in.ResourceID) {
		return apperror.NewValidation("checkout resource id is required")
	}
	if id.IsNil(in.MerchantID) {
		return apperror.NewValidation("merchant is required")
	}
	return nil
}

func currentTenant(ctx context.Context) (*tenant.Tenant, error) {
	t := tenant.GetTenant(ctx)
	if t == nil || t.ID == "" {
		return nil, fmt.Errorf("tenant context is required for checkout reference")
	}
	return t, nil
}

func normalizePublicBaseURL(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return "", fmt.Errorf("PUBLIC_CHECKOUT_BASE_URL is required")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("PUBLIC_CHECKOUT_BASE_URL must be an absolute URL")
	}
	host := strings.ToLower(u.Hostname())
	if u.Scheme == "http" && host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return "", fmt.Errorf("PUBLIC_CHECKOUT_BASE_URL must use HTTPS outside localhost")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", fmt.Errorf("PUBLIC_CHECKOUT_BASE_URL must use http or https")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/"), nil
}

func normalizeHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		return strings.ToLower(h)
	}
	return host
}
