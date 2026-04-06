package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
)

// ---------------------------------------------------------------------------
// 定数
// ---------------------------------------------------------------------------

const (
	pbkdf2Iterations = 600_000
	saltSize         = 32
	nonceSize        = 12
	keySize          = 32 // AES-256
)

// ---------------------------------------------------------------------------
// 暗号化基盤
// ---------------------------------------------------------------------------

func deriveKey(password string, salt []byte) []byte {
	// PBKDF2-HMAC-SHA256 を標準ライブラリで実装
	// RFC 2898 準拠
	prf := hmac.New(sha256.New, []byte(password))
	hashLen := prf.Size()
	numBlocks := (keySize + hashLen - 1) / hashLen

	var buf [4]byte
	dk := make([]byte, 0, numBlocks*hashLen)
	U := make([]byte, hashLen)
	for block := 1; block <= numBlocks; block++ {
		prf.Reset()
		prf.Write(salt)
		buf[0] = byte(block >> 24)
		buf[1] = byte(block >> 16)
		buf[2] = byte(block >> 8)
		buf[3] = byte(block)
		prf.Write(buf[:4])
		U = U[:0]
		U = prf.Sum(U)
		T := make([]byte, hashLen)
		copy(T, U)
		for n := 2; n <= pbkdf2Iterations; n++ {
			prf.Reset()
			prf.Write(U)
			U = U[:0]
			U = prf.Sum(U)
			for x := range T {
				T[x] ^= U[x]
			}
		}
		dk = append(dk, T...)
	}
	return dk[:keySize]
}

func encryptData(plaintext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ct := gcm.Seal(nil, nonce, plaintext, nil)
	return append(nonce, ct...), nil
}

func decryptData(blob, key []byte) ([]byte, error) {
	if len(blob) < nonceSize {
		return nil, errors.New(i18nMsg(MsgErrCiphertextTooShort))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, blob[:nonceSize], blob[nonceSize:], nil)
}
