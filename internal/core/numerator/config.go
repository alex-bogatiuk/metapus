// Package numerator provides domain contracts for document auto-numbering.
package numerator

// Strategy defines the numbering generation strategy.
type Strategy int

const (
	// StrategyStrict uses UPDATE ... RETURNING for every number.
	// Guarantees sequential numbers without gaps.
	// Slower, suitable for invoices and accounting documents.
	StrategyStrict Strategy = iota

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
