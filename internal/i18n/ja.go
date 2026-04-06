package i18n

var catalogJA = messages{
	// エラー
	MsgErrMessageTooLarge:    "メッセージが大きすぎます",
	MsgErrAgentStartFailed:   "エージェントの起動に失敗しました",
	MsgErrCiphertextTooShort: "暗号文が短すぎます",
	MsgErrFileCorrupted:      "ファイルが破損しています",
	MsgErrJSONParse:          "JSONパースエラー: %v",
	MsgErrPasswordWrong:      "パスワードが違います（または kakusu が破損しています）",
	MsgErrUnresolvedRefs:     "以下の kks:// 参照が解決できませんでした:\n%s",
	MsgErrRefDetail:          "  line %d: %s → kks://%s/%s",
	MsgErrSecretNotFound:     "kks://%s/%s は存在しません",
	MsgErrGroupNotFound:      "グループ '%s' は存在しません",
	MsgErrEnvFileNotFound:    ".env ファイルが見つかりません: %s",
	MsgErrCommandNotFound:    "コマンドが見つかりません: %s",
	MsgErrUnknownSubcommand:  "不明なサブコマンド: agent %s",
	MsgErrAgentNotRunning:    "エージェントは起動していません",
	MsgErrAgentConnFailed:    "エージェントに接続できません",
	MsgErrUnknownCommand:     "不明なコマンド '%s'",
	MsgErrVaultNotFound:      "Kakusu が見つかりません。先に 'kakusu init' を実行してください",

	// 使い方
	MsgUsageSet:    "使い方: kakusu set <group/key> [value]",
	MsgUsageGet:    "使い方: kakusu get <group/key>",
	MsgUsageShow:   "使い方: kakusu show <group/key>",
	MsgUsageDelete: "使い方: kakusu delete <group/key>",
	MsgUsageRun:    "使い方: kakusu run [--env FILE] -- <command> [args...]",
	MsgUsageAgent:  "使い方: kakusu agent <stop|status>",
	MsgUsageHint:   "  kakusu help  でコマンド一覧を確認してください",

	// プロンプト
	MsgPromptMasterPassword: "マスターパスワード: ",
	MsgPromptNewPassword:    "新しいマスターパスワード: ",
	MsgPromptConfirm:        "もう一度入力: ",
	MsgPromptMinLength:      "8文字以上にしてください",
	MsgPromptMismatch:       "一致しませんでした。再入力してください。",
	MsgPromptOverwrite:      "既存のKakusuが存在します。上書きしますか？ [y/N]: ",
	MsgPromptSecretValue:    "%s/%s の値: ",
	MsgPromptDeleteConfirm:  "kks://%s/%s を削除しますか？ [y/N]: ",

	// ステータス
	MsgCancelled:       "キャンセルしました",
	MsgInitDone:        "Kakusu を初期化しました: %s",
	MsgSecretSaved:     "kks://%s/%s を保存しました",
	MsgNoSecrets:       "（保存済みのシークレットはありません）",
	MsgSecretDeleted:   "kks://%s/%s を削除しました",
	MsgInjected:        "注入: %s",
	MsgPasswordChanged: "マスターパスワードを変更しました",
	MsgAgentStopped:    "エージェントを停止しました",
	MsgAgentStatus:     "エージェント: 起動中 (PID %s)",
	MsgAgentTTL:        "TTL:         %s",
	MsgKeyCachePresent: "鍵キャッシュ: あり（残り %s）",
	MsgKeyCacheNone:    "鍵キャッシュ: なし",
	MsgKeyCacheCleared: "鍵キャッシュを消去しました",

	// ヘルプ（全文）
	MsgHelp: `
kakusu - ローカル秘密情報管理CLI

【基本操作】
  kakusu init                          Kakusu を新規作成（マスターパスワード設定）
  kakusu set <group/key> [value]       シークレットを保存（value省略で非表示入力）
  kakusu get <group/key>               値を取得（stdout、パイプ可）
  kakusu show <group/key>              値を完全表示
  kakusu list [group]                  一覧表示（値はマスク）
  kakusu delete <group/key>            削除
  kakusu passwd                        マスターパスワードを変更
  kakusu version                       バージョン表示

【鍵キャッシュ（エージェント）】
  kakusu lock                          鍵キャッシュを即座に消去
  kakusu agent stop                    エージェントを停止
  kakusu agent status                  エージェントの状態を表示

【.env 連携】
  kakusu run [--env FILE] -- <cmd>     .env の kks:// 参照を解決してコマンド実行
  kakusu export [--env FILE]           eval 用に export 文を出力

【.env ファイルの書き方】
  DB_HOST=localhost
  DB_PASSWORD=kks://myproject/db_password
  OPENAI_API_KEY=kks://shared/openai_key

【使用例】
  kakusu set myproject/db_password
  kakusu set shared/openai_key sk-xxx...
  kakusu run -- python app.py
  kakusu run --env .env.staging -- python app.py
  eval "$(kakusu export)"
  kakusu list
  kakusu list myproject

【環境変数】
  KAKUSU_FILE       保存先のカスタマイズ（例: ~/Dropbox/kakusu/secrets.enc）
  KAKUSU_LANG       出力言語（en, ja; デフォルト: en）
  KAKUSU_TTL        鍵キャッシュの有効期間（デフォルト: 30m、例: 1h, 45m）
  KAKUSU_NO_AGENT   1 に設定するとエージェントを無効化（従来動作）

【セキュリティ】
  暗号化  : AES-256-GCM（認証付き暗号、改ざん検知あり）
  鍵導出  : PBKDF2-HMAC-SHA256（600,000 回反復）
  鍵キャッシュ: エージェントプロセスがメモリ上のみに保持（自動起動・自動終了）
  保存先  : ~/.kakusu/secrets.enc（KAKUSU_FILE 環境変数で変更可）
  グループ: ファイルは1つ、内部で論理的にグループ分離
`,
	MsgVersion: "kakusu version %s",
}
