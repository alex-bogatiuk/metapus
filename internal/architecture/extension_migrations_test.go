package architecture_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestExtensionMigrationsAreEmbedded keeps extension SQL out of extensions/.
// Only db/migrations/ and db/configuration/<name>/ are embedded into the
// binaries (dbassets.TenantMigrations); the images ship without the source
// tree, so a migration under extensions/<name>/migrations/ would compile its
// module into the server while its tables and permissions never get created.
func TestExtensionMigrationsAreEmbedded(t *testing.T) {
	t.Parallel()
	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(currentFile)))

	stray, err := filepath.Glob(filepath.Join(repoRoot, "extensions", "*", "migrations", "*.sql"))
	require.NoError(t, err)
	require.Emptyf(t, stray, "move extension migrations to db/configuration/<name>/ so they are embedded and applied")
}
