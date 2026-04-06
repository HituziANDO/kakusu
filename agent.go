package main

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

// ---------------------------------------------------------------------------
// エージェント設定
// ---------------------------------------------------------------------------

func agentTTL() time.Duration {
	if v := os.Getenv("KAKUSU_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return defaultTTL
}

func agentDisabled() bool {
	return os.Getenv("KAKUSU_NO_AGENT") == "1"
}

// ---------------------------------------------------------------------------
// エージェント プロトコル
// メッセージ: [length 4B LE][JSON payload]
// ---------------------------------------------------------------------------

type agentRequest struct {
	Type string `json:"type"`
	Key  string `json:"key,omitempty"`
	Salt string `json:"salt,omitempty"`
}

type agentResponse struct {
	Status           string `json:"status"`
	Key              string `json:"key,omitempty"`
	Salt             string `json:"salt,omitempty"`
	HasKey           bool   `json:"has_key,omitempty"`
	TTLSeconds       int    `json:"ttl_seconds,omitempty"`
	RemainingSeconds int    `json:"remaining_seconds,omitempty"`
}

func agentSendMsg(conn net.Conn, msg any) error {
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(data)))
	if _, err := conn.Write(lenBuf[:]); err != nil {
		return err
	}
	_, err = conn.Write(data)
	return err
}

func agentRecvMsg(conn net.Conn, msg any) error {
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var lenBuf [4]byte
	if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
		return err
	}
	length := binary.LittleEndian.Uint32(lenBuf[:])
	if length > 1<<20 {
		return errors.New(i18nMsg(MsgErrMessageTooLarge))
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(conn, data); err != nil {
		return err
	}
	return json.Unmarshal(data, msg)
}

// ---------------------------------------------------------------------------
// エージェント クライアント
// ---------------------------------------------------------------------------

func agentDial() (net.Conn, error) {
	return net.DialTimeout("unix", agentSocketPath(), time.Second)
}

func agentGetKey() ([]byte, []byte, error) {
	conn, err := agentDial()
	if err != nil {
		return nil, nil, err
	}
	defer conn.Close()
	if err := agentSendMsg(conn, &agentRequest{Type: "GET_KEY"}); err != nil {
		return nil, nil, err
	}
	var resp agentResponse
	if err := agentRecvMsg(conn, &resp); err != nil {
		return nil, nil, err
	}
	if resp.Status == "NO_KEY" {
		return nil, nil, nil
	}
	key, err := base64.StdEncoding.DecodeString(resp.Key)
	if err != nil {
		return nil, nil, err
	}
	salt, err := base64.StdEncoding.DecodeString(resp.Salt)
	if err != nil {
		return nil, nil, err
	}
	return key, salt, nil
}

func agentSetKey(key, salt []byte) error {
	conn, err := agentDial()
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := agentSendMsg(conn, &agentRequest{
		Type: "SET_KEY",
		Key:  base64.StdEncoding.EncodeToString(key),
		Salt: base64.StdEncoding.EncodeToString(salt),
	}); err != nil {
		return err
	}
	var resp agentResponse
	return agentRecvMsg(conn, &resp)
}

func agentClearKey() error {
	conn, err := agentDial()
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := agentSendMsg(conn, &agentRequest{Type: "CLEAR_KEY"}); err != nil {
		return err
	}
	var resp agentResponse
	return agentRecvMsg(conn, &resp)
}

func agentQueryStatus() (*agentResponse, error) {
	conn, err := agentDial()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if err := agentSendMsg(conn, &agentRequest{Type: "STATUS"}); err != nil {
		return nil, err
	}
	var resp agentResponse
	if err := agentRecvMsg(conn, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func ensureAgent() error {
	conn, err := agentDial()
	if err == nil {
		conn.Close()
		return nil
	}

	sockPath := agentSocketPath()
	os.Remove(sockPath)
	os.Remove(agentPIDPath())
	os.MkdirAll(kakusuHome(), 0700)

	exe, err := os.Executable()
	if err != nil {
		return err
	}

	ttl := agentTTL()
	cmd := exec.Command(exe, "__agent__", ttl.String())
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = agentSysProcAttr()

	if err := cmd.Start(); err != nil {
		return err
	}
	cmd.Process.Release()

	for i := 0; i < 20; i++ {
		time.Sleep(50 * time.Millisecond)
		if c, err := agentDial(); err == nil {
			c.Close()
			return nil
		}
	}
	return errors.New(i18nMsg(MsgErrAgentStartFailed))
}

// ---------------------------------------------------------------------------
// エージェント サーバー（バックグラウンドプロセス）
// ---------------------------------------------------------------------------

func stopTimer(t *time.Timer) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
}

func agentServe(ttlDur time.Duration) {
	sockPath := agentSocketPath()

	// 別のエージェントが既に起動中なら終了
	if conn, err := net.DialTimeout("unix", sockPath, 500*time.Millisecond); err == nil {
		conn.Close()
		return
	}

	os.Remove(sockPath)
	os.MkdirAll(kakusuHome(), 0700)

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		return
	}
	os.Chmod(sockPath, 0600)

	pidPath := agentPIDPath()
	os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0600)

	// Accept goroutine → channel
	connCh := make(chan net.Conn, 1)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				close(connCh)
				return
			}
			connCh <- conn
		}
	}()

	// シグナルハンドリング
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	ignoreHUP()

	// 状態
	var cachedKey, cachedSalt []byte
	var expiresAt time.Time

	clearKey := func() {
		for i := range cachedKey {
			cachedKey[i] = 0
		}
		cachedKey = nil
		cachedSalt = nil
		expiresAt = time.Time{}
	}

	// keyTimer: 鍵キャッシュの有効期限
	keyTimer := time.NewTimer(0)
	stopTimer(keyTimer)

	// idleTimer: 鍵なし状態が続いたらプロセス自動終了 (TTL × 2)
	idleTimer := time.NewTimer(ttlDur * 2)

	for {
		select {
		case conn, ok := <-connCh:
			if !ok {
				goto done
			}

			var req agentRequest
			if err := agentRecvMsg(conn, &req); err != nil {
				conn.Close()
				continue
			}

			var resp agentResponse
			switch req.Type {
			case "GET_KEY":
				if cachedKey != nil && time.Now().Before(expiresAt) {
					resp.Status = "OK"
					resp.Key = base64.StdEncoding.EncodeToString(cachedKey)
					resp.Salt = base64.StdEncoding.EncodeToString(cachedSalt)
					// TTL リフレッシュ（アクティビティベース）
					expiresAt = time.Now().Add(ttlDur)
					stopTimer(keyTimer)
					keyTimer.Reset(ttlDur)
				} else {
					if cachedKey != nil {
						clearKey()
						stopTimer(keyTimer)
						stopTimer(idleTimer)
						idleTimer.Reset(ttlDur * 2)
					}
					resp.Status = "NO_KEY"
				}

			case "SET_KEY":
				clearKey()
				cachedKey, _ = base64.StdEncoding.DecodeString(req.Key)
				cachedSalt, _ = base64.StdEncoding.DecodeString(req.Salt)
				expiresAt = time.Now().Add(ttlDur)
				stopTimer(keyTimer)
				keyTimer.Reset(ttlDur)
				stopTimer(idleTimer)
				resp.Status = "OK"

			case "CLEAR_KEY":
				clearKey()
				stopTimer(keyTimer)
				stopTimer(idleTimer)
				idleTimer.Reset(ttlDur * 2)
				resp.Status = "OK"

			case "STATUS":
				resp.Status = "OK"
				resp.HasKey = cachedKey != nil && time.Now().Before(expiresAt)
				resp.TTLSeconds = int(ttlDur.Seconds())
				if resp.HasKey {
					remaining := time.Until(expiresAt)
					if remaining < 0 {
						remaining = 0
					}
					resp.RemainingSeconds = int(remaining.Seconds())
				}

			default:
				resp.Status = "ERROR"
			}

			agentSendMsg(conn, &resp)
			conn.Close()

		case <-keyTimer.C:
			clearKey()
			stopTimer(idleTimer)
			idleTimer.Reset(ttlDur * 2)

		case <-idleTimer.C:
			goto done

		case <-sigCh:
			goto done
		}
	}

done:
	clearKey()
	listener.Close()
	os.Remove(sockPath)
	os.Remove(pidPath)
}
