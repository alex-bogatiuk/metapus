// Package envconfig reads typed process configuration from environment
// variables or their <NAME>_FILE secret files (see secretfile).
//
// A Reader never exits the process and never falls back silently: an unset
// variable yields its default, but a value that is set and cannot be parsed is
// recorded as an error. Callers read every value first and then check Err, so
// a misconfigured deployment reports all of its bad values in one start.
package envconfig

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"metapus/internal/platform/secretfile"
)

// Reader accumulates configuration errors across reads.
type Reader struct {
	errs []error
}

// New returns an empty Reader.
func New() *Reader { return &Reader{} }

// Err joins every recorded error, or returns nil when all values were valid.
func (r *Reader) Err() error { return errors.Join(r.errs...) }

// Fail records a validation error for key that the caller detected itself.
func (r *Reader) Fail(key string, err error) {
	r.errs = append(r.errs, fmt.Errorf("%s: %w", key, err))
}

// lookup returns the raw value and whether it was set to a non-empty value.
func (r *Reader) lookup(key string) (string, bool) {
	value, _, err := secretfile.Lookup(key)
	if err != nil {
		r.errs = append(r.errs, err)
		return "", false
	}
	return value, value != ""
}

// String returns the value of key, or fallback when it is unset or empty.
func (r *Reader) String(key, fallback string) string {
	if value, ok := r.lookup(key); ok {
		return value
	}
	return fallback
}

// Required returns the value of key and records an error when it is unset or
// empty.
func (r *Reader) Required(key string) string {
	value, ok := r.lookup(key)
	if !ok {
		r.errs = append(r.errs, fmt.Errorf("required environment variable %s is not set", key))
	}
	return value
}

// Int returns key parsed as a base-10 int, or fallback when it is unset.
func (r *Reader) Int(key string, fallback int) int {
	value, ok := r.lookup(key)
	if !ok {
		return fallback
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		r.Fail(key, fmt.Errorf("must be an integer, got %q", value))
		return fallback
	}
	return parsed
}

// Int64 returns key parsed as a base-10 int64, or fallback when it is unset.
func (r *Reader) Int64(key string, fallback int64) int64 {
	value, ok := r.lookup(key)
	if !ok {
		return fallback
	}
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		r.Fail(key, fmt.Errorf("must be an integer, got %q", value))
		return fallback
	}
	return parsed
}

// PositiveInt is Int that also rejects zero and negative values.
func (r *Reader) PositiveInt(key string, fallback int) int {
	parsed := r.Int(key, fallback)
	if parsed <= 0 {
		r.Fail(key, fmt.Errorf("must be positive, got %d", parsed))
		return fallback
	}
	return parsed
}

// NonNegativeInt is Int that also rejects negative values.
func (r *Reader) NonNegativeInt(key string, fallback int) int {
	parsed := r.Int(key, fallback)
	if parsed < 0 {
		r.Fail(key, fmt.Errorf("must not be negative, got %d", parsed))
		return fallback
	}
	return parsed
}

// Duration returns key parsed by time.ParseDuration ("30s", "5m"), or
// fallback when it is unset. Zero and negative durations are rejected.
func (r *Reader) Duration(key string, fallback time.Duration) time.Duration {
	value, ok := r.lookup(key)
	if !ok {
		return fallback
	}
	parsed, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		r.Fail(key, fmt.Errorf("must be a duration such as 30s or 5m, got %q", value))
		return fallback
	}
	if parsed <= 0 {
		r.Fail(key, fmt.Errorf("must be positive, got %s", parsed))
		return fallback
	}
	return parsed
}

// Bool returns key parsed by strconv.ParseBool, or fallback when it is unset.
func (r *Reader) Bool(key string, fallback bool) bool {
	value, ok := r.lookup(key)
	if !ok {
		return fallback
	}
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		r.Fail(key, fmt.Errorf("must be true or false, got %q", value))
		return fallback
	}
	return parsed
}
