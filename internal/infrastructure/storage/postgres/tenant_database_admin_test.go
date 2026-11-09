package postgres

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateDatabaseIdentifier(t *testing.T) {
	require.NoError(t, validateDatabaseIdentifier("tenant-with-hyphen"))
	require.Error(t, validateDatabaseIdentifier(""))
	require.Error(t, validateDatabaseIdentifier(strings.Repeat("a", 64)))
	require.Error(t, validateDatabaseIdentifier("tenant\x00name"))
}
