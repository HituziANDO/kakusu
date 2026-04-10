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
- Minimal dependencies (`cobra` + `golang.org/x/term`)

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
| `kakusu run [--env FILE] -- <cmd>` | Resolve `kks://` refs in `.env` and run a command    |
| `kakusu export [--env FILE]`       | Output export statements for eval (POSIX shell)      |
| `kakusu export <group>`            | Export all secrets in a group                        |
| `kakusu export <group/key>`        | Export a single secret                               |

### Agent (Key Cache)

| Command               | Description                                          |
| ---------------------- | ---------------------------------------------------- |
| `kakusu lock`          | Clear cached key (succeeds even if agent is not running) |
| `kakusu agent status`  | Show agent status                                    |
| `kakusu agent stop`    | Stop the agent                                       |

## .env File Format

```dotenv
# Plain values as-is
DB_HOST=localhost
DB_PORT=5432

# Secrets use kks:// references
DB_PASSWORD=kks://myproject/db_password
OPENAI_API_KEY=kks://shared/openai_key
```

`kakusu run` or `kakusu export` replaces `kks://group/key` with actual values.

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

## Environment Variables

| Variable          | Description                   | Default                 |
| ----------------- | ----------------------------- | ----------------------- |
| `KAKUSU_FILE`     | Path to the encrypted vault   | `~/.kakusu/secrets.enc` |
| `KAKUSU_LANG`     | Output language (`en`, `ja`)  | `en`                    |
| `KAKUSU_TTL`      | Key cache TTL                 | `30m`                   |
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

## Security

- **Encryption**: AES-256-GCM (authenticated encryption with tamper detection)
- **Key Derivation**: PBKDF2-HMAC-SHA256 (600,000 iterations)
- **Key Cache**: Agent process holds the key in memory only (never written to disk)
- **File Permissions**: Encrypted vault is `0600`, socket is `0600`
- **Agent**: Auto-start, key cleared after TTL expiry, auto-shutdown on idle

## License

MIT
