package agent

import (
	"encoding/base64"
	"errors"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/HituziANDO/kakusu/internal/config"
	"github.com/HituziANDO/kakusu/internal/platform"
)

func Dial() (net.Conn, error) {
	return net.DialTimeout("unix", config.AgentSocketPath(), time.Second)
}

func GetKey() ([]byte, []byte, error) {
	conn, err := Dial()
	if err != nil {
		return nil, nil, err
	}
	defer conn.Close()
	if err := SendMsg(conn, &Request{Type: "GET_KEY"}); err != nil {
		return nil, nil, err
	}
	var resp Response
	if err := RecvMsg(conn, &resp); err != nil {
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

func SetKey(key, salt []byte) error {
	conn, err := Dial()
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := SendMsg(conn, &Request{
		Type: "SET_KEY",
		Key:  base64.StdEncoding.EncodeToString(key),
		Salt: base64.StdEncoding.EncodeToString(salt),
	}); err != nil {
		return err
	}
	var resp Response
	return RecvMsg(conn, &resp)
}

func ClearKey() error {
	conn, err := Dial()
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := SendMsg(conn, &Request{Type: "CLEAR_KEY"}); err != nil {
		return err
	}
	var resp Response
	return RecvMsg(conn, &resp)
}

func QueryStatus() (*Response, error) {
	conn, err := Dial()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if err := SendMsg(conn, &Request{Type: "STATUS"}); err != nil {
		return nil, err
	}
	var resp Response
	if err := RecvMsg(conn, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func EnsureAgent() error {
	conn, err := Dial()
	if err == nil {
		conn.Close()
		return nil
	}

	sockPath := config.AgentSocketPath()
	os.Remove(sockPath)
	os.Remove(config.AgentPIDPath())
	os.MkdirAll(config.KakusuHome(), 0700)

	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	ttl := config.AgentTTL()
	cmd := exec.Command(exePath, "__agent__", ttl.String())
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = platform.AgentSysProcAttr()

	if err := cmd.Start(); err != nil {
		return err
	}
	cmd.Process.Release()

	for i := 0; i < 20; i++ {
		time.Sleep(50 * time.Millisecond)
		if c, err := Dial(); err == nil {
			c.Close()
			return nil
		}
	}
	return errors.New("failed to start agent")
}
