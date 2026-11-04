package crypto

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// Vault files written by released versions decrypt only with the key
// DeriveKey returned when they were written, so these two tests pin it.

func TestDeriveKeyGolden(t *testing.T) {
	salt := make([]byte, 32)
	for i := range salt {
		salt[i] = byte(i)
	}
	// Python: hashlib.pbkdf2_hmac("sha256", b"kakusu-golden-password", bytes(range(32)), 600000, 32).hex()
	const want = "9b5cb87537c8c43ae1e8eb0ab3aab0cff83ac84e3f1eff9eb15b2ee5eb321551"

	key, err := DeriveKey("kakusu-golden-password", salt)
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(key); got != want {
		t.Errorf("DeriveKey = %s, want %s", got, want)
	}
}

// testdata/secrets-v0.5.0.enc was written by kakusu v0.5.0 with
// `init` (password "golden-fixture-pw"), `set app/db_password dummy-db-pass`
// and `set token dummy-token`. v0.4.1 writes the same format:
// salt ‖ nonce ‖ AES-256-GCM ciphertext and tag.
func TestDecryptReleasedVault(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "secrets-v0.5.0.enc"))
	if err != nil {
		t.Fatal(err)
	}
	salt, blob := raw[:SaltSize], raw[SaltSize:]
	const want = `{"app":{"db_password":"dummy-db-pass"},"default":{"token":"dummy-token"}}`

	key, err := DeriveKey("golden-fixture-pw", salt)
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := DecryptData(blob, key)
	if err != nil {
		t.Fatalf("DecryptData: %v", err)
	}
	if string(plaintext) != want {
		t.Errorf("plaintext = %s, want %s", plaintext, want)
	}
}
