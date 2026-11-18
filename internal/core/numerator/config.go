// Package numerator provides domain contracts for document auto-numbering.
package numerator

import (
	"fmt"
	"time"

	"metapus/internal/core/entity"
)

// Strategy defines the numbering generation strategy.
type Strategy int

const (
	// StrategyStrict uses UPDATE ... RETURNING for every number.
	// Guarantees sequential numbers without gaps.
	// Slower, suitable for invoices and accounting documents.
	StrategyStrict Strategy = iota + 1

	// StrategyCached allocates ranges of numbers in memory.
	// Much faster, but may produce gaps if application restarts.
	// Suitable for internal documents (orders, shipments).
	StrategyCached
)

// Options configuration for number generation.
type Options struct {
	// Strategy to use for number generation
	Strategy Strategy
	// RangeSize is the number of IDs to allocate at once in Cached strategy.
	// Default is 50.
	RangeSize int64
}

// DefaultOptions returns standard options (Strict).
func DefaultOptions() *Options {
	return &Options{
		Strategy: StrategyStrict,
	}
}

// Config holds numbering configuration.
type Config struct {
	// Key identifies the counter when set: a stable series key such as
	// "catalog.organization", so that changing Prefix keeps the sequence.
	// Without it the counter is keyed by Prefix.
	Key string

	// Prefix added to all numbers (e.g., "INV", "GR")
	Prefix string

	// IncludeYear adds year to the number
	IncludeYear bool

	// PadWidth is the minimum number width (default 5)
	PadWidth int

	// ResetPeriod: "year", "month", "never"
	ResetPeriod string
}

// DefaultConfig returns sensible defaults.
func DefaultConfig(prefix string) Config {
	return Config{
		Prefix:      prefix,
		IncludeYear: true,
		PadWidth:    5,
		ResetPeriod: "year",
	}
}

// SequenceKey is the counter of the series for the period: Key (or Prefix
// without a Key) followed by the year or month of a periodic series.
func (c Config) SequenceKey(period time.Time) string {
	base := c.Key
	if base == "" {
		base = c.Prefix
	}
	switch c.ResetPeriod {
	case "month":
		return fmt.Sprintf("%s_%s", base, period.Format("2006_01"))
	case "year":
		return fmt.Sprintf("%s_%s", base, period.Format("2006"))
	default:
		return base
	}
}

// Format renders the n-th number of the series: PREFIX-00001, or
// PREFIX-YEAR-00001 with IncludeYear.
func (c Config) Format(period time.Time, n int64) string {
	width := c.PadWidth
	if width == 0 {
		width = 5
	}
	if c.IncludeYear {
		return fmt.Sprintf("%s-%s-%0*d", c.Prefix, period.Format("2006"), width, n)
	}
	return fmt.Sprintf("%s-%0*d", c.Prefix, width, n)
}

// CatalogSeries is the numerator configuration of a catalog code series: a
// never-resetting counter keyed by the series, not by its prefix.
func CatalogSeries(series entity.CodeSeries) Config {
	return Config{Key: series.Key, Prefix: series.Prefix, PadWidth: series.Width, ResetPeriod: "never"}
}
