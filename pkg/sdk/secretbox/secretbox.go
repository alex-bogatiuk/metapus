// Package secretbox seals stored credentials with the platform's encryption
// key. The key never leaves the platform: a configuration seals and opens its
// providers' credentials here instead of reading AUTOMATION_ENCRYPTION_KEY.
package secretbox

import (
	core "metapus/internal/infrastructure/secretbox"
)

// ErrNotConfigured is the cause of Ready's error when the key is not set.
var ErrNotConfigured = core.ErrNotConfigured

// Ready reports whether credentials can be sealed and opened.
func Ready() error { return core.Ready() }

// Seal encrypts plaintext with the platform key.
func Seal(plaintext []byte) ([]byte, error) { return core.Seal(plaintext) }

// Open decrypts what Seal produced.
func Open(sealed []byte) ([]byte, error) { return core.Open(sealed) }
