package main

import (
	"fmt"
	"os"
	"time"

	"github.com/HituziANDO/kakusu/internal/agent"
	"github.com/HituziANDO/kakusu/internal/cli"
	"github.com/HituziANDO/kakusu/internal/config"
	"github.com/HituziANDO/kakusu/internal/i18n"
)

func main() {
	i18n.InitLang()

	// Internal command: start agent server.
	if len(os.Args) >= 2 && os.Args[1] == "__agent__" {
		ttl := config.AgentTTL()
		if len(os.Args) >= 3 {
			if d, err := time.ParseDuration(os.Args[2]); err == nil && d > 0 {
				ttl = d
			}
		}
		agent.Serve(ttl)
		return
	}

	if err := cli.RootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		fmt.Fprintln(os.Stderr, i18n.Msg(i18n.MsgUsageHint))
		os.Exit(1)
	}
}
