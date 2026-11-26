package settings

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/domain/auth"
)

// lockDateLayout is how the lock date is written: a calendar day.
const lockDateLayout = "2006-01-02"

// PeriodLockSettings close the period up to a date (дата запрета
// изменения): no document dated on or before it is created, changed,
// posted, unposted or marked for deletion, nor moved into or out of it.
type PeriodLockSettings struct {
	// Date is the last closed day, YYYY-MM-DD in the tenant's timezone;
	// empty when nothing is closed.
	Date string `json:"date"`
}

// DefaultPeriodLock closes nothing.
func DefaultPeriodLock() PeriodLockSettings { return PeriodLockSettings{} }

// Validate checks the date's form.
func (p PeriodLockSettings) Validate() error {
	if p.Date == "" {
		return nil
	}
	if _, err := time.Parse(lockDateLayout, p.Date); err != nil {
		return apperror.NewValidation("Дата запрета: укажите день в формате ГГГГ-ММ-ДД")
	}
	return nil
}

// CheckDocumentDates refuses a change of a document dated dates (its new
// date and, for a stored document, its stored one) when one of them is in
// the closed period of the tenant's timezone. A request without a user is
// the system's and is not refused, nor is an administrator or a user with
// auth.PermissionClosedPeriodEdit (the exception roles get in the role
// editor).
func (p PeriodLockSettings) CheckDocumentDates(ctx context.Context, timezone string, dates ...time.Time) error {
	if p.Date == "" {
		return nil
	}
	user := appctx.GetUser(ctx)
	if user == nil || user.IsAdmin || slices.Contains(user.Permissions, auth.PermissionClosedPeriodEdit) {
		return nil
	}
	location, err := time.LoadLocation(timezone)
	if err != nil || timezone == "" {
		location = time.UTC
	}
	for _, date := range dates {
		if date.IsZero() {
			continue
		}
		if day := date.In(location).Format(lockDateLayout); day <= p.Date {
			closed, _ := time.Parse(lockDateLayout, p.Date)
			return apperror.NewPeriodClosed(fmt.Sprintf("до %s включительно", closed.Format("02.01.2006"))).
				WithDetail("documentDate", day).WithDetail("lockDate", p.Date)
		}
	}
	return nil
}
