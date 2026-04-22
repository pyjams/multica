# multica-lite

A self-contained, Windows-native backend for the Multica desktop app.

- **SQLite database** — no PostgreSQL or Docker required
- **Single binary** — no dependencies to install
- **Works on Windows, macOS, and Linux**
- **Full API compatibility** with the Multica frontend

## Usage

```bash
# Run with defaults (port 8081, data in ~/.multica/lite/)
./server-lite

# Custom port and data directory
PORT=9000 MULTICA_DATA_DIR=./data ./server-lite
```

## Auth

In lite mode, authentication is simplified:

- `POST /auth/send-code` — accepts any email, always returns success
- `POST /auth/verify-code` — accepts any email + any code, returns a JWT token
- `GET /auth/auto-login` — returns a token for the default local user (localhost only)

## Building

```bash
# Build for all platforms
./build.sh

# Or manually:
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o dist/windows/server-lite.exe .
GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build -o dist/darwin/server-lite .
GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build -o dist/linux/server-lite .
```

## Data

All data is stored in `~/.multica/lite/multica-lite.db` (SQLite).

To reset, delete the `.db` file.

## Agent / CLI Integration

The daemon from the main multica CLI can connect to this lite server.
Configure it to point at `http://localhost:8081`:

```bash
multica config set api_url http://localhost:8081
multica daemon start
```

The daemon will register a runtime and start polling for tasks,
executing them using the configured CLI (Claude Code, Codex, etc.).
