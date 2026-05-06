package automation

import (
	"time"

	"metapus/internal/domain/automations"
)

// ResolvedPeriod holds the calculated date range for a report.
type ResolvedPeriod struct {
	From time.Time // Start of period (inclusive), midnight in the resolved timezone.
	To   time.Time // End of period (inclusive), midnight in the resolved timezone.
}

// ResolvePeriod calculates the actual date range from PeriodType and trigger time.
// loc is the timezone for calculation (from tenant settings or per-rule override).
// customDays is used only when pt == PeriodCustomDays.
//
// All returned times are at midnight (00:00:00) in the given location,
// representing full calendar days.
func ResolvePeriod(pt automations.PeriodType, triggerTime time.Time, loc *time.Location, customDays int) ResolvedPeriod {
	if loc == nil {
		loc = time.UTC
	}
	now := triggerTime.In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	switch pt {
	case automations.PeriodToday:
		return ResolvedPeriod{From: today, To: today}

	case automations.PeriodYesterday:
		y := today.AddDate(0, 0, -1)
		return ResolvedPeriod{From: y, To: y}

	case automations.PeriodCurrentWeek:
		weekday := int(today.Weekday())
		if weekday == 0 {
			weekday = 7 // Sunday = 7 (ISO 8601)
		}
		mon := today.AddDate(0, 0, -(weekday - 1))
		return ResolvedPeriod{From: mon, To: today}

	case automations.PeriodLastWeek:
		weekday := int(today.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		thisMon := today.AddDate(0, 0, -(weekday - 1))
		lastMon := thisMon.AddDate(0, 0, -7)
		lastSun := thisMon.AddDate(0, 0, -1)
		return ResolvedPeriod{From: lastMon, To: lastSun}

	case automations.PeriodCurrentMonth:
		first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
		return ResolvedPeriod{From: first, To: today}

	case automations.PeriodLastMonth:
		firstThisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
		firstLastMonth := firstThisMonth.AddDate(0, -1, 0)
		lastDayLastMonth := firstThisMonth.AddDate(0, 0, -1)
		return ResolvedPeriod{From: firstLastMonth, To: lastDayLastMonth}

	case automations.PeriodAsOfNow:
		// Point-in-time report (e.g. stock balance "as of now").
		// From == To == today, the executor uses this as the as_of_date parameter.
		return ResolvedPeriod{From: today, To: today}

	case automations.PeriodCustomDays:
		if customDays < 1 {
			customDays = 1
		}
		from := today.AddDate(0, 0, -customDays)
		return ResolvedPeriod{From: from, To: today}

	default:
		return ResolvedPeriod{From: today, To: today}
	}
}
