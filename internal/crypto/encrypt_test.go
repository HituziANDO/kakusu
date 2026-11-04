package crypto

import (
	"bytes"
	"crypto/rand"
	"testing"
)

const gcmTagSize = 16

// randomKey returns a new AES-256 key. crypto/rand.Read never returns an error.
func randomKey() []byte {
	key := make([]byte, KeySize)
	rand.Read(key)
	return key
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := randomKey()
	plaintext := []byte(`{"app":{"db_password":"dummy"}}`)

	blob, err := EncryptData(plaintext, key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecryptData(blob, key)
	if err != nil {
		t.Fatalf("DecryptData: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("DecryptData = %q, want %q", got, plaintext)
	}
}

func TestEncryptUsesNewNonceEachTime(t *testing.T) {
	key := randomKey()
	plaintext := []byte("dummy-secret")

	a, err := EncryptData(plaintext, key)
	if err != nil {
		t.Fatal(err)
	}
	b, err := EncryptData(plaintext, key)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a[:NonceSize], b[:NonceSize]) {
		t.Errorf("two EncryptData calls used the same nonce %x", a[:NonceSize])
	}
}

func TestDecryptFailsOnAnyFlippedByte(t *testing.T) {
	key := randomKey()
	blob, err := EncryptData([]byte("dummy-secret"), key)
	if err != nil {
		t.Fatal(err)
	}
	tagStart := len(blob) - gcmTagSize
	for i := range blob {
		part := "ciphertext"
		switch {
		case i < NonceSize:
			part = "nonce"
		case i >= tagStart:
			part = "tag"
		}
		tampered := bytes.Clone(blob)
		tampered[i] ^= 0x01
		if _, err := DecryptData(tampered, key); err == nil {
			t.Errorf("DecryptData succeeded with %s byte %d flipped", part, i)
		}
	}
}

func TestDecryptFailsWithWrongKey(t *testing.T) {
	blob, err := EncryptData([]byte("dummy-secret"), randomKey())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecryptData(blob, randomKey()); err == nil {
		t.Error("DecryptData succeeded with the wrong key")
	}
}

func TestDecryptRejectsBlobShorterThanNonce(t *testing.T) {
	key := randomKey()
	for n := 0; n < NonceSize; n++ {
		if _, err := DecryptData(make([]byte, n), key); err == nil {
			t.Errorf("DecryptData succeeded with a %d-byte blob", n)
		}
	}
}
