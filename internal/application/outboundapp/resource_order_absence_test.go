package outboundapp

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"metapus/internal/domain/crypto"
)

// Stand: two rentals placed against "https://https//nile.itrx.io" never
// reached iTRX; once the URL was fixed iTRX answered "Can’t find the order"
// and the rentals stayed held forever.
func TestOrderAbsenceIsSettledOnlyForOldUnacceptedRentals(t *testing.T) {
	now := time.Date(2026, 11, 19, 19, 0, 0, 0, time.UTC)
	service := &ResourceRentalService{now: func() time.Time { return now }}
	notFound := fmt.Errorf("%w: itrx query order rejected: Can’t find the order.", crypto.ErrResourceOrderNotFound)
	old := &ResourceRental{CreatedAt: now.Add(-time.Hour)}
	if !service.orderAbsenceIsSettled(old, notFound) {
		t.Fatal("an hour-old rental the provider does not know was never placed")
	}
	for name, rental := range map[string]*ResourceRental{
		"fresh":    {CreatedAt: now.Add(-time.Minute)},
		"accepted": {CreatedAt: now.Add(-time.Hour), ExternalSerial: "serial-1"},
		"unknown":  {},
	} {
		if service.orderAbsenceIsSettled(rental, notFound) {
			t.Errorf("%s rental must stay ambiguous", name)
		}
	}
	if service.orderAbsenceIsSettled(old, errors.New("connection reset")) {
		t.Error("a transport failure is not evidence of absence")
	}
}
