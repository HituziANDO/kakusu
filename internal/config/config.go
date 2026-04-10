package config

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Version is set via ldflags at build time (e.g. -X ...config.Version=1.0.0).
var Version = "0.5.0"

const DefaultTTL = 30 * time.Minute

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
