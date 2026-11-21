package aml

import (
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestInventoryProjectionNeverLaundersUnknownOrHigh(t *testing.T) {
	now := time.Now()
	fresh := now.Add(time.Hour)
	expired := now.Add(-time.Second)
	for _, tc := range []struct {
		name          string
		items         []Contribution
		covered, high bool
		class         Class
	}{
		{"covered low", []Contribution{{Low, &fresh}}, true, false, Low},
		{"missing baseline", []Contribution{{Low, &fresh}}, false, false, Unknown},
		{"unassessed receipt", []Contribution{{Low, &fresh}, {Unknown, nil}}, true, false, Unknown},
		{"known high survives unknown", []Contribution{{Unknown, nil}}, false, true, High},
		{"high fact survives missing coverage", []Contribution{{High, &expired}}, false, false, High},
		{"expired low", []Contribution{{Low, &expired}}, true, false, Unknown},
		{"mixed class cannot average", []Contribution{{Low, &fresh}, {Medium, &fresh}}, true, false, Medium},
		{"empty wallet has no implied low", nil, true, false, Unknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := ProjectInventory(tc.items, tc.covered, tc.high, now)
			require.Equal(t, tc.class, result.Class)
		})
	}
}
