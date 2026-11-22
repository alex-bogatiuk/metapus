// Package secretbox seals stored credentials with the platform's encryption
// key: integration accounts, configuration provider accounts and automation
// seed data. The key (AUTOMATION_ENCRYPTION_KEY, 32 bytes, AES-256-GCM) is
// read only here and never leaves the package, so a configuration seals and
// opens credentials without ever holding the key.
package secretbox

import (
	"errors"
	"fmt"

	"metapus/internal/core/apperror"
	corecrypto "metapus/internal/core/crypto"
	"metapus/internal/platform/secretfile"
)

// keyName is the secret that holds the key, from the environment or a file.
const keyName = "AUTOMATION_ENCRYPTION_KEY"

// ErrNotConfigured is the cause of Ready's error when the key is not set,
// so a caller that may run without it (a demo seed) can tell that apart
// from a key of the wrong length.
var ErrNotConfigured = errors.New("integration encryption key is not configured")

// Ready reports whether credentials can be sealed and opened. The error is a
// validation error (400) asking for a configured 32-byte key, with
// ErrNotConfigured as its cause when the key is not set.
func Ready() error {
	_, err := key()
	return err
}

// Seal encrypts plaintext with the platform key.
func Seal(plaintext []byte) ([]byte, error) {
	k, err := key()
	if err != nil {
		return nil, err
	}
	sealed, err := corecrypto.Encrypt(plaintext, k)
	if err != nil {
		return nil, fmt.Errorf("seal credentials: %w", err)
	}
	return sealed, nil
}

// Open decrypts what Seal produced.
func Open(sealed []byte) ([]byte, error) {
	k, err := key()
	if err != nil {
		return nil, err
	}
	plaintext, err := corecrypto.Decrypt(sealed, k)
	if err != nil {
		return nil, fmt.Errorf("open credentials: %w", err)
	}
	return plaintext, nil
}

func key() ([]byte, error) {
	s, err := secretfile.Get(keyName, "")
	if err != nil {
		return nil, fmt.Errorf("load integrations encryption key: %w", err)
	}
	switch {
	case s == "":
		return nil, invalidKey().WithCause(ErrNotConfigured)
	case len(s) != 32:
		return nil, invalidKey()
	}
	return []byte(s), nil
}

func invalidKey() *apperror.AppError {
	return apperror.NewValidation("Для ключей интеграций необходим настроенный 32-байтовый ключ шифрования")
}
