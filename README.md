# logus

Small structured logging module for Go services, built on `log/slog`. It writes one line per call with the server IP, node name, caller location, request id and user id attached automatically. It has no web framework dependency; its only external dependency is `github.com/google/uuid`.

## Installation

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
	logus.WriteLogWithContext(reqCtx, logus.LogLevelData, "request id is ", id)
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

| Constant | Value | slog level |
| --- | --- | --- |
| `LogLevelPanic` | 0 | Error |
| `LogLevelError` | 1 | Error |
| `LogLevelFail` | 2 | Info |
| `LogLevelInfo` | 3 | Info |
| `LogLevelData` | 4 | Info |
| `LogLevelWarn` | 5 | Warn |
| `LogLevelDebug` | 6 | Debug |

A call is written when `LOG_LEVEL` is greater than or equal to its level value.

## Configuration

| Variable | Default | Description |
| --- | --- | --- |
| `LOG_LEVEL` | `5` for `WriteLog`, `6` for `WriteLogWithContext` | Highest level value that is written. |
| `LOG_FORMAT` | `json` | `json`, `text` (slog key=value) or `string` (compact bracketed line). Any other value falls back to `json`. |
| `ServerIP` | empty | Written as `server_ip`. |
| `NODE` | empty | Written as `node` when set. |

### Output formats

`string` produces lines such as:

```
2026/01/02 03:04:05 .123456 [10.0.0.1][api][WARN][log-id][user-id][path/file.go:12] message
```

The user id and source segments are omitted when empty. The log id segment is present whenever a non-nil context is logged, which includes every `WriteLog` call.

## Known behavior

- Without `LOG_LEVEL`, `WriteLog` uses the default 5 (Debug is dropped) and `WriteLogWithContext` uses the default 6.
- An empty or non-numeric `LOG_LEVEL` is treated as 0, so only Panic level is written.
- `LOG_LEVEL` is read on every call; `LOG_FORMAT` is read only once, when the logger is first used.
- Contexts exposing either `Get(string)`/`Set(string, any)` or `Get(any)`/`Set(any, any)` (the latter is `*gin.Context` in current gin) are recognized without importing gin, using the keys `CTX_ID` for the request id and `userId` for the user id.
- `log_id` is always written for non-nil contexts, including `WriteLog` (which uses `context.Background()`). When no log id or request id is stored, a new id is generated per call, so consecutive `WriteLog` lines get different ids.
- A stored request id equal to the nil UUID is treated as missing and replaced with a newly generated id.
