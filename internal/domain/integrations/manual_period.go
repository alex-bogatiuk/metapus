package integrations

import (
	"metapus/internal/core/apperror"
	"time"
)

func (a *Account) ManualPeriod() bool {
	definition, _ := a.Provider.Definition()
	return definition.QuotaPolicy == QuotaManual
}

// Expired fails closed for an incomplete manual period. It never advances a date.
func (a *Account) Expired(now time.Time) bool {
	if !a.ManualPeriod() {
		return false
	}
	start, end, err := a.Period(now, time.UTC)
	return err != nil || now.Before(start) || !now.Before(end)
}

func sameTime(a, b *time.Time) bool {
	return a == nil && b == nil || a != nil && b != nil && a.Equal(*b)
}

// PrepareManualPeriod plans an atomic credential replacement without I/O.
// sameKey compares the current decrypted key, not randomized ciphertext.
func (a *Account) PrepareManualPeriod(previous *Account, sameKey bool, now time.Time) error {
	if !a.ManualPeriod() {
		return nil
	}
	if a.CredentialsExpiresAt != nil {
		end := a.CredentialsExpiresAt.UTC().Truncate(time.Microsecond)
		a.CredentialsExpiresAt = &end
	}
	fieldError := func(message string) error {
		return apperror.NewValidation(message).WithDetail("field", "credentialsExpiresAt")
	}
	empty := a.PendingCredentials == nil || len(a.PendingCredentials.Keys) == 0
	if previous != nil {
		a.ManualPeriodStartedAt, a.ManualPeriodVersion = previous.ManualPeriodStartedAt, previous.ManualPeriodVersion
		if empty {
			if !sameTime(a.CredentialsExpiresAt, previous.CredentialsExpiresAt) {
				return fieldError("Для изменения срока введите новый ключ")
			}
			return nil
		}
		if sameKey {
			return apperror.NewValidation("Для продления введите новый ключ").WithDetail("field", "credentials")
		}
		if sameTime(a.CredentialsExpiresAt, previous.CredentialsExpiresAt) {
			return fieldError("Для нового ключа укажите новую дату окончания")
		}
	}
	if empty {
		return apperror.NewValidation("Добавьте ключ и срок его действия").WithDetail("field", "credentials")
	}
	if a.CredentialsExpiresAt == nil || !a.CredentialsExpiresAt.After(now) {
		return fieldError("Укажите дату окончания в будущем")
	}
	stamp := now.UTC().Truncate(time.Microsecond)
	end := a.CredentialsExpiresAt.UTC().Truncate(time.Microsecond)
	if !end.After(stamp) {
		return fieldError("Укажите дату окончания в будущем")
	}
	a.ManualPeriodStartedAt, a.CredentialsExpiresAt = &stamp, &end
	a.ManualPeriodVersion++
	return nil
}
