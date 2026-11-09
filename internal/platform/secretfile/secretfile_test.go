package secretfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLookupReadsFileAndRemovesOnlyLineEnding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte(" value with spaces \r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_SECRET_FILE", path)

	got, set, err := Lookup("TEST_SECRET")
	if err != nil {
		t.Fatal(err)
	}
	if !set {
		t.Fatal("expected value to be configured")
	}
	if got != " value with spaces " {
		t.Fatalf("got %q", got)
	}
}

func TestLookupRejectsAmbiguousSources(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("from-file"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_SECRET", "from-env")
	t.Setenv("TEST_SECRET_FILE", path)

	_, _, err := Lookup("TEST_SECRET")
	if err == nil || !strings.Contains(err.Error(), "cannot both be set") {
		t.Fatalf("expected ambiguity error, got %v", err)
	}
}

func TestLookupRejectsOversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, make([]byte, maxSecretBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_SECRET_FILE", path)

	_, _, err := Lookup("TEST_SECRET")
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected size error, got %v", err)
	}
}

func TestGetUsesFallbackForEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_SECRET_FILE", path)

	got, err := Get("TEST_SECRET", "fallback")
	if err != nil {
		t.Fatal(err)
	}
	if got != "fallback" {
		t.Fatalf("got %q", got)
	}
}
