// Package secretfile resolves configuration values from either an environment
// variable or a file named by the corresponding <NAME>_FILE variable.
//
// File-backed values are intended for Docker/Podman secrets. A caller must not
// configure both forms because doing so makes secret rotation ambiguous.
package secretfile

import (
	"fmt"
	"os"
	"strings"
)

const maxSecretBytes = 1 << 20

// Lookup returns a configured value and whether either NAME or NAME_FILE was
// present. One trailing newline sequence is removed from file-backed values so
// files created by standard CLI tools can be mounted directly.
func Lookup(name string) (string, bool, error) {
	value, valueSet := os.LookupEnv(name)
	fileName, fileSet := os.LookupEnv(name + "_FILE")
	fileName = strings.TrimSpace(fileName)

	if valueSet && value != "" && fileSet && fileName != "" {
		return "", true, fmt.Errorf("%s and %s_FILE cannot both be set", name, name)
	}
	if !fileSet || fileName == "" {
		return value, valueSet, nil
	}

	info, err := os.Stat(fileName)
	if err != nil {
		return "", true, fmt.Errorf("stat %s_FILE: %w", name, err)
	}
	if info.Size() > maxSecretBytes {
		return "", true, fmt.Errorf("%s_FILE exceeds %d bytes", name, maxSecretBytes)
	}
	raw, err := os.ReadFile(fileName)
	if err != nil {
		return "", true, fmt.Errorf("read %s_FILE: %w", name, err)
	}
	value = strings.TrimSuffix(string(raw), "\n")
	value = strings.TrimSuffix(value, "\r")
	if strings.IndexByte(value, 0) >= 0 {
		return "", true, fmt.Errorf("%s_FILE contains a NUL byte", name)
	}
	return value, true, nil
}

// Get returns a value or fallback when neither NAME nor NAME_FILE is set, or
// when the selected source contains an empty value.
func Get(name, fallback string) (string, error) {
	value, _, err := Lookup(name)
	if err != nil {
		return "", err
	}
	if value == "" {
		return fallback, nil
	}
	return value, nil
}
