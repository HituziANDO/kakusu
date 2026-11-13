# kakusu

[English](../README.md)

`kakusu` は、ローカルに秘密情報（APIキー、パスワード等）を安全に保存・管理するCLIツールです。

`.env` ファイルに `kks://group/key` という参照を書き、コマンド実行時に実際の値に置換して環境変数として注入します。シークレット本体は暗号化ファイルにのみ存在し、`.env` ファイル自体には機密情報を一切含みません。

## 特徴

- **group/key 階層**でシークレットを整理（`/` を省略すると `default` グループに格納）
- `.env` ファイルの `kks://` 参照を自動解決してコマンドに注入
- **AES-256-GCM** + **PBKDF2-HMAC-SHA256** (600,000 iterations) による暗号化
- **エージェント**によるマスターパスワードキャッシュ（ssh-agent パターン）
- AI ツールや CI が読む出力など、端末でない `kakusu run` の出力では `kks://` 参照の値を隠す
- 値を表示するコマンドは、毎回マスターパスワードを求める
- 書き込むコマンドは vault をロックし、実行中に同期サービスが行った変更を上書きしない
- 外部サービス不要、すべてローカルで完結
- **多言語対応**: 英語（デフォルト）/ 日本語を環境変数で切り替え可能
- 依存ライブラリ最小限（`cobra` + `golang.org/x/term` + `golang.org/x/sys`）

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
| `kakusu run [--env FILE] -- <cmd>` | `.env` の `kks://` 参照を解決してコマンド実行（端末でない出力では値を隠す） |
| `kakusu run --no-masking -- <cmd>` | 出力中の値を隠さずにコマンド実行 |
| `kakusu export [--env FILE]`       | eval 用に export 文を出力（POSIX シェル向け）|
| `kakusu export <group>`            | グループ内の全シークレットを export |
| `kakusu export <group/key>`        | 特定のシークレットを export |

### エージェント（鍵キャッシュ）

| コマンド                  | 説明           |
| --------------------- | ------------ |
| `kakusu lock`         | 鍵キャッシュを消去（エージェント未起動時も成功） |
| `kakusu agent status` | エージェントの状態を表示 |
| `kakusu agent stop`   | エージェントを停止    |

マスターパスワードを入力すると、エージェントが鍵をメモリ上にキャッシュします。`list`・`set`・`delete`・`run` はキャッシュした鍵を使い、パスワードを再度求めません。`get`・`show`・`export`・`passwd` はキャッシュを使わず、毎回マスターパスワードを求めます。コマンドがマスターパスワードを求めるとき、プロンプトにはコマンドが行う操作と、対象があればその対象が表示されます（`list` と `passwd` には対象がありません）。新しいマスターパスワードを設定するときのプロンプトは `新しいマスターパスワード: ` です。macOS と Linux では、stderr をリダイレクトしていてもパスワードのプロンプトは端末に表示されます。正しいパスワードを入力すると、上記のとおり鍵がキャッシュされます。

キャッシュの有効期間（`KAKUSU_TTL`）は使うたびに延長されますが、どれだけ使っていても最後にパスワードを入力してから 8 時間で終わります。どちらも実時間で数えるため、コンピューターがスリープしている間も経過します。

v0.5.0 以前から更新した場合は、更新後に一度 `kakusu agent stop` を実行してください。起動中のエージェントは 8 時間の上限が無い古い動作のまま使われ続け、`kakusu lock` は鍵を消すだけでエージェントを止めません。

## .env ファイルの書き方

```dotenv
# 通常の値はそのまま
DB_HOST=localhost
DB_PORT=5432

# シークレットは kks:// 参照で
DB_PASSWORD=kks://myproject/db_password
OPENAI_API_KEY=kks://shared/openai_key
```

`kakusu run` または `kakusu export` を使うと、`kks://group/key` が実際の値に置換されます。グループを省いた `kks://key` は `default` グループを指します。`kks://` で始まる値を解決できないと、`run` と `export` はエラーで終了します。`kakusu run` では、`.env` ファイルで設定した変数が、同じ名前の継承した環境変数を置き換えます。

```bash
# 直接実行
kakusu run -- python app.py

# 別の .env ファイルを指定
kakusu run --env .env.staging -- python app.py

# シェルの環境変数に展開
eval "$(kakusu export)"
```

`kakusu export` が受け付ける名前は、POSIX シェルの変数名の規則（`[A-Za-z_][A-Za-z0-9_]*`）に合うものだけです。`.env` ファイルの名前、または `kakusu export <group>`・`kakusu export <group/key>` が出力するキーに規則に合わないものが1つでもあると、何も出力せず、その名前を挙げたエラーで終了します。`export FOO=bar` と書いた行も、名前が `export FOO` になるためエラーになります。`FOO=bar` と書いてください。export が保証するのは `.env` ファイルが出力する文の構造を変えられないことだけで、`PS1`・`PROMPT_COMMAND`・`PATH` のような規則に合う名前でも後でシェルにコードを実行させられるため、他人から受け取った `.env` ファイルは、その export を `eval` する前に中身を確認してください。

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

## `run` の出力での値の隠蔽

stdout または stderr が端末でないとき（AI ツールや CI がコマンドを実行するときのようにパイプやファイルのとき）、`kakusu run` はその出力にそのまま現れる `kks://` 参照の値をすべて `<concealed by kakusu>` に置き換えます。`DB_HOST=localhost` のような `.env` ファイルのリテラル値はそのまま出力されます。値はどこに現れてもすべて置き換えるため、`1` や `true` のような短い値やありふれた値では、出力の多くが隠れることがあります。

```bash
kakusu run -- printenv DB_PASSWORD | cat
# <concealed by kakusu>
```

次のものは隠されません。

- 端末への出力（コマンドを疑似端末で実行する AI ツールを含む）
- コマンドが変換した値（base64、URL や JSON のエスケープ、値の一部など）。値はバイト単位で照合するため、改行が LF ではなく CRLF で出力された複数行の値も隠されません
- コマンドが stdout や stderr ではなく、ファイル・`/dev/tty`・ネットワークに書き出した値
- `--no-masking` を付けたときの出力。パイプで値を渡すときに使います: `kakusu run --no-masking -- printenv DB_PASSWORD | other-command`

隠している間、`kakusu run` はコマンドを自分と同じプロセスグループの子として実行し、コマンドの出力をパイプで中継します。そのため、コマンドを直接実行したときと次の点が異なります。

- `kakusu run` はコマンドが終了し、その出力が終わってから終了します。コマンドが起動して出力を開いたままにするバックグラウンドプロセスがあると、Ctrl-C などのシグナルで止めるまで `kakusu run` は終了しません。
- `kakusu run` の出力がファイルでも、コマンドが書く先はパイプです。そのため、出力をシークするコマンドは "Illegal seek" で失敗します。`--no-masking` を使うか、コマンド自身にファイルを書かせてください。
- Unix では、`kakusu run` は受け取ったシグナルをコマンドに渡します。そのため、プロセスグループ全体に送ったシグナル（`kill -TERM -<PGID>`）はコマンドに 2 回届きます。端末の前景にある `kakusu run` だけに送った SIGINT や SIGQUIT は、端末がコマンドにも送るものとして、コマンドに渡しません。
- （Unix で）読み手がいなくなって出力を書けなかったときは、コマンドの終了ステータスで終了します。コマンドが成功していた場合は、シェルが SIGPIPE による終了を表すのと同じ終了ステータス 141 で終了します。ディスクがいっぱいなど他の理由で書けなかったときは、コマンドの結果にかかわらず、エラーを出して終了ステータス 1 で終了します。
- Unix では、SIGHUP・SIGINT・SIGTERM 以外のシグナルでコマンドが終了したとき、`kakusu run` は同じシグナルでは終了せず、終了ステータス 128+N で終了します。シェルが表示する `$?` は同じですが、子プロセスがシグナルで終了したかを調べるプログラムからは、通常の終了に見えます。
- `kakusu run` 自体が SIGKILL で終了すると、コマンドは動き続け、中継していた出力への書き込みはそれ以降失敗します。
- Windows では、stdout と stderr が同じファイル（`2>&1`）でも別々のパイプを通るため、両者の出力の順序がコマンドが書いた順と入れ替わることがあります。

`--no-masking` を付けると、コマンドはシグナルも出力も直接実行したときと同じように動きます。

## 環境変数

| 変数                | 説明              | デフォルト                   |
| ----------------- | --------------- | ----------------------- |
| `KAKUSU_FILE`     | 暗号化ファイルのパス      | `~/.kakusu/secrets.enc` |
| `KAKUSU_LANG`     | 出力言語（`en`, `ja`）  | `en`                    |
| `KAKUSU_TTL`      | 鍵キャッシュの有効期間（上限 `8h`） | `30m`                   |
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

鍵がキャッシュされている間も、その場でパスワードを入力しない限り kakusu 自身は値を出力しません。ただし `kakusu run` で起動したプロセスには値が渡ります。`kakusu run` は、それらのプロセスが端末でない出力にそのまま書いた値を隠しますが（「`run` の出力での値の隠蔽」を参照）、`kakusu run` の実行を許可された AI ツールは、そこに挙げた方法で値を得られます。たとえば、変換して出力する（`kakusu run -- sh -c 'printenv DB_PASSWORD | base64'`）か、`--no-masking` を付けます。`kakusu run` と `--no-masking` を許可するかどうかは AI ツールの権限設定で決めてください。

kakusu は同じユーザーのプロセスを区別できず、どのプロセスもエージェントからキャッシュした鍵を受け取れます。`~/.kakusu` へのアクセスを防ぐのは AI ツールのサンドボックスの役割です。作業を終えたら `kakusu lock` でキャッシュを消去してください。

## コマンドを同時に実行したとき

`init`・`set`・`delete`・`passwd` は、実行中に暗号化ファイルのパスに `.lock` を付けたファイル（既定では `~/.kakusu/secrets.enc.lock`）をロックし、同じデバイスでは 1 つずつ暗号化ファイルを変更します。これらのコマンドのどれかがロックを持っている間、別のコマンドはロックファイルのパスを表示し、先のコマンドが終了するまで待ちます（タイムアウトはありません）。その他のコマンドはロックを取りません。ロックファイルはコマンドの終了後も残ります。

ファイルをロックできないファイルシステムでは、警告を表示してロックせずに続行します。そのためロックは保証ではなく、できる範囲で行うものです。

これらのコマンドは保存の直前に、読み込んだ後で暗号化ファイルが変わっていないかも確かめます。その間に Dropbox などの同期サービスがファイルを置き換えていれば、何も保存せずにエラーで終了します。もう一度コマンドを実行してください。ただし、同期サービスが一方の変更をもう一方へ届ける前に、2 台のデバイスでそれぞれ変更した場合は防げません。同期サービスが競合コピーを作ることがありますが、kakusu はそれをマージしません。

## セキュリティ

- **暗号化**: AES-256-GCM（認証付き暗号、改ざん検知あり）
- **鍵導出**: PBKDF2-HMAC-SHA256（600,000 回反復）
- **鍵キャッシュ**: エージェントプロセスがメモリ上のみに保持（ディスクに書き出さない）
- **値の表示**: `get`・`show`・`export` は毎回マスターパスワードを求め、`list` は値を一部も表示しない
- **`run` の出力**: 端末でない出力では `kks://` 参照の値を隠す
- **ファイル権限**: 暗号化ファイルとそのロックファイル、ソケットは `0600`
- **エージェント**: 自動起動・TTL 経過または最後のパスワード入力から 8 時間で鍵消去・アイドル時に自動終了

## ライセンス

MIT
