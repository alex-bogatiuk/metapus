package version

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

func TestExpectedSchemaVersionMatchesMigrations(t *testing.T) {
	// Tenant migrations: the platform's and every configuration's.
	root := filepath.Join("..", "..", "..", "db")
	dirs, err := filepath.Glob(filepath.Join(root, "configuration", "*"))
	if err != nil {
		t.Fatalf("list configuration migrations: %v", err)
	}
	var entries []os.DirEntry
	for _, dir := range append([]string{filepath.Join(root, "migrations")}, dirs...) {
		dirEntries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read migrations dir: %v", err)
		}
		entries = append(entries, dirEntries...)
	}

	re := regexp.MustCompile(`^(\d{5})_.+\.sql$`)
	maxVersion := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		match := re.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}
		version, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("parse migration version %q: %v", entry.Name(), err)
		}
		if version > maxVersion {
			maxVersion = version
		}
	}

	if maxVersion == 0 {
		t.Fatalf("no versioned migration files found in %s", root)
	}
	if ExpectedSchemaVersion != maxVersion {
		t.Fatalf("ExpectedSchemaVersion = %d, want %d", ExpectedSchemaVersion, maxVersion)
	}
}
