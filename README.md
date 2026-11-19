# logus

Small structured logging module for Go services, built on `log/slog`. It writes one line per call with the server IP, node name, caller location, request id and user id attached automatically. It has no web framework dependency; its only external dependency is `github.com/google/uuid`.

## Installation

Requires Go 1.27 or newer.

```bash
go get github.com/zazhedho/logus
```

## Usage

```go
package main

import (
	"context"

	"github.com/google/uuid"
	"github.com/zazhedho/logus"
)

func main() {
	// No context: logs with context.Background(), so log_id is a freshly
	// generated id for this call.
	logus.Log(logus.LevelInfo, "service started")
	logus.Logf(logus.LevelInfo, "listening on port %d", 8080)

	// Request and user ids travel in the context; log_id equals the request id.
	ctx := logus.WithRequestID(context.Background(), uuid.New())
	ctx = logus.WithUserID(ctx, "user-42")
	logus.LogContext(ctx, logus.LevelInfo, "request id is", logus.RequestID(ctx))
	logus.LogContextf(ctx, logus.LevelWarn, "slow query: %dms", 1200)
}
```

| Function | Purpose |
| --- | --- |
| `Log(level, args...)` | Logs with `context.Background()`: a fresh `log_id` per call, no `user_id`. |
| `Logf(level, format, args...)` | `Log` with a `fmt.Sprintf` message. |
| `LogContext(ctx, level, args...)` | Adds `log_id` and `user_id` taken from the context. |
| `LogContextf(ctx, level, format, args...)` | `LogContext` with a `fmt.Sprintf` message. |
| `WithRequestID(ctx, id)` | Stores a `uuid.UUID` request id in the context. |
| `RequestID(ctx)` | Returns the request id, generating one when none is present. |
| `RequestIDFromContext(ctx)` | Returns the id stored by `WithRequestID` and whether there was one. |
| `WithUserID(ctx, id)` | Stores a user id in the context. |
| `UserID(ctx)` | Returns the user id, or an empty string. |
| `SetOutput(w)` | Sends all lines to `w`; `nil` restores `os.Stdout`. |
| `SetFormat(name)` | Selects `json`, `text` or `string`. |
| `SetLevel(level)` / `GetLevel()` | Changes or reads the threshold. |

`Log` and `LogContext` join their arguments with a single space, whatever their types: `Log(LevelInfo, "a", 1, "b")` writes `a 1 b`.

Call these functions directly from your code. Each one is a direct call into the implementation so that `source_file`, `source_line` and `source_function` name your call site; wrapping them in your own helper makes the helper the reported caller.

## Levels

Six standard levels, ordered trace < debug < info < warn < error < fatal. `Level` is an `int` whose values match `log/slog`.

| Constant | Value | Name | When to use |
| --- | --- | --- | --- |
| `LevelTrace` | -8 | `TRACE` | Finest-grained tracing, normally off. |
| `LevelDebug` | -4 | `DEBUG` | Diagnostics for development. |
| `LevelInfo` | 0 | `INFO` | Normal significant events. |
| `LevelWarn` | 4 | `WARN` | Something is off but the service continues. |
| `LevelError` | 8 | `ERROR` | An operation failed and needs attention. |
| `LevelFatal` | 12 | `FATAL` | An unrecoverable condition. It is only logged: logus never exits or panics, the caller decides whether to stop. |

The level appears by name (`TRACE`, `FATAL`, ...) in every output format. A call is written when its level is at or above the configured threshold. A call whose level is not one of the six constants is ignored.

### Setting the level

`LOG_LEVEL` accepts `trace`, `debug`, `info`, `warn` (or `warning`), `error` and `fatal`, case-insensitive and ignoring surrounding spaces. The default is `info`. An empty or blank `LOG_LEVEL` counts as unset. The numeric values `0`-`1` (meaning `error`), `2`-`5` (`info`) and `6` (`debug`) are also accepted. Any other value falls back to `info` and writes one warning line the first time.

`LOG_LEVEL` is read once, when the logger is first used. To change the threshold while running, use `SetLevel` and `GetLevel`; both are safe for concurrent use, and `SetLevel` called before the first log wins over `LOG_LEVEL`:

```go
logus.SetLevel(logus.LevelDebug)
current := logus.GetLevel() // LevelDebug
```

`SetLevel` accepts only the six level constants. Any other value, such as `Level(3)` or `Level(99)`, is ignored and the threshold stays as it was.

## Configuration

| Variable | Default | Description |
| --- | --- | --- |
| `LOG_LEVEL` | `info` | Lowest level that is written; see above. Read once. |
| `LOG_FORMAT` | `json` | `json`, `text` (slog key=value) or `string` (compact bracketed line). An unknown value falls back to `json` and writes one warning. Read once. |
| `SERVER_IP` | empty | Written as `server_ip`; the line omits the field when it is empty. The legacy name `ServerIP` is read when `SERVER_IP` is empty. Read once. |
| `NODE` | empty | Written as `node` when set. Read once. |

The working directory (used to shorten `source_file`), `SERVER_IP` and `NODE` are resolved on the first log call and not looked up again.

### Output and format

Lines go to `os.Stdout` unless redirected. `SetOutput` and `SetFormat` take effect for the lines that follow and are safe for concurrent use; `SetFormat` called before the first log wins over `LOG_FORMAT`.

```go
var buf bytes.Buffer
logus.SetOutput(&buf)    // any io.Writer; nil restores os.Stdout
logus.SetFormat("string") // "json", "text" or "string"
```

An empty format name means `json`; an unknown name falls back to `json` and writes one warning.

`json` and `text` write `time`, `level` and `msg` plus these attributes: `server_ip` (only when known), `source_file`, `source_line`, `source_function`, `node` (only when set), `log_id` (for non-nil contexts) and `user_id` (only when known). `string` produces lines such as:

```
2026/01/02 03:04:05 .123456 [10.0.0.1][api][WARN][log-id][user-id][path/file.go:12] message
```

The user id and source segments are omitted when empty. The log id segment is present whenever a non-nil context is logged, which includes every `Log` call.

### Control characters

Every format writes one line per call. In the `string` format, carriage returns, line feeds, tabs and all other control characters (including DEL and the Unicode line and paragraph separators) in the message and in every bracketed field are written as visible escapes such as `\n`, `\r` and `\x1b`, so user-controlled text cannot forge additional log lines. The `json` and `text` formats escape or quote these characters themselves.

## Request ids and gin

Contexts exposing either `Get(string)`/`Set(string, any)` or `Get(any)`/`Set(any, any)` (the latter is `*gin.Context` in current gin) are recognized without importing gin. The request id is read from, and a generated one stored under, the key `CtxKeyRequestID` (`"CTX_ID"`); the user id is read from `CtxKeyUserID` (`"userId"`). `WithRequestID` and `WithUserID` take precedence over these keys.

`*gin.Context` objects are pooled and reused after the handler returns. Do not pass a `*gin.Context` to a goroutine that outlives the handler and call `RequestID` on it there: the context may already belong to another request, or `RequestID` may write to it concurrently. Take the request's standard context instead and pass that on:

```go
func handler(c *gin.Context) {
	ctx := logus.WithRequestID(c.Request.Context(), logus.RequestID(c))
	ctx = logus.WithUserID(ctx, c.GetString(logus.CtxKeyUserID))
	go func() {
		logus.LogContext(ctx, logus.LevelInfo, "background work")
	}()
}
```

## Known behavior

- `log_id` is always written for non-nil contexts, including `Log` (which uses `context.Background()`). When no request id is stored, a new id is generated per call, so consecutive `Log` lines get different ids.
- A stored request id equal to the nil UUID is treated as missing and replaced with a newly generated id.

## Versioning

logus follows semantic versioning. While it is pre-1.0, a minor version bump (v0.x to v0.y) may contain breaking changes; a patch release (v0.x.y to v0.x.z) never does. Pin the minor version and read the release notes before upgrading.

## Development

```bash
make test         # go test -race ./...
go vet ./...      # also run in CI, together with go mod tidy and govulncheck
make lint-install # one-time: installs golangci-lint
make lint         # golangci-lint with .golangci.yml
```

## Layout

The repository root holds only the public API (`logus.go`) and its tests. The
implementation lives under `internal/core` and is not importable by other
modules. `Level`, the level constants and the context keys are aliases of the
`internal/core` definitions.

## License

MIT; see [LICENSE](LICENSE).
