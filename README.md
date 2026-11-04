# kakusu

[Japanese (日本語)](./docs/README_ja.md)

![kakusu image](./readme-image/kakusu-image.jpg "Header Image")

`kakusu` is a CLI tool for securely storing and managing secrets (API keys, passwords, etc.) locally.

Write `kks://group/key` references in your `.env` files, and kakusu resolves them to actual values at runtime, injecting them as environment variables. The secrets themselves exist only in an encrypted file — your `.env` files never contain sensitive data.

## Features

- **group/key hierarchy** to organize secrets (omitting `/` stores under the `default` group)
- Automatically resolves `kks://` references in `.env` files and injects them into commands
- **AES-256-GCM** + **PBKDF2-HMAC-SHA256** (600,000 iterations) encryption
- **Agent** for master password caching (ssh-agent pattern)
- Fully local — no external services required
- **Multi-language support**: English (default) / Japanese, switchable via environment variable
- Minimal dependencies (`cobra` + `golang.org/x/term` + `golang.org/x/sys`)

## Supported Platforms

| OS      | Arch   | Target            |
| ------- | ------ | ----------------- |
| macOS   | x86_64 | Intel Mac         |
| macOS   | arm64  | Apple Silicon     |
| Linux   | x86_64 | Linux Intel/AMD   |
| Linux   | arm64  | Linux ARM64       |
| Windows | x86_64 | Windows Intel/AMD |

## Installation

Pre-built binaries are available on the [Releases](https://github.com/HituziANDO/kakusu/releases) page.

```bash
go install github.com/HituziANDO/kakusu@latest
```

Or build from source:

```bash
git clone https://github.com/HituziANDO/kakusu.git
cd kakusu
go build -o kakusu .
```

### Local Build with GoReleaser

Build binaries for all platforms at once:

```bash
# Install GoReleaser
brew install goreleaser

# Snapshot build (generate all binaries locally without releasing)
goreleaser release --snapshot --clean
```

Artifacts are output to the `dist/` directory.

## Quick Start

```bash
# 1. Initialize (set master password)
kakusu init

# 2. Store secrets
kakusu set myproject/db_password
kakusu set shared/openai_key sk-xxx...

# 3. Create a .env file
cat <<EOF > .env
DB_HOST=localhost
DB_PASSWORD=kks://myproject/db_password
OPENAI_API_KEY=kks://shared/openai_key
EOF

# 4. Inject secrets and run a command
kakusu run -- python app.py
```

## Commands

### Basic Operations

| Command                          | Description                                      |
| -------------------------------- | ------------------------------------------------ |
| `kakusu init`                    | Initialize a new vault (set master password)     |
| `kakusu set <group/key> [value]` | Store a secret (hidden input if value is omitted)|
| `kakusu get <group/key>`         | Get a value (stdout, pipeable)                   |
| `kakusu show <group/key>`       | Show the full value                              |
| `kakusu list [group]`            | List secrets (values are masked)                 |
| `kakusu delete <group/key>`      | Delete a secret                                  |
| `kakusu passwd`                  | Change master password                           |
| `kakusu version`                 | Show version                                     |

### .env Integration

| Command                            | Description                                          |
| ---------------------------------- | ---------------------------------------------------- |
| `kakusu run [--env FILE] -- <cmd>` | Resolve `kks://` refs in `.env` and run a command (conceals values in output that is not a terminal) |
| `kakusu run --no-masking -- <cmd>` | Run a command without concealing values in output    |
| `kakusu export [--env FILE]`       | Output export statements for eval (POSIX shell)      |
| `kakusu export <group>`            | Export all secrets in a group                        |
| `kakusu export <group/key>`        | Export a single secret                               |

### Agent (Key Cache)

| Command               | Description                                          |
| ---------------------- | ---------------------------------------------------- |
| `kakusu lock`          | Clear cached key (succeeds even if agent is not running) |
| `kakusu agent status`  | Show agent status                                    |
| `kakusu agent stop`    | Stop the agent                                       |

When you enter the master password, the agent caches the key in memory. `list`, `set`, `delete`, and `run` use the cached key without asking again. `get`, `show`, `export`, and `passwd` never use the cache and always ask for the master password.

The cache TTL (`KAKUSU_TTL`) is extended each time the cache is used, but the cache ends 8 hours after the last password entry however often it is used.

If you upgrade from v0.5.0 or earlier, run `kakusu agent stop` once after upgrading. An agent that is already running keeps being used with the old behavior, which has no 8-hour limit, and `kakusu lock` only clears its key without stopping it.

## .env File Format

```dotenv
# Plain values as-is
DB_HOST=localhost
DB_PORT=5432

# Secrets use kks:// references
DB_PASSWORD=kks://myproject/db_password
OPENAI_API_KEY=kks://shared/openai_key
```

`kakusu run` or `kakusu export` replaces `kks://group/key` with actual values. With `kakusu run`, a variable set in the `.env` file replaces an inherited environment variable of the same name.

```bash
# Run directly
kakusu run -- python app.py

# Specify a different .env file
kakusu run --env .env.staging -- python app.py

# Expand into shell environment variables
eval "$(kakusu export)"
```

## `run` vs `export`

|         | `kakusu run`                        | `kakusu export`                              |
| ------- | ----------------------------------- | -------------------------------------------- |
| Method  | Injects env vars and runs a command | Outputs `export KEY='VALUE'` shell statements|
| Scope   | Target command only                 | Entire shell session via `eval`              |
| Use for | Passing secrets to a single command | Using secrets across multiple commands        |

```bash
# run: inject secrets into a specific command only
kakusu run -- python app.py

# export: expand as environment variables in the current shell
eval "$(kakusu export)"
eval "$(kakusu export --env .env.staging)"

# export: export a specific group directly from vault
eval "$(kakusu export myproject)"
```

## Concealing Values in `run` Output

When stdout or stderr is not a terminal (a pipe or a file, as when an AI tool or CI runs the command), `kakusu run` replaces every value resolved from a `kks://` reference that appears as is in that output with `<concealed by kakusu>`. Literal values in the `.env` file, such as `DB_HOST=localhost`, are printed as is. Every occurrence is replaced wherever it appears, so a short or common value such as `1` or `true` may conceal much of the output.

```bash
kakusu run -- printenv DB_PASSWORD | cat
# <concealed by kakusu>
```

The following are not concealed:

- Output to a terminal, including AI tools that run commands in a pseudo-terminal
- Values the command transforms, such as base64, URL or JSON escaping, or part of a value. Values are matched byte for byte, so a multi-line value printed with CRLF line endings instead of LF is not concealed either
- Values the command writes to files, `/dev/tty`, or the network instead of its stdout or stderr
- Output with `--no-masking`. Use it to pass a value through a pipe: `kakusu run --no-masking -- printenv DB_PASSWORD | other-command`

While concealing, `kakusu run` runs the command as a child in the same process group and relays the command's output through a pipe. This differs from running the command directly:

- `kakusu run` exits after the command exits and its output ends. A background process that the command starts and that keeps the output open keeps `kakusu run` running until a signal, such as Ctrl-C, ends it.
- The command writes to a pipe even when the output of `kakusu run` is a file, so a command that seeks in its output fails with "Illegal seek". Use `--no-masking`, or let the command write the file itself.
- On Unix, `kakusu run` passes on the signals it receives, so a signal sent to the whole process group (`kill -TERM -<PGID>`) reaches the command twice. SIGINT or SIGQUIT sent to `kakusu run` alone while it is in the terminal's foreground is not passed on, because the terminal sends them to the command too.
- If the output cannot be written because the reader has gone (on Unix), `kakusu run` exits with the command's status, or with status 141, as a shell reports a death by SIGPIPE, if the command succeeded. If it cannot be written for another reason, such as a full disk, `kakusu run` prints the error and exits with status 1, whatever the command did.

With `--no-masking`, the command runs as if run directly, with the same signals and output.

## Environment Variables

| Variable          | Description                   | Default                 |
| ----------------- | ----------------------------- | ----------------------- |
| `KAKUSU_FILE`     | Path to the encrypted vault   | `~/.kakusu/secrets.enc` |
| `KAKUSU_LANG`     | Output language (`en`, `ja`)  | `en`                    |
| `KAKUSU_TTL`      | Key cache TTL (max `8h`)      | `30m`                   |
| `KAKUSU_NO_AGENT` | Set to `1` to disable agent   | -                       |

## Using with AI Coding Tools

When using AI coding assistants such as Claude Code, **`kakusu run` is strongly recommended**.

```bash
# Recommended: secrets are injected only into the subprocess
kakusu run -- npm start

# Not recommended: exported to the entire shell, readable by AI tools via env / printenv
eval "$(kakusu export)"
```

| Method | Visibility to AI tools |
|---|---|
| `kakusu run -- <cmd>` | Secrets do not remain in the parent shell, so they are **not readable from the parent shell** (they are passed to the target process and its descendants) |
| `eval "$(kakusu export)"` | Expanded as shell environment variables, so they are **readable** |

kakusu protects `.env` files from containing plaintext secrets (secrets never touch disk in plain form). However, values expanded via `export` become regular environment variables accessible to any tool running in the same shell. Using `kakusu run` avoids leaving plaintext environment variables in the parent shell. Note that the target process, its descendants, and process monitoring tools can still access them — `run` narrows the exposure surface compared to `export`.

While the key is cached, kakusu itself prints no value unless you type the password at that moment. Processes started by `kakusu run`, however, receive the values. `kakusu run` conceals the values they print as is to output that is not a terminal (see "Concealing Values in `run` Output"), but an AI tool allowed to run `kakusu run` can still obtain them in the ways listed there, for example by printing them transformed (`kakusu run -- sh -c 'printenv DB_PASSWORD | base64'`) or with `--no-masking`. Decide whether to allow `kakusu run` and `--no-masking` in the AI tool's permission settings.

kakusu cannot distinguish processes running as the same user: any of them can obtain the cached key from the agent. Blocking access to `~/.kakusu` is the job of the AI tool's sandbox. Run `kakusu lock` to clear the cache when you finish.

## Security

- **Encryption**: AES-256-GCM (authenticated encryption with tamper detection)
- **Key Derivation**: PBKDF2-HMAC-SHA256 (600,000 iterations)
- **Key Cache**: Agent process holds the key in memory only (never written to disk)
- **File Permissions**: Encrypted vault is `0600`, socket is `0600`
- **Agent**: Auto-start, key cleared after TTL expiry or 8 hours after the last password entry, auto-shutdown on idle

## License

MIT
