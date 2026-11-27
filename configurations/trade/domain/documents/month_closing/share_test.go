package month_closing

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/entity"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

func TestShareRoundsHalfAwayFromZeroAndKeepsTheWhole(t *testing.T) {
	t.Parallel()
	q := func(n int64) types.Quantity { return types.Quantity(n * types.QuantityScale) }
	require.Equal(t, types.MinorUnits(170455), share(250000, q(15), q(22)), "170 454.5… rounds up")
	require.Equal(t, types.MinorUnits(128788), share(154545, q(10), q(12)), "a half rounds away from zero")
	require.Equal(t, types.MinorUnits(-128788), share(-154545, q(10), q(12)))
	require.Equal(t, types.MinorUnits(154545), share(154545, q(12), q(12)), "the whole stock takes the whole cost")
	require.Zero(t, share(0, q(1), q(3)))
	// Large amounts and quantities do not overflow.
	require.Equal(t, types.MinorUnits(500_000_000_000_000), share(1_000_000_000_000_000, q(500_000), q(1_000_000)))
}

func TestTheClosingClosesTheCalendarMonthOfItsDate(t *testing.T) {
	t.Parallel()
	closing := MonthClosing{Document: entity.Document{Date: time.Date(2026, 2, 14, 18, 0, 0, 0, time.FixedZone("MSK", 3*3600))}}
	from, to := closing.SequencePeriod()
	require.Equal(t, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), from)
	require.Equal(t, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), to)
	require.NoError(t, Definition.Validate())
}
