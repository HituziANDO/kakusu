package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// 定数
// ---------------------------------------------------------------------------

const (
	version    = "0.4.0"
	defaultTTL = 30 * time.Minute
)

var kakusuRefRe = regexp.MustCompile(`^kks://([^/\s]+)/([^\s]+)$`)

// ---------------------------------------------------------------------------
// ファイルパス解決
// ---------------------------------------------------------------------------

func kakusuFile() string {
	if v := os.Getenv("KAKUSU_FILE"); v != "" {
		if strings.HasPrefix(v, "~/") {
			home, _ := os.UserHomeDir()
			return filepath.Join(home, v[2:])
		}
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".kakusu", "secrets.enc")
}

func kakusuDir() string {
	return filepath.Dir(kakusuFile())
}

// kakusuHome は ~/.kakusu を返す（KAKUSU_FILE に依存しない）
func kakusuHome() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".kakusu")
}

func agentSocketPath() string {
	return filepath.Join(kakusuHome(), "agent.sock")
}

func agentPIDPath() string {
	return filepath.Join(kakusuHome(), "agent.pid")
}

// ---------------------------------------------------------------------------
// Kakusu ファイル I/O
// ストレージ構造: map[group]map[key]value (JSON)
// ファイル構造:   salt(32B) | nonce(12B) | ciphertext+tag
// ---------------------------------------------------------------------------

type kakusuData map[string]map[string]string

type kakusuState struct {
	data kakusuData
	key  []byte
	salt []byte
}

func loadKakusu(password string, allowCreate bool) (*kakusuState, error) {
	path := kakusuFile()
	noAgent := agentDisabled()

	// ファイルが存在しない場合
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if !allowCreate {
			return nil, errors.New(i18nMsg(MsgErrVaultNotFound))
		}
		// 新規作成
		if password == "" {
			var err error
			password, err = promptNewPassword()
			if err != nil {
				return nil, err
			}
		}
		salt := make([]byte, saltSize)
		if _, err := io.ReadFull(rand.Reader, salt); err != nil {
			return nil, err
		}
		key := deriveKey(password, salt)
		if !noAgent {
			if err := ensureAgent(); err == nil {
				agentSetKey(key, salt)
			}
		}
		return &kakusuState{
			data: make(kakusuData),
			key:  key,
			salt: salt,
		}, nil
	}

	// ファイルが存在する場合 → 復号
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) < saltSize {
		return nil, errors.New(i18nMsg(MsgErrFileCorrupted))
	}

	salt := raw[:saltSize]
	blob := raw[saltSize:]

	// エージェントから鍵を取得（有効であればパスワード入力不要）
	if password == "" && !noAgent {
		if err := ensureAgent(); err == nil {
			if aKey, aSalt, err := agentGetKey(); err == nil && aKey != nil {
				if bytes.Equal(aSalt, salt) {
					if plaintext, err := decryptData(blob, aKey); err == nil {
						var data kakusuData
						if err := json.Unmarshal(plaintext, &data); err != nil {
							return nil, fmt.Errorf(i18nMsg(MsgErrJSONParse), err)
						}
						return &kakusuState{data: data, key: aKey, salt: salt}, nil
					}
				}
				// salt不一致 or 復号失敗 → キャッシュが古い
				agentClearKey()
			}
		}
	}

	// パスワードをプロンプトで入力
	if password == "" {
		password, err = promptPassword(i18nMsg(MsgPromptMasterPassword))
		if err != nil {
			return nil, err
		}
	}

	key := deriveKey(password, salt)
	plaintext, err := decryptData(blob, key)
	if err != nil {
		return nil, errors.New(i18nMsg(MsgErrPasswordWrong))
	}

	var data kakusuData
	if err := json.Unmarshal(plaintext, &data); err != nil {
		return nil, fmt.Errorf(i18nMsg(MsgErrJSONParse), err)
	}

	// エージェントに鍵を登録
	if !noAgent {
		agentSetKey(key, salt)
	}

	return &kakusuState{data: data, key: key, salt: salt}, nil
}

func (s *kakusuState) save() error {
	dir := kakusuDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	plaintext, err := json.Marshal(s.data)
	if err != nil {
		return err
	}
	blob, err := encryptData(plaintext, s.key)
	if err != nil {
		return err
	}
	content := append(s.salt, blob...)
	path := kakusuFile()

	// Atomic write: 一時ファイル → fsync → rename
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

// ---------------------------------------------------------------------------
// group/key ヘルパー
// ---------------------------------------------------------------------------

func parseRef(ref string) (group, key string) {
	ref = strings.TrimPrefix(ref, "kks://")
	if idx := strings.Index(ref, "/"); idx >= 0 {
		return ref[:idx], ref[idx+1:]
	}
	return "default", ref
}

func getSecret(data kakusuData, group, key string) (string, bool) {
	g, ok := data[group]
	if !ok {
		return "", false
	}
	v, ok := g[key]
	return v, ok
}

func setSecret(data kakusuData, group, key, value string) {
	if _, ok := data[group]; !ok {
		data[group] = make(map[string]string)
	}
	data[group][key] = value
}

func deleteSecret(data kakusuData, group, key string) bool {
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
