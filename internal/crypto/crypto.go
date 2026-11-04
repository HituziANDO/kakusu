package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
)

const (
	Pbkdf2Iterations = 600_000
	SaltSize         = 32
	NonceSize        = 12
	KeySize          = 32 // AES-256
)

// DeriveKey derives an AES-256 key using PBKDF2-HMAC-SHA256 (RFC 2898).
func DeriveKey(password string, salt []byte) ([]byte, error) {
	return pbkdf2.Key(sha256.New, password, salt, Pbkdf2Iterations, KeySize)
}

func EncryptData(plaintext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, NonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ct := gcm.Seal(nil, nonce, plaintext, nil)
	return append(nonce, ct...), nil
}

func DecryptData(blob, key []byte) ([]byte, error) {
	if len(blob) < NonceSize {
		return nil, errors.New("ciphertext too short")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, blob[:NonceSize], blob[NonceSize:], nil)
}
