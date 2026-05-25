---
name: go-http-server
description: HTTP server patterns for jarvis-go specifically -- chi/v5 router, handler Context dependency injection, decodeBody/ok/WriteError envelope, SSE streaming via the existing Broker, panic recovery middleware, graceful shutdown. Every example maps to a file in internal/web/.
---

# Go HTTP Server (jarvis-go flavor)

jarvis-go is a `net/http` + `chi/v5` server. This skill is the in-repo handler recipe. Follow it exactly when adding new endpoints so the 60+ existing handlers stay consistent.

## Cardinal rule

> One handler method per endpoint. One `ctx *Context` receiver. Use `decodeBody` / `ok` / `WriteError`. Never `http.Error` the raw error.

## The shape

Every handler in `internal/web/handler/office.go` follows this shape:

```go
type myRequest struct {
    SessionID string `json:"sessionId"`
    Value     string `json:"value"`
}

// Package comment for go doc -- short, present tense.
func (ctx *Context) MyHandler(w http.ResponseWriter, r *http.Request) {
    var req myRequest
    if !decodeBody(w, r, &req) {
        return // decodeBody already wrote a 400
    }
    agent := ctx.OfficeManager.GetAgent(req.SessionID)
    if agent == nil {
        request.WriteError(w, http.StatusNotFound, "agent not found")
        return
    }
    // ... do the thing
    ok(w, map[string]interface{}{"result": "..."})
}
```

Then register it in `internal/web/server.go` in the matching `Route` group:

```go
r.Post("/my-thing", h.MyHandler)
```

That's the entire contract. No per-handler middleware, no custom response writers, no `http.Error`.

## The three response helpers

Defined in `internal/web/handler/` and used in every handler:

- `decodeBody(w, r, &req)` -- JSON-decode into `req`, return false + write 400 on failure
- `ok(w, payload)` -- 200 + JSON-encode payload
- `request.WriteError(w, status, msg)` -- non-2xx + JSON error envelope `{error: msg}`

If you find yourself calling `http.Error` or `json.NewEncoder(w).Encode(err.Error())`, stop -- you're about to diverge from the established pattern. Use the helpers.

## Middleware composition

The router is assembled in `internal/web/server.go`:

```go
r := chi.NewRouter()
r.Use(middleware.Recovery())       // internal/web/middleware/recovery.go
r.Use(middleware.SecurityHeaders()) // HSTS, XCTO, XFO
r.Use(mw.RateLimit(rateLimitCfg))  // internal/web/middleware/ratelimit.go
// ... then route groups
```

Middleware is applied at the router level, not per-handler. If your endpoint needs bespoke behavior (e.g. streaming), handle it in the handler itself. Don't invent new middleware without a good reason.

### Authenticated routes

```go
r.Group(func(r chi.Router) {
    r.Use(mw.Auth(s.authManager))
    // protected routes here
})
```

Mirror this pattern. Never check auth inside individual handlers.

## Dependency injection via `Context`

`handler.Context` (in `internal/web/handler/context.go`) holds ~15 maps and sub-managers. New handlers receive everything they need by calling `ctx.SomeManager.SomeMethod(...)`.

When adding a new dependency:
1. Add a field to `Context` struct
2. Populate it in `NewContext` / `server.go` wiring
3. If it's a lazy map keyed by session, follow the double-check pattern (see `GetOrCreateRespawnController` in context.go and the `go-concurrency` skill)

## SSE streaming

The repo has a shared `sse.Broker` (`internal/web/sse/broker.go`). New event types just call `broker.Broadcast(eventName, payload)` and any subscribed client receives it.

For handlers that need to stream a response directly (not via broker), use the standard SSE flush pattern:

```go
w.Header().Set("Content-Type", "text/event-stream")
w.Header().Set("Cache-Control", "no-cache")
w.Header().Set("Connection", "keep-alive")
flusher, ok := w.(http.Flusher)
if !ok {
    request.WriteError(w, http.StatusInternalServerError, "streaming unsupported")
    return
}
for event := range events {
    fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Name, event.JSON)
    flusher.Flush()
    if r.Context().Err() != nil {
        return // client went away
    }
}
```

Always check `r.Context().Err()` between writes -- clients disconnect silently.

## Panic recovery

`middleware.Recovery()` catches panics, logs the stack, and returns 500. You do NOT need `defer recover()` inside handlers. Let the middleware handle it.

## Graceful shutdown

From `cmd/jarvis/main.go`:

```go
srv := &http.Server{Addr: addr, Handler: r}
go func() {
    if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
        slog.Error("server failed", "error", err)
    }
}()

// ... on signal:
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
_ = srv.Shutdown(ctx)
```

`Shutdown` drains in-flight requests. Don't call `Close` -- it drops mid-request clients.

## Worked example: adding a new office endpoint

Say you're adding `POST /api/office/agent/thing`:

1. **Request type** in `internal/web/handler/office.go` near other request types:
   ```go
   type agentThingRequest struct {
       SessionID string `json:"sessionId"`
       Payload   string `json:"payload"`
   }
   ```
2. **Handler method**:
   ```go
   func (ctx *Context) OfficeAgentPostThing(w http.ResponseWriter, r *http.Request) {
       var req agentThingRequest
       if !decodeBody(w, r, &req) {
           return
       }
       agent := ctx.OfficeManager.GetAgent(req.SessionID)
       if agent == nil {
           request.WriteError(w, http.StatusNotFound, "agent not found")
           return
       }
       if err := office.DoTheThing(agent.ConfigID, req.Payload); err != nil {
           slog.Error("DoTheThing failed", "error", err, "session", req.SessionID)
           request.WriteError(w, http.StatusInternalServerError, "operation failed")
           return
       }
       ok(w, nil)
   }
   ```
3. **Route registration** in `internal/web/server.go`:
   ```go
   r.Post("/thing", h.OfficeAgentPostThing)
   ```
4. **cm command** (if agent-facing) in `cmd/cm/main.go` handleSomething case.
5. **Test** in `internal/web/handler/office_test.go` using the same pattern as existing tests.

## Anti-patterns

- **`http.Error(w, err.Error(), 500)`** -- leaks internal error strings. Use `request.WriteError`.
- **`defer r.Body.Close()`** in a handler -- `net/http` already closes request bodies.
- **Per-handler mutex fields on `Context`** -- go broader and put the mutex on the sub-manager, not on `Context`.
- **Blocking in a handler without passing `r.Context()`** -- leaks goroutines if the client disconnects.
- **Inventing a new response envelope** per endpoint -- consistency across 60+ handlers beats individual cleverness.

## Verification

1. `go build ./...` after registration.
2. `go test ./internal/web/handler/...` -- the test pattern lives there.
3. `curl -sk https://localhost:3556/api/office/agent/thing -X POST -d '{"sessionId":"x","payload":"y"}'` against a running container. A 200 with `{}` or a proper 4xx with `{error:...}` means the envelope is right.
4. Attach to a running agent via web UI and invoke via `cm` to verify the agent-facing side still works.
