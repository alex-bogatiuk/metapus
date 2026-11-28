package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Every configuration of the repository generates the same platform files
// and its own files under frontend/configuration/<name>/ (the add-on
// automotive's: its extensions of the base's catalogs); the platform alone,
// none.
func TestEveryConfigurationGeneratesTheSamePlatformFiles(t *testing.T) {
	t.Parallel()
	files, err := artifacts(configurations())
	require.NoError(t, err)
	paths := map[string]bool{}
	for _, file := range files.list {
		require.Falsef(t, paths[file.Path], "%s generated twice", file.Path)
		paths[file.Path] = true
	}
	for _, cfg := range configurations() {
		own := 0
		for path := range paths {
			if strings.HasPrefix(path, "frontend/configuration/"+cfg.Name()+"/") {
				own++
			}
		}
		if cfg.Name() == "none" {
			require.Zero(t, own, cfg.Name())
		} else {
			require.Equalf(t, 3, own, "%s's own files", cfg.Name())
		}
	}
}
