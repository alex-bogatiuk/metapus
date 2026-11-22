// Package encryption encrypts secrets at rest with the platform's keys.
package encryption

import (
	core "metapus/internal/core/crypto"
)

// Decrypt decrypts ciphertext produced by Encrypt using AES-256-GCM.
// Expects input format: nonce || ciphertext.
func Decrypt(ciphertext []byte, key []byte) ([]byte, error) { return core.Decrypt(ciphertext, key) }

// Encrypt encrypts plaintext using AES-256-GCM.
// The nonce is prepended to the ciphertext. Output format: nonce || ciphertext.
func Encrypt(plaintext []byte, key []byte) ([]byte, error) { return core.Encrypt(plaintext, key) }
