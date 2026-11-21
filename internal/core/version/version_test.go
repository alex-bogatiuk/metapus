package version

import (
	"testing"

	dbassets "metapus/db"
)

func TestExpectedSchemaVersionMatchesPlatformMigrations(t *testing.T) {
	platform, err := dbassets.PlatformMigrations()
	if err != nil {
		t.Fatalf("platform migrations: %v", err)
	}
	want, err := dbassets.HighestVersion(platform)
	if err != nil {
		t.Fatalf("platform schema version: %v", err)
	}
	if want == 0 {
		t.Fatal("no platform migrations found")
	}
	if ExpectedSchemaVersion != want {
		t.Fatalf("ExpectedSchemaVersion = %d, want %d", ExpectedSchemaVersion, want)
	}
}
