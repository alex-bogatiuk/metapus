package version

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

var migrationFile = regexp.MustCompile(`^(\d{5})_.+\.sql$`)

// deliverySchemaVersion is the highest migration version among names below
// FirstExtensionMigration: the schema version the delivery's migrations
// bring a tenant database to.
func deliverySchemaVersion(names []string) int {
	highest := 0
	for _, name := range names {
		match := migrationFile.FindStringSubmatch(name)
		if match == nil {
			continue
		}
		v, _ := strconv.Atoi(match[1])
		if v < FirstExtensionMigration && v > highest {
			highest = v
		}
	}
	return highest
}

func TestDeliverySchemaVersionIgnoresExtensionMigrations(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files []string
		want  int
	}{
		{"delivery only", []string{"00001_core.sql", "00009_crypto_custody.sql"}, 9},
		{"an extension migration keeps it", []string{"00001_core.sql", "00009_crypto_custody.sql", "10001_cat_products.sql", "10002_doc_receipts.sql"}, 9},
		{"a delivery migration raises it", []string{"00009_crypto_custody.sql", "00010_next.sql", "10001_cat_products.sql"}, 10},
		{"the first extension version is an extension's", []string{"00009_crypto_custody.sql", "10000_ext.sql"}, 9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := deliverySchemaVersion(tc.files); got != tc.want {
				t.Fatalf("deliverySchemaVersion(%v) = %d, want %d", tc.files, got, tc.want)
			}
		})
	}
}

func TestExpectedSchemaVersionMatchesMigrations(t *testing.T) {
	// Tenant migrations: the platform's and every configuration's, extension
	// modules' included (they live in db/configuration/<name>/ too).
	root := filepath.Join("..", "..", "..", "db")
	dirs, err := filepath.Glob(filepath.Join(root, "configuration", "*"))
	if err != nil {
		t.Fatalf("list configuration migrations: %v", err)
	}
	var names []string
	for _, dir := range append([]string{filepath.Join(root, "migrations")}, dirs...) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read migrations dir: %v", err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				names = append(names, entry.Name())
			}
		}
	}

	want := deliverySchemaVersion(names)
	if want == 0 {
		t.Fatalf("no versioned migration files found in %s", root)
	}
	if ExpectedSchemaVersion != want {
		t.Fatalf("ExpectedSchemaVersion = %d, want %d", ExpectedSchemaVersion, want)
	}
}
