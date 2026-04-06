package vault

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/HituziANDO/kakusu/internal/agent"
	"github.com/HituziANDO/kakusu/internal/config"
	"github.com/HituziANDO/kakusu/internal/crypto"
	"github.com/HituziANDO/kakusu/internal/i18n"
)

// Data is the in-memory representation of stored secrets: map[group]map[key]value.
type Data map[string]map[string]string

// State holds the decrypted vault contents together with the derived key and salt.
type State struct {
	Data Data
	Key  []byte
	Salt []byte
}

// LoadKakusu loads (or creates) the vault.
// Password prompters are injected to avoid a dependency on the UI layer.
func LoadKakusu(password string, allowCreate bool, promptPw func(string) (string, error), promptNewPw func() (string, error)) (*State, error) {
	path := config.KakusuFile()
	noAgent := config.AgentDisabled()

	// File does not exist — create a new vault.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if !allowCreate {
			return nil, errors.New(i18n.Msg(i18n.MsgErrVaultNotFound))
		}
		if password == "" {
			var err error
			password, err = promptNewPw()
			if err != nil {
				return nil, err
			}
		}
		salt := make([]byte, crypto.SaltSize)
		if _, err := io.ReadFull(rand.Reader, salt); err != nil {
			return nil, err
		}
		key := crypto.DeriveKey(password, salt)
		if !noAgent {
			if err := agent.EnsureAgent(); err == nil {
				agent.SetKey(key, salt)
			}
		}
		return &State{
			Data: make(Data),
			Key:  key,
			Salt: salt,
		}, nil
	}

	// File exists — decrypt.
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) < crypto.SaltSize {
		return nil, errors.New(i18n.Msg(i18n.MsgErrFileCorrupted))
	}

	salt := raw[:crypto.SaltSize]
	blob := raw[crypto.SaltSize:]

	// Try the agent cache first.
	if password == "" && !noAgent {
		if err := agent.EnsureAgent(); err == nil {
			if aKey, aSalt, err := agent.GetKey(); err == nil && aKey != nil {
				if bytes.Equal(aSalt, salt) {
					if plaintext, err := crypto.DecryptData(blob, aKey); err == nil {
						var data Data
						if err := json.Unmarshal(plaintext, &data); err != nil {
							return nil, fmt.Errorf(i18n.Msg(i18n.MsgErrJSONParse), err)
						}
						return &State{Data: data, Key: aKey, Salt: salt}, nil
					}
				}
				agent.ClearKey()
			}
		}
	}

	// Prompt for password.
	if password == "" {
		password, err = promptPw(i18n.Msg(i18n.MsgPromptMasterPassword))
		if err != nil {
			return nil, err
		}
	}

	key := crypto.DeriveKey(password, salt)
	plaintext, err := crypto.DecryptData(blob, key)
	if err != nil {
		return nil, errors.New(i18n.Msg(i18n.MsgErrPasswordWrong))
	}

	var data Data
	if err := json.Unmarshal(plaintext, &data); err != nil {
		return nil, fmt.Errorf(i18n.Msg(i18n.MsgErrJSONParse), err)
	}

	if !noAgent {
		agent.SetKey(key, salt)
	}

	return &State{Data: data, Key: key, Salt: salt}, nil
}

// Save writes the vault atomically to disk.
func (s *State) Save() error {
	dir := config.KakusuDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	plaintext, err := json.Marshal(s.Data)
	if err != nil {
		return err
	}
	blob, err := crypto.EncryptData(plaintext, s.Key)
	if err != nil {
		return err
	}
	content := append(s.Salt, blob...)
	path := config.KakusuFile()

	tmpFile, err := os.CreateTemp(dir, ".secrets.enc.tmp.*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(content); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, 0600); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// ParseRef splits a "group/key" or "kks://group/key" string.
func ParseRef(ref string) (group, key string) {
	ref = strings.TrimPrefix(ref, "kks://")
	if idx := strings.Index(ref, "/"); idx >= 0 {
		return ref[:idx], ref[idx+1:]
	}
	return "default", ref
}

func GetSecret(data Data, group, key string) (string, bool) {
	g, ok := data[group]
	if !ok {
		return "", false
	}
	v, ok := g[key]
	return v, ok
}

func SetSecret(data Data, group, key, value string) {
	if _, ok := data[group]; !ok {
		data[group] = make(map[string]string)
	}
	data[group][key] = value
}

func DeleteSecret(data Data, group, key string) bool {
	g, ok := data[group]
	if !ok {
		return false
	}
	if _, ok := g[key]; !ok {
		return false
	}
	delete(g, key)
	if len(g) == 0 {
		delete(data, group)
	}
	return true
}
