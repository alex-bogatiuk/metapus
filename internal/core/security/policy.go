package security

import (
	"context"
	"time"

	"metapus/internal/core/apperror"
)

// PostingPolicy defines rules for document posting.
// Different tenants may have different policies (strict vs flexible).
type PostingPolicy interface {
	// CanPost checks if document can be posted with given date
	CanPost(ctx context.Context, docDate time.Time) error
	
	// CanModify checks if posted document can be modified
	CanModify(ctx context.Context, docDate time.Time) error
	
	// CanUnpost checks if document can be unposted
	CanUnpost(ctx context.Context, docDate time.Time) error
	
	// GetClosedPeriod returns the date until which period is closed
	GetClosedPeriod(ctx context.Context) time.Time
}

// StrictPolicy forbids any changes to closed period.
// Used for regulatory compliance.
type StrictPolicy struct {
	closedUntil time.Time
}

// NewStrictPolicy creates policy that forbids changes before closedUntil.
func NewStrictPolicy(closedUntil time.Time) *StrictPolicy {
	return &StrictPolicy{closedUntil: closedUntil}
}

func (p *StrictPolicy) CanPost(ctx context.Context, docDate time.Time) error {
	if docDate.Before(p.closedUntil) {
		return apperror.NewPeriodClosed(p.closedUntil.Format("2006-01"))
	}
	return nil
}

func (p *StrictPolicy) CanModify(ctx context.Context, docDate time.Time) error {
	return p.CanPost(ctx, docDate)
}

func (p *StrictPolicy) CanUnpost(ctx context.Context, docDate time.Time) error {
	return p.CanPost(ctx, docDate)
}

func (p *StrictPolicy) GetClosedPeriod(ctx context.Context) time.Time {
	return p.closedUntil
}

// FlexiblePolicy allows backdated changes with warnings.
// Suitable for development and small businesses.
type FlexiblePolicy struct {
	warningThreshold time.Duration // Warn if older than this
	closedUntil      time.Time     // Hard limit
}

// NewFlexiblePolicy creates policy with soft warnings.
func NewFlexiblePolicy(warningThreshold time.Duration, closedUntil time.Time) *FlexiblePolicy {
	return &FlexiblePolicy{
		warningThreshold: warningThreshold,
		closedUntil:      closedUntil,
	}
}

func (p *FlexiblePolicy) CanPost(ctx context.Context, docDate time.Time) error {
	if !p.closedUntil.IsZero() && docDate.Before(p.closedUntil) {
		return apperror.NewPeriodClosed(p.closedUntil.Format("2006-01"))
	}
	// Soft warning would be logged or returned as warning, not error
	return nil
}

func (p *FlexiblePolicy) CanModify(ctx context.Context, docDate time.Time) error {
	return p.CanPost(ctx, docDate)
}

func (p *FlexiblePolicy) CanUnpost(ctx context.Context, docDate time.Time) error {
	return p.CanPost(ctx, docDate)
}

func (p *FlexiblePolicy) GetClosedPeriod(ctx context.Context) time.Time {
	return p.closedUntil
}

// IsBackdatedWarning checks if operation deserves a warning.
func (p *FlexiblePolicy) IsBackdatedWarning(docDate time.Time) bool {
	if p.warningThreshold == 0 {
		return false
	}
	return time.Since(docDate) > p.warningThreshold
}

// OpenPolicy allows all operations (for development/testing).
type OpenPolicy struct{}

func (OpenPolicy) CanPost(ctx context.Context, docDate time.Time) error   { return nil }
func (OpenPolicy) CanModify(ctx context.Context, docDate time.Time) error { return nil }
func (OpenPolicy) CanUnpost(ctx context.Context, docDate time.Time) error { return nil }
func (OpenPolicy) GetClosedPeriod(ctx context.Context) time.Time          { return time.Time{} }
