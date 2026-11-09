package id

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/stretchr/testify/require"
)

func TestCompactPreservesCompleteUUID(t *testing.T) {
	value := MustParse("0198c5a6-7b8c-7def-8123-456789abcdef")

	require.Equal(t, "0198c5a67b8c7def8123456789abcdef", Compact(value))
}

type failedEntropy struct{}

func (failedEntropy) Read([]byte) (int, error) { return 0, errors.New("entropy unavailable") }

func TestNewAlwaysUsesV7AndFailsWithoutEntropy(t *testing.T) {
	require.Equal(t, uuid.Version(7), New().Version())
	uuid.SetRand(failedEntropy{})
	defer uuid.SetRand(nil)
	require.Panics(t, func() { New() })
}
