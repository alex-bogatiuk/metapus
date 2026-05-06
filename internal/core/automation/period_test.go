package automation

import (
	"testing"
	"time"

	"metapus/internal/domain/automations"
)

func TestResolvePeriod(t *testing.T) {
	// 2025-04-14 is a Monday
	loc := time.UTC
	trigger := time.Date(2025, 4, 14, 10, 30, 0, 0, loc) // Monday 10:30

	tests := []struct {
		give       automations.PeriodType
		customDays int
		wantFrom   string
		wantTo     string
	}{
		{automations.PeriodToday, 0, "2025-04-14", "2025-04-14"},
		{automations.PeriodYesterday, 0, "2025-04-13", "2025-04-13"},
		{automations.PeriodCurrentWeek, 0, "2025-04-14", "2025-04-14"}, // Monday = start of week
		{automations.PeriodLastWeek, 0, "2025-04-07", "2025-04-13"},
		{automations.PeriodCurrentMonth, 0, "2025-04-01", "2025-04-14"},
		{automations.PeriodLastMonth, 0, "2025-03-01", "2025-03-31"},
		{automations.PeriodAsOfNow, 0, "2025-04-14", "2025-04-14"},
		{automations.PeriodCustomDays, 7, "2025-04-07", "2025-04-14"},
		{automations.PeriodCustomDays, 1, "2025-04-13", "2025-04-14"},
	}

	for _, tt := range tests {
		t.Run(string(tt.give), func(t *testing.T) {
			got := ResolvePeriod(tt.give, trigger, loc, tt.customDays)
			from := got.From.Format("2006-01-02")
			to := got.To.Format("2006-01-02")

			if from != tt.wantFrom {
				t.Errorf("From = %q, want %q", from, tt.wantFrom)
			}
			if to != tt.wantTo {
				t.Errorf("To = %q, want %q", to, tt.wantTo)
			}
		})
	}
}

func TestResolvePeriod_Wednesday(t *testing.T) {
	// 2025-04-16 is a Wednesday
	loc := time.UTC
	trigger := time.Date(2025, 4, 16, 8, 0, 0, 0, loc)

	tests := []struct {
		give     automations.PeriodType
		wantFrom string
		wantTo   string
	}{
		{automations.PeriodCurrentWeek, "2025-04-14", "2025-04-16"}, // Mon-Wed
		{automations.PeriodLastWeek, "2025-04-07", "2025-04-13"},    // previous Mon-Sun
	}

	for _, tt := range tests {
		t.Run(string(tt.give), func(t *testing.T) {
			got := ResolvePeriod(tt.give, trigger, loc, 0)
			from := got.From.Format("2006-01-02")
			to := got.To.Format("2006-01-02")

			if from != tt.wantFrom {
				t.Errorf("From = %q, want %q", from, tt.wantFrom)
			}
			if to != tt.wantTo {
				t.Errorf("To = %q, want %q", to, tt.wantTo)
			}
		})
	}
}

func TestResolvePeriod_Sunday(t *testing.T) {
	// 2025-04-20 is a Sunday
	loc := time.UTC
	trigger := time.Date(2025, 4, 20, 23, 59, 0, 0, loc)

	got := ResolvePeriod(automations.PeriodCurrentWeek, trigger, loc, 0)
	from := got.From.Format("2006-01-02")
	to := got.To.Format("2006-01-02")

	if from != "2025-04-14" {
		t.Errorf("Sunday current_week From = %q, want 2025-04-14 (Monday)", from)
	}
	if to != "2025-04-20" {
		t.Errorf("Sunday current_week To = %q, want 2025-04-20", to)
	}
}

func TestResolvePeriod_NilLocation(t *testing.T) {
	trigger := time.Date(2025, 4, 14, 10, 0, 0, 0, time.UTC)
	got := ResolvePeriod(automations.PeriodToday, trigger, nil, 0)

	if got.From.Location() != time.UTC {
		t.Errorf("nil location should default to UTC, got %v", got.From.Location())
	}
}
