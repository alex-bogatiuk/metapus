package crypto_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"metapus/internal/configuration/crypto/application/checkout"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
)

type CheckoutReferenceRepo struct {
	pool *pgxpool.Pool
}

func NewCheckoutReferenceRepo(pool *pgxpool.Pool) *CheckoutReferenceRepo {
	return &CheckoutReferenceRepo{pool: pool}
}

func (r *CheckoutReferenceRepo) Reserve(ctx context.Context, ref checkout.Reference) error {
	if r.pool == nil {
		return fmt.Errorf("checkout reference reserve: meta pool is required")
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO public_checkout_refs
			(ref_hash, ref_prefix, tenant_id, merchant_id, resource_type, resource_id, status, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, ref.RefHash, ref.Prefix, ref.TenantID, ref.MerchantID, ref.ResourceType, ref.ResourceID, checkout.StatusReserved, ref.ExpiresAt)
	if err != nil {
		if postgres.IsUniqueViolation(err) {
			return checkout.ErrReferenceCollision
		}
		return fmt.Errorf("checkout reference reserve: %w", err)
	}
	return nil
}

func (r *CheckoutReferenceRepo) Activate(ctx context.Context, refHash string) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE public_checkout_refs
		SET status = $2, expires_at = NULL, updated_at = NOW()
		WHERE ref_hash = $1
		  AND status IN ($3, $2)
	`, refHash, checkout.StatusActive, checkout.StatusReserved)
	if err != nil {
		return fmt.Errorf("checkout reference activate: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("checkout reference activate: reference not found")
	}
	return nil
}

func (r *CheckoutReferenceRepo) Disable(ctx context.Context, refHash string) error {
	if r.pool == nil {
		return nil
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE public_checkout_refs
		SET status = $2, updated_at = NOW()
		WHERE ref_hash = $1
	`, refHash, checkout.StatusDisabled)
	if err != nil {
		return fmt.Errorf("checkout reference disable: %w", err)
	}
	return nil
}

func (r *CheckoutReferenceRepo) EnsureActive(ctx context.Context, ref checkout.Reference) error {
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO public_checkout_refs
			(ref_hash, ref_prefix, tenant_id, merchant_id, resource_type, resource_id, status, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULL)
		ON CONFLICT ON CONSTRAINT uq_public_checkout_refs_resource DO UPDATE
		SET status = EXCLUDED.status,
		    expires_at = NULL,
		    updated_at = NOW()
		WHERE public_checkout_refs.ref_hash = EXCLUDED.ref_hash
	`, ref.RefHash, ref.Prefix, ref.TenantID, ref.MerchantID, ref.ResourceType, ref.ResourceID, checkout.StatusActive)
	if err != nil {
		if postgres.IsUniqueViolation(err) {
			return checkout.ErrReferenceCollision
		}
		return fmt.Errorf("checkout reference ensure active: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("checkout reference ensure active: resource already has a different public ref")
	}
	return nil
}

func (r *CheckoutReferenceRepo) ResolveActive(ctx context.Context, refHash string) (*checkout.Reference, error) {
	var ref checkout.Reference
	var merchantID id.ID
	err := r.pool.QueryRow(ctx, `
		SELECT ref_hash, ref_prefix, tenant_id::text, merchant_id, resource_type, resource_id, status, expires_at
		FROM public_checkout_refs
		WHERE ref_hash = $1
		  AND status = $2
		  AND (expires_at IS NULL OR expires_at > NOW())
	`, refHash, checkout.StatusActive).Scan(
		&ref.RefHash, &ref.Prefix, &ref.TenantID, &merchantID,
		&ref.ResourceType, &ref.ResourceID, &ref.Status, &ref.ExpiresAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("checkout reference resolve: %w", err)
	}
	ref.MerchantID = merchantID
	return &ref, nil
}

func (r *CheckoutReferenceRepo) ResolveDomain(ctx context.Context, host string) (*checkout.Domain, error) {
	if host == "" {
		return nil, nil
	}
	var d checkout.Domain
	err := r.pool.QueryRow(ctx, `
		SELECT host, tenant_id::text, merchant_id, status, verified_at
		FROM checkout_domains
		WHERE host = $1
		  AND status = 'active'
	`, host).Scan(&d.Host, &d.TenantID, &d.MerchantID, &d.Status, &d.VerifiedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("checkout domain resolve: %w", err)
	}
	return &d, nil
}

var _ checkout.Repository = (*CheckoutReferenceRepo)(nil)
