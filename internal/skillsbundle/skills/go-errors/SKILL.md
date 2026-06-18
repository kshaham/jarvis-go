---
name: go-errors
description: Go error handling discipline -- fmt.Errorf with %w wrapping, errors.Is / errors.As, sentinel vs typed errors, when to return vs slog.Error and swallow. Follows the patterns established in internal/web/auth and internal/state for the jarvis-go codebase.
---

# Go Error Handling

Errors are values in Go. Wrap them with context, unwrap them when you check, and pick one policy per subsystem: return or swallow. Mixing the two is what causes the "I have no idea why this failed" bugs.

## Cardinal rule

> Wrap at every layer boundary with `%w`; check with `errors.Is` / `errors.As`; pick "return" or "log-and-swallow" consciously, never by accident.

## The three policies

### Policy 1 -- Return with context (default)

Wrap the error with enough context to trace it without a debugger:

```go
data, err := os.ReadFile(path)
if err != nil {
    return fmt.Errorf("read auth config at %s: %w", path, err)
}
```

Used throughout `internal/web/auth/manager.go`, `internal/web/tls.go`. The `%w` verb preserves the underlying error so callers can still `errors.Is(err, os.ErrNotExist)`.

### Policy 2 -- Log and swallow (persistence, telemetry)

Some failures are not the caller's fault and should not propagate. The state store is the canonical example (`internal/state/store.go`):

```go
func (s *AppState) flushLocked() {
    data, err := json.MarshalIndent(s, "", "  ")
    if err != nil {
        slog.Error("Failed to marshal state", "error", err)
        return
    }
    // ... atomic write
}
```

Callers of `save()` do not get an error back. The invariant is: "best-effort persistence, we'll retry on the next save." Use this policy only when the caller genuinely cannot act on the error.

### Policy 3 -- Panic (invariant violation only)

Panic when the program is in an impossible state:

```go
if got == nil {
    panic("office.Manager: agents map was nil after initialization")
}
```

Do not panic for user input, HTTP errors, disk errors, or network failures. Those are policy-1 territory.

## Wrapping with `%w`

`%w` is the wrapping verb. It's the only one that preserves the chain. `%v` and `%s` print the string but lose the underlying error type.

```go
// YES -- preserves chain, errors.Is still works
return fmt.Errorf("load cert: %w", err)

// NO -- chain broken, callers can't unwrap
return fmt.Errorf("load cert: %v", err)
return errors.New("load cert: " + err.Error())
```

You can wrap multiple errors in Go 1.20+:

```go
return fmt.Errorf("startup failed: %w; %w", firstErr, secondErr)
```

## Checking errors

Use `errors.Is` for sentinel equality, `errors.As` for typed extraction. Never compare with `==` unless the package explicitly guarantees sentinel identity.

```go
// Sentinel check
if errors.Is(err, os.ErrNotExist) {
    return defaultConfig(), nil
}

// Typed extraction
var pathErr *fs.PathError
if errors.As(err, &pathErr) {
    slog.Warn("path error", "op", pathErr.Op, "path", pathErr.Path)
}
```

## Sentinel errors vs typed errors

**Sentinel** (an exported variable):
```go
package office
var ErrAgentNotFound = errors.New("agent not found")
```
Use when the condition is binary and callers just want to branch on it.

**Typed** (a struct implementing `error`):
```go
type TaskBlockedError struct {
    TaskID string
    Reason string
}
func (e *TaskBlockedError) Error() string {
    return fmt.Sprintf("task %s blocked: %s", e.TaskID, e.Reason)
}
```
Use when callers need to read fields off the error (task ID, status code, etc.).

## Worked example: an HTTP handler

From the pattern in `internal/web/handler/office.go`:

```go
func (ctx *Context) SomeHandler(w http.ResponseWriter, r *http.Request) {
    var req someRequest
    if !decodeBody(w, r, &req) {
        return // decodeBody already wrote 400
    }
    agent := ctx.OfficeManager.GetAgent(req.SessionID)
    if agent == nil {
        request.WriteError(w, http.StatusNotFound, "agent not found")
        return
    }
    result, err := someOperation(r.Context(), agent, req.Input)
    if err != nil {
        slog.Error("someOperation failed",
            "error", err, "sessionId", req.SessionID)
        request.WriteError(w, http.StatusInternalServerError, "operation failed")
        return
    }
    ok(w, result)
}
```

Notes:
- The handler **never** returns the raw error string to the client (information leak).
- It **logs** the wrapped error for debugging.
- It **returns** a generic message over HTTP.
- `someOperation` uses Policy 1 to bubble the real cause up.

## Anti-patterns

- **`if err != nil { return err }`** with no context. You lose the trail. Wrap with `%w`.
- **Empty catch blocks / `_ = err`.** If you're ignoring the error, leave a one-line `//` comment explaining why.
- **`err.Error() == "..."`.** Use `errors.Is` or a sentinel.
- **Logging `err` inside every layer.** One log per error, at the layer that owns the policy decision. Otherwise one failure shows up as five log lines.
- **Returning `nil` error with a nil result.** Pick one: err!=nil OR result!=nil. Never both nil unless absence is a valid success.
- **`panic(err)` because it was convenient.** Panic is for invariant violations; user-visible failures are errors.

## Verification

After any error-handling change:
1. `go vet ./...` -- catches `printf` mismatches on `%w`.
2. Search for `%v", err` and `errors.New(` near existing wrapping and decide if they should be `%w`.
3. Walk from any failing test's error message back to the original `fmt.Errorf` -- the chain should read top-down.
