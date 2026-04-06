package config

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	Version    = "0.4.0"
	DefaultTTL = 30 * time.Minute
)

func KakusuFile() string {
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

func KakusuDir() string {
	return filepath.Dir(KakusuFile())
}

// KakusuHome returns ~/.kakusu (independent of KAKUSU_FILE).
func KakusuHome() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".kakusu")
}

func AgentSocketPath() string {
	return filepath.Join(KakusuHome(), "agent.sock")
}

func AgentPIDPath() string {
	return filepath.Join(KakusuHome(), "agent.pid")
}

func AgentTTL() time.Duration {
	if v := os.Getenv("KAKUSU_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return DefaultTTL
}

func AgentDisabled() bool {
	return os.Getenv("KAKUSU_NO_AGENT") == "1"
}
