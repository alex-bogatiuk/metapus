package crypto

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name      string
		plaintext []byte
	}{
		{"empty", []byte{}},
		{"short", []byte("hello")},
		{"bot_token", []byte("123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11")},
		{"json_credentials", []byte(`{"smtp_password":"s3cret","api_key":"ak_live_xxx"}`)},
		{"binary", func() []byte {
			b := make([]byte, 1024)
			rand.Read(b) //nolint:errcheck
			return b
		}()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ct, err := Encrypt(tc.plaintext, key)
			if err != nil {
				t.Fatalf("Encrypt: %v", err)
			}

			// Ciphertext must differ from plaintext (unless empty)
			if len(tc.plaintext) > 0 && bytes.Equal(ct, tc.plaintext) {
				t.Fatal("ciphertext equals plaintext — encryption did nothing")
			}

			got, err := Decrypt(ct, key)
			if err != nil {
				t.Fatalf("Decrypt: %v", err)
			}

			if !bytes.Equal(got, tc.plaintext) {
				t.Fatalf("round-trip mismatch: got %q, want %q", got, tc.plaintext)
			}
		})
	}
}

func TestEncrypt_UniqueNonce(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key) //nolint:errcheck

	plaintext := []byte("same-data")

	ct1, _ := Encrypt(plaintext, key)
	ct2, _ := Encrypt(plaintext, key)

	if bytes.Equal(ct1, ct2) {
		t.Fatal("two encryptions of the same plaintext produced identical ciphertext (nonce reuse)")
	}
}

func TestDecrypt_WrongKey(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	rand.Read(key1) //nolint:errcheck
	rand.Read(key2) //nolint:errcheck

	ct, err := Encrypt([]byte("secret"), key1)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Decrypt(ct, key2)
	if err == nil {
		t.Fatal("Decrypt with wrong key should fail")
	}
}

func TestEncrypt_InvalidKeyLength(t *testing.T) {
	for _, klen := range []int{0, 16, 24, 31, 33, 64} {
		_, err := Encrypt([]byte("x"), make([]byte, klen))
		if err == nil {
			t.Fatalf("Encrypt should reject key length %d", klen)
		}
	}
}

func TestDecrypt_InvalidKeyLength(t *testing.T) {
	for _, klen := range []int{0, 16, 24, 31, 33, 64} {
		_, err := Decrypt([]byte("x"), make([]byte, klen))
		if err == nil {
			t.Fatalf("Decrypt should reject key length %d", klen)
		}
	}
}

func TestDecrypt_TooShortCiphertext(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key) //nolint:errcheck

	// GCM nonce is 12 bytes, so anything shorter than that should fail
	_, err := Decrypt([]byte("short"), key)
	if err == nil {
		t.Fatal("Decrypt should fail on too-short ciphertext")
	}
}

func TestDecrypt_CorruptedCiphertext(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key) //nolint:errcheck

	ct, _ := Encrypt([]byte("data"), key)

	// Flip a byte in the ciphertext body (after nonce)
	ct[len(ct)-1] ^= 0xFF

	_, err := Decrypt(ct, key)
	if err == nil {
		t.Fatal("Decrypt should detect corrupted ciphertext (GCM auth tag failure)")
	}
}
