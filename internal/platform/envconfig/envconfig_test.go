package envconfig

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReaderReturnsDefaultsForUnsetValues(t *testing.T) {
	r := New()

	require.Equal(t, "fallback", r.String("ENVCONFIG_TEST_UNSET", "fallback"))
	require.Equal(t, 7, r.Int("ENVCONFIG_TEST_UNSET", 7))
	require.Equal(t, int64(8), r.Int64("ENVCONFIG_TEST_UNSET", 8))
	require.Equal(t, 9, r.PositiveInt("ENVCONFIG_TEST_UNSET", 9))
	require.Equal(t, time.Minute, r.Duration("ENVCONFIG_TEST_UNSET", time.Minute))
	require.True(t, r.Bool("ENVCONFIG_TEST_UNSET", true))
	require.NoError(t, r.Err())
}

func TestReaderParsesValidValues(t *testing.T) {
	t.Setenv("ENVCONFIG_TEST_INT", " 42 ")
	t.Setenv("ENVCONFIG_TEST_ZERO", "0")
	t.Setenv("ENVCONFIG_TEST_DURATION", "5m")
	t.Setenv("ENVCONFIG_TEST_BOOL", "TRUE")
	r := New()

	require.Equal(t, 42, r.Int("ENVCONFIG_TEST_INT", 1))
	require.Equal(t, int64(42), r.Int64("ENVCONFIG_TEST_INT", 1))
	require.Equal(t, 0, r.NonNegativeInt("ENVCONFIG_TEST_ZERO", 168))
	require.Equal(t, 5*time.Minute, r.Duration("ENVCONFIG_TEST_DURATION", time.Second))
	require.True(t, r.Bool("ENVCONFIG_TEST_BOOL", false))
	require.NoError(t, r.Err())
}

func TestReaderRejectsInvalidValuesInsteadOfFallingBack(t *testing.T) {
	tests := []struct {
		name  string
		value string
		read  func(*Reader)
	}{
		{name: "int with trailing garbage", value: "10abc", read: func(r *Reader) { r.Int("ENVCONFIG_TEST_BAD", 1) }},
		{name: "int as duration", value: "5m", read: func(r *Reader) { r.Int64("ENVCONFIG_TEST_BAD", 1) }},
		{name: "zero positive int", value: "0", read: func(r *Reader) { r.PositiveInt("ENVCONFIG_TEST_BAD", 1) }},
		{name: "negative int", value: "-1", read: func(r *Reader) { r.NonNegativeInt("ENVCONFIG_TEST_BAD", 1) }},
		{name: "bare number duration", value: "300", read: func(r *Reader) { r.Duration("ENVCONFIG_TEST_BAD", time.Second) }},
		{name: "zero duration", value: "0s", read: func(r *Reader) { r.Duration("ENVCONFIG_TEST_BAD", time.Second) }},
		{name: "yes bool", value: "yes", read: func(r *Reader) { r.Bool("ENVCONFIG_TEST_BAD", false) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ENVCONFIG_TEST_BAD", tt.value)
			r := New()
			tt.read(r)
			require.ErrorContains(t, r.Err(), "ENVCONFIG_TEST_BAD")
		})
	}
}

func TestReaderReportsEveryErrorAndMissingRequiredValues(t *testing.T) {
	t.Setenv("ENVCONFIG_TEST_A", "x")
	t.Setenv("ENVCONFIG_TEST_B", "y")
	r := New()

	r.Int("ENVCONFIG_TEST_A", 0)
	r.Duration("ENVCONFIG_TEST_B", time.Second)
	r.Required("ENVCONFIG_TEST_MISSING")

	err := r.Err()
	require.ErrorContains(t, err, "ENVCONFIG_TEST_A")
	require.ErrorContains(t, err, "ENVCONFIG_TEST_B")
	require.ErrorContains(t, err, "ENVCONFIG_TEST_MISSING")
}

func TestReaderReadsSecretFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "value")
	require.NoError(t, os.WriteFile(path, []byte("25\n"), 0o600))
	t.Setenv("ENVCONFIG_TEST_FROM_FILE_FILE", path)
	r := New()

	require.Equal(t, 25, r.Int("ENVCONFIG_TEST_FROM_FILE", 1))
	require.NoError(t, r.Err())
}

func TestReaderRecordsConflictingSecretSources(t *testing.T) {
	t.Setenv("ENVCONFIG_TEST_BOTH", "a")
	t.Setenv("ENVCONFIG_TEST_BOTH_FILE", filepath.Join(t.TempDir(), "missing"))
	r := New()

	require.Equal(t, "fallback", r.String("ENVCONFIG_TEST_BOTH", "fallback"))
	require.ErrorContains(t, r.Err(), "cannot both be set")
}
