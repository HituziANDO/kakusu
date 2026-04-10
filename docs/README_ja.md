# kakusu

[English](../README.md)

![kakusu image](../readme-image/kakusu-image.jpg "Header Image")

`kakusu` は、ローカルに秘密情報（APIキー、パスワード等）を安全に保存・管理するCLIツールです。

`.env` ファイルに `kks://group/key` という参照を書き、コマンド実行時に実際の値に置換して環境変数として注入します。シークレット本体は暗号化ファイルにのみ存在し、`.env` ファイル自体には機密情報を一切含みません。

## 特徴

- **group/key 階層**でシークレットを整理（`/` を省略すると `default` グループに格納）
- `.env` ファイルの `kks://` 参照を自動解決してコマンドに注入
- **AES-256-GCM** + **PBKDF2-HMAC-SHA256** (600,000 iterations) による暗号化
- **エージェント**によるマスターパスワードキャッシュ（ssh-agent パターン）
- 外部サービス不要、すべてローカルで完結
- **多言語対応**: 英語（デフォルト）/ 日本語を環境変数で切り替え可能
- 依存ライブラリ最小限（`cobra` + `golang.org/x/term`）

## 対応プラットフォーム

| OS      | Arch   | 対象                |
| ------- | ------ | ----------------- |
| macOS   | x86_64 | Intel Mac         |
| macOS   | arm64  | Apple Silicon     |
| Linux   | x86_64 | Linux Intel/AMD   |
| Linux   | arm64  | Linux ARM64       |
| Windows | x86_64 | Windows Intel/AMD |

## インストール

[Releases](https://github.com/HituziANDO/kakusu/releases) ページからお使いの OS/Arch に合ったバイナリをダウンロードできます。

```bash
go install github.com/HituziANDO/kakusu@latest
```

または、ソースからビルド:

```bash
git clone https://github.com/HituziANDO/kakusu.git
cd kakusu
go build -o kakusu .
```

### GoReleaser でローカルビルド

全プラットフォーム向けのバイナリをまとめてビルドできます。

```bash
# GoReleaser のインストール
brew install goreleaser

# スナップショットビルド（リリースせずにローカルで全バイナリを生成）
goreleaser release --snapshot --clean
```

成果物は `dist/` ディレクトリに出力されます。

## クイックスタート

```bash
# 1. 初期化（マスターパスワードを設定）
kakusu init

# 2. シークレットを保存
kakusu set myproject/db_password
kakusu set shared/openai_key sk-xxx...

# 3. .env ファイルを作成
cat <<EOF > .env
DB_HOST=localhost
DB_PASSWORD=kks://myproject/db_password
OPENAI_API_KEY=kks://shared/openai_key
EOF

# 4. シークレットを注入してコマンド実行
kakusu run -- python app.py
```

## コマンド

### 基本操作

| コマンド                             | 説明                        |
| -------------------------------- | ------------------------- |
| `kakusu init`                    | Kakusu を新規作成（マスターパスワード設定） |
| `kakusu set <group/key> [value]` | シークレットを保存（value 省略で非表示入力） |
| `kakusu get <group/key>`         | 値を取得（stdout、パイプ可）         |
| `kakusu show <group/key>`        | 値を完全表示                    |
| `kakusu list [group]`            | 一覧表示（値はマスク）               |
| `kakusu delete <group/key>`      | 削除                        |
| `kakusu passwd`                  | マスターパスワードを変更              |
| `kakusu version`                 | バージョン表示                   |

### .env 連携

| コマンド                               | 説明                              |
| ---------------------------------- | ------------------------------- |
| `kakusu run [--env FILE] -- <cmd>` | `.env` の `kks://` 参照を解決してコマンド実行 |
| `kakusu export [--env FILE]`       | eval 用に export 文を出力（POSIX シェル向け）|
| `kakusu export <group>`            | グループ内の全シークレットを export |
| `kakusu export <group/key>`        | 特定のシークレットを export |

### エージェント（鍵キャッシュ）

| コマンド                  | 説明           |
| --------------------- | ------------ |
| `kakusu lock`         | 鍵キャッシュを消去（エージェント未起動時も成功） |
| `kakusu agent status` | エージェントの状態を表示 |
| `kakusu agent stop`   | エージェントを停止    |

## .env ファイルの書き方

```dotenv
# 通常の値はそのまま
DB_HOST=localhost
DB_PORT=5432

# シークレットは kks:// 参照で
DB_PASSWORD=kks://myproject/db_password
OPENAI_API_KEY=kks://shared/openai_key
```

`kakusu run` または `kakusu export` を使うと、`kks://group/key` が実際の値に置換されます。

```bash
# 直接実行
kakusu run -- python app.py

# 別の .env ファイルを指定
kakusu run --env .env.staging -- python app.py

# シェルの環境変数に展開
eval "$(kakusu export)"
```

## `run` と `export` の違い

|      | `kakusu run`          | `kakusu export`                 |
| ---- | --------------------- | ------------------------------- |
| 方式   | 環境変数を注入してコマンドを実行      | `export KEY='VALUE'` 形式のシェル文を出力 |
| スコープ | 指定したコマンドのみ            | `eval` で現在のシェルセッション全体           |
| 用途   | 1つのコマンドにシークレットを渡したいとき | シェル全体で複数コマンドから使いたいとき            |

```bash
# run: 指定コマンドだけにシークレットを注入
kakusu run -- python app.py

# export: 現在のシェルに環境変数として展開
eval "$(kakusu export)"
eval "$(kakusu export --env .env.staging)"

# export: vault から特定グループを直接 export
eval "$(kakusu export myproject)"
```

## 環境変数

| 変数                | 説明              | デフォルト                   |
| ----------------- | --------------- | ----------------------- |
| `KAKUSU_FILE`     | 暗号化ファイルのパス      | `~/.kakusu/secrets.enc` |
| `KAKUSU_LANG`     | 出力言語（`en`, `ja`）  | `en`                    |
| `KAKUSU_TTL`      | 鍵キャッシュの有効期間     | `30m`                   |
| `KAKUSU_NO_AGENT` | `1` でエージェントを無効化 | -                       |

## AI コーディングツールとの共存

Claude Code などの AI コーディングアシスタントと併用する場合、**`kakusu run` の使用を強く推奨**します。

```bash
# 推奨: シークレットはサブプロセスにのみ注入される
kakusu run -- npm start

# 非推奨: シェル全体にexportされ、AIツールから env / printenv で読める
eval "$(kakusu export)"
```

| 方式 | AI ツールからの可視性 |
|---|---|
| `kakusu run -- <cmd>` | 親シェルに秘密が残らないため**親シェルからは読めない**（実行対象プロセスとその子孫には渡る） |
| `eval "$(kakusu export)"` | シェルの環境変数として展開されるため**読める** |

kakusu は `.env` ファイルの平文保護（ディスク上に秘密を置かない）を提供しますが、`export` でシェルに展開した値は通常の環境変数となり、同一シェルで動作するツールからアクセス可能です。`kakusu run` を使えば、親シェルに平文環境変数を残しません。ただし、実行対象プロセスとその子孫、プロセス監視ツールからはアクセス可能であり、`export` より露出範囲を狭める手段です。

## セキュリティ

- **暗号化**: AES-256-GCM（認証付き暗号、改ざん検知あり）
- **鍵導出**: PBKDF2-HMAC-SHA256（600,000 回反復）
- **鍵キャッシュ**: エージェントプロセスがメモリ上のみに保持（ディスクに書き出さない）
- **ファイル権限**: 暗号化ファイルは `0600`、ソケットも `0600`
- **エージェント**: 自動起動・TTL 経過で鍵消去・アイドル時に自動終了

## ライセンス

MIT
