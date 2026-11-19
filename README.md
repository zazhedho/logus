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
	// No context given: logs with context.Background(), so log_id is a freshly
	// generated id for this call.
	logus.WriteLog(logus.LogLevelInfo, "service started")

	// Request id: log_id equals the request id.
	reqCtx := logus.WithRequestID(context.Background(), uuid.New())
	id := logus.RequestID(reqCtx)
	logus.WriteLogWithContext(reqCtx, logus.LogLevelInfo, "request id is ", id)
}
```

To set an explicit log id and user id instead, use `WithLogMetadata`:

```go
ctx := logus.WithLogMetadata(context.Background(), "req-123", "user-42")
logus.WriteLogWithContext(ctx, logus.LogLevelWarn, "slow query: ", 1200, "ms")
// log_id = "req-123", user_id = "user-42"
```

Metadata wins over the request id: when both are present, `log_id` is the one from `WithLogMetadata`.

- `WriteLog(level, msg...)` logs with `context.Background()`, so each line gets a freshly generated `log_id` and no `user_id`.
- `WriteLogWithContext(ctx, level, msg...)` adds `log_id` and `user_id` from the context.
- `WithLogMetadata(ctx, logID, userID)` stores an explicit log id and user id; empty values do not overwrite existing ones.
- `WithRequestID(ctx, id)` stores a request id; `RequestID(ctx)` returns it, generating a new one when none is present.

## Levels

Six standard levels, ordered trace < debug < info < warn < error < fatal. `Level` is an `int` whose values match `log/slog`.

| Constant | Value | Name | When to use |
| --- | --- | --- | --- |
| `LogLevelTrace` | -8 | `TRACE` | Finest-grained tracing, normally off. |
| `LogLevelDebug` | -4 | `DEBUG` | Diagnostics for development. |
| `LogLevelInfo` | 0 | `INFO` | Normal significant events. |
| `LogLevelWarn` | 4 | `WARN` | Something is off but the service continues. |
| `LogLevelError` | 8 | `ERROR` | An operation failed and needs attention. |
| `LogLevelFatal` | 12 | `FATAL` | An unrecoverable condition. It is only logged: logus never exits or panics, the caller decides whether to stop. |

The level appears by name (`TRACE`, `FATAL`, ...) in every output format. A call is written when its level is at or above the configured level. Values that are not one of the six constants are ignored.

### Setting the level

`LOG_LEVEL` accepts `trace`, `debug`, `info`, `warn` (or `warning`), `error` and `fatal`, case-insensitive and ignoring surrounding spaces. The default is `info`, for both `WriteLog` and `WriteLogWithContext`.

Legacy numeric values from v0.1.x are still accepted: `0`-`1` mean `error`, `2`-`5` mean `info`, `6` means `debug`. Any other value, including an empty one, falls back to `info` and writes one warning line the first time.

`LOG_LEVEL` is read once, when the logger is first used. To change the level while running, use `SetLevel` and `GetLevel`; both are safe for concurrent use, and `SetLevel` called before the first log wins over `LOG_LEVEL`:

```go
logus.SetLevel(logus.LogLevelDebug)
current := logus.GetLevel() // LogLevelDebug
```

## Configuration

| Variable | Default | Description |
| --- | --- | --- |
| `LOG_LEVEL` | `info` | Lowest level that is written; see above. Read once. |
| `LOG_FORMAT` | `json` | `json`, `text` (slog key=value) or `string` (compact bracketed line). Any other value falls back to `json`. Read once. |
| `ServerIP` | empty | Written as `server_ip`. |
| `NODE` | empty | Written as `node` when set. |

### Output formats

`json` and `text` write `time`, `level` and `msg` plus these attributes: `server_ip`, `source_file`, `source_line`, `source_function`, `node` (only when set), `log_id` (for non-nil contexts) and `user_id` (only when known). `string` produces lines such as:

```
2026/01/02 03:04:05 .123456 [10.0.0.1][api][WARN][log-id][user-id][path/file.go:12] message
```

The user id and source segments are omitted when empty. The log id segment is present whenever a non-nil context is logged, which includes every `WriteLog` call.

## Known behavior

- `ServerIP` and `NODE` are read on every call; `LOG_LEVEL` and `LOG_FORMAT` only once.
- Contexts exposing either `Get(string)`/`Set(string, any)` or `Get(any)`/`Set(any, any)` (the latter is `*gin.Context` in current gin) are recognized without importing gin, using the keys `CTX_ID` for the request id and `userId` for the user id.
- `log_id` is always written for non-nil contexts, including `WriteLog` (which uses `context.Background()`). When no log id or request id is stored, a new id is generated per call, so consecutive `WriteLog` lines get different ids.
- A stored request id equal to the nil UUID is treated as missing and replaced with a newly generated id.

## Upgrading from v0.1.x

- Removed constants: `LogLevelPanic` (use `LogLevelError`), `LogLevelFail` and `LogLevelData` (use `LogLevelInfo`).
- `WriteLog` and `WriteLogWithContext` take a `Level` instead of an `int`; the constants are typed, so only code passing raw integers needs changing.
- A numeric `LOG_LEVEL` still works through the mapping above, but the old meaning (a higher number logs more) is gone: prefer level names.
- The default is now `info` for both functions (it was 5 and 6, which logged debug for `WriteLogWithContext`).
- `LOG_LEVEL` is read once, not on every call; use `SetLevel` to change it at runtime.

## Versioning

logus follows semantic versioning. While it is pre-1.0, a minor version bump (v0.2.0 to v0.3.0) may contain breaking changes; a patch release (v0.2.0 to v0.2.1) never does. Pin the minor version and read the release notes before upgrading.

## Development

```bash
make test         # go test -race ./...
make lint-install # one-time: installs golangci-lint
make lint         # golangci-lint with .golangci.yml
```

## Layout

The repository root holds only the public API (`logus.go`) and its tests. The
implementation lives under `internal/core` and is not importable by other
modules.

## License

MIT; see [LICENSE](LICENSE).
