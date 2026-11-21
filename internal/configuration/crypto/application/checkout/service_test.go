package checkout

import (
	"context"
	"errors"
	"testing"

	"metapus/internal/configuration/crypto/domain/crypto/publicref"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
)

func TestResolveTenantUsesMetaReferenceWithoutTenantHint(t *testing.T) {
	t.Parallel()

	ref := publicref.MustNewInvoice()
	refHash, err := publicref.Hash(ref)
	if err != nil {
		t.Fatal(err)
	}
	resourceID := id.New()
	merchantID := id.New()
	repo := &fakeCheckoutRepo{
		refs: map[string]*Reference{
			refHash: {
				RefHash:      refHash,
				Prefix:       publicref.InvoicePrefix,
				TenantID:     "tenant-a",
				MerchantID:   merchantID,
				ResourceType: ResourceTypeInvoice,
				ResourceID:   resourceID,
				Status:       StatusActive,
			},
		},
	}
	svc := newTestService(t, repo, nil, "")

	resolved, tenantID, err := svc.ResolveTenant(context.Background(), ref, "pay.example.com")
	if err != nil {
		t.Fatalf("ResolveTenant: %v", err)
	}
	if tenantID != "tenant-a" {
		t.Fatalf("tenantID = %q, want tenant-a", tenantID)
	}
	if resolved == nil || resolved.ResourceID != resourceID || resolved.MerchantID != merchantID {
		t.Fatalf("unexpected resolved ref: %#v", resolved)
	}
}

func TestResolveTenantRejectsHostTenantMismatch(t *testing.T) {
	t.Parallel()

	ref := publicref.MustNewInvoice()
	refHash, err := publicref.Hash(ref)
	if err != nil {
		t.Fatal(err)
	}
	repo := &fakeCheckoutRepo{
		refs: map[string]*Reference{
			refHash: {
				RefHash:      refHash,
				Prefix:       publicref.InvoicePrefix,
				TenantID:     "tenant-a",
				MerchantID:   id.New(),
				ResourceType: ResourceTypeInvoice,
				ResourceID:   id.New(),
				Status:       StatusActive,
			},
		},
		domains: map[string]*Domain{
			"pay.example.com": {Host: "pay.example.com", TenantID: "tenant-b", Status: StatusActive},
		},
	}
	svc := newTestService(t, repo, nil, "")

	if _, _, err := svc.ResolveTenant(context.Background(), ref, "pay.example.com"); err == nil {
		t.Fatal("ResolveTenant succeeded, want mismatch error")
	}
}

func TestResolveTenantDedicatedFallbackRequiresSingleTenant(t *testing.T) {
	t.Parallel()

	ref := publicref.MustNewInvoice()
	svc := newTestService(t, &fakeCheckoutRepo{}, []*tenant.Tenant{{ID: "tenant-only"}}, "dedicated")

	resolved, tenantID, err := svc.ResolveTenant(context.Background(), ref, "localhost:8080")
	if err != nil {
		t.Fatalf("ResolveTenant dedicated: %v", err)
	}
	if resolved != nil {
		t.Fatalf("resolved = %#v, want nil for stale/unindexed ref", resolved)
	}
	if tenantID != "tenant-only" {
		t.Fatalf("tenantID = %q, want tenant-only", tenantID)
	}
}

func TestPaymentURLsAreBuiltFromConfiguredBase(t *testing.T) {
	t.Parallel()

	svc := newTestService(t, &fakeCheckoutRepo{}, nil, "")

	if got, want := svc.InvoicePayURL("ci_0123456789abcdef0123456789abcdef"), "http://127.0.0.1:8080/pay/i/ci_0123456789abcdef0123456789abcdef"; got != want {
		t.Fatalf("InvoicePayURL = %q, want %q", got, want)
	}
	if got, want := svc.PaymentLinkPayURL("pl_0123456789abcdef0123456789abcdef"), "http://127.0.0.1:8080/pay/l/pl_0123456789abcdef0123456789abcdef"; got != want {
		t.Fatalf("PaymentLinkPayURL = %q, want %q", got, want)
	}
}

func newTestService(t *testing.T, repo Repository, activeTenants []*tenant.Tenant, mode string) *Service {
	t.Helper()
	svc, err := NewService(Config{
		Repo:          repo,
		Registry:      fakeTenantRegistry{active: activeTenants},
		PublicBaseURL: "http://127.0.0.1:8080",
		TenancyMode:   mode,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc
}

type fakeCheckoutRepo struct {
	refs    map[string]*Reference
	domains map[string]*Domain
}

func (r *fakeCheckoutRepo) Reserve(context.Context, Reference) error      { return nil }
func (r *fakeCheckoutRepo) Activate(context.Context, string) error        { return nil }
func (r *fakeCheckoutRepo) Disable(context.Context, string) error         { return nil }
func (r *fakeCheckoutRepo) EnsureActive(context.Context, Reference) error { return nil }
func (r *fakeCheckoutRepo) ResolveActive(_ context.Context, refHash string) (*Reference, error) {
	if r.refs == nil {
		return nil, nil
	}
	return r.refs[refHash], nil
}
func (r *fakeCheckoutRepo) ResolveDomain(_ context.Context, host string) (*Domain, error) {
	if r.domains == nil {
		return nil, nil
	}
	return r.domains[host], nil
}

type fakeTenantRegistry struct {
	active []*tenant.Tenant
}

func (r fakeTenantRegistry) GetByID(context.Context, string) (*tenant.Tenant, error) {
	return nil, errors.New("not implemented")
}
func (r fakeTenantRegistry) GetBySlug(context.Context, string) (*tenant.Tenant, error) {
	return nil, errors.New("not implemented")
}
func (r fakeTenantRegistry) ListActive(context.Context) ([]*tenant.Tenant, error) {
	return r.active, nil
}
func (r fakeTenantRegistry) ListAll(context.Context) ([]*tenant.Tenant, error) { return nil, nil }
func (r fakeTenantRegistry) Create(context.Context, *tenant.Tenant) error      { return nil }
func (r fakeTenantRegistry) WithProvisioningLock(context.Context, string, func(context.Context) error) error {
	return nil
}
func (r fakeTenantRegistry) SetProvisioningStage(context.Context, string, tenant.ProvisioningStage) error {
	return nil
}
func (r fakeTenantRegistry) MarkProvisioningFailed(context.Context, string, tenant.ProvisioningStage, string) error {
	return nil
}
func (r fakeTenantRegistry) MarkProvisioned(context.Context, string, tenant.Schema) error {
	return nil
}
func (r fakeTenantRegistry) UpdateStatusByID(context.Context, string, tenant.Status) error {
	return nil
}
func (r fakeTenantRegistry) UpdateSchema(context.Context, string, tenant.Schema) error { return nil }
