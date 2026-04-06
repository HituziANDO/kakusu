package main

// kakusu - ローカル秘密情報管理CLI
//
// 仕様:
//   - group/key 階層でシークレットを管理
//   - .env ファイルに kks://group/key と書いてコマンド実行時に注入
//   - 保存先: ~/.kakusu/secrets.enc (環境変数 KAKUSU_FILE で変更可)
//   - 暗号化: AES-256-GCM + PBKDF2-HMAC-SHA256 (600,000 iterations)
//   - エージェント: バックグラウンドプロセスで鍵をキャッシュ（自動起動）

import (
	"fmt"
	"os"
	"time"
)

func main() {
	initLang()

	// 内部コマンド: エージェントサーバー起動
	if len(os.Args) >= 2 && os.Args[1] == "__agent__" {
		ttl := agentTTL()
		if len(os.Args) >= 3 {
			if d, err := time.ParseDuration(os.Args[2]); err == nil && d > 0 {
				ttl = d
			}
		}
		agentServe(ttl)
		return
	}

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		fmt.Fprintln(os.Stderr, i18nMsg(MsgUsageHint))
		os.Exit(1)
	}
}
