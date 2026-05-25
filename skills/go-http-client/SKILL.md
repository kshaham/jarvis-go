---
name: go-http-client
description: Go HTTP client discipline -- always set a Timeout, always pass a context, always close the response body, parse status codes before bodies, retry with exponential backoff when idempotent. Matches the hand-rolled http.Client style in cmd/cm/main.go and cmd/jarvis-mcp/api.go.
---

# Go HTTP Client

The jarvis-go repo hand-rolls HTTP clients instead of using a library. Every call site follows a narrow recipe. This skill is the recipe.

## Cardinal rule

> `http.Client{}` with no timeout is a goroutine leak waiting to happen. Always set a timeout. Always close the body. Always check the status code before the body.

## The three rules, enforced

1. **Timeout**. `http.Client{Timeout: 15 * time.Second}` at minimum. `cmd/cm/main.go:25` and `cmd/jarvis-mcp/api.go` already follow this.
2. **Context**. Use `http.NewRequestWithContext` so request cancellation flows through.
3. **Close the body**. `defer resp.Body.Close()` immediately after the error check. Missing closes leak connections from the pool until you hit the OS file-descriptor limit.

## The recipe

```go
var client = &http.Client{Timeout: 15 * time.Second}

func doPost(ctx context.Context, url string, body map[string]interface{}) ([]byte, error) {
    data, err := json.Marshal(body)
    if err != nil {
        return nil, fmt.Errorf("marshal body: %w", err)
    }
    req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
    if err != nil {
        return nil, fmt.Errorf("build request: %w", err)
    }
    req.Header.Set("Content-Type", "application/json")

    resp, err := client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("POST %s: %w", url, err)
    }
    defer resp.Body.Close()

    respBody, err := io.ReadAll(resp.Body)
    if err != nil {
        return nil, fmt.Errorf("read response: %w", err)
    }
    if resp.StatusCode >= 400 {
        return nil, fmt.Errorf("POST %s: HTTP %d: %s", url, resp.StatusCode, string(respBody))
    }
    return respBody, nil
}
```

Note the order of operations:

1. Build request with context.
2. Set headers.
3. `client.Do` + immediate defer close.
4. Read body.
5. **Check status AFTER reading** -- the body contains the error message and you need it for logs.
6. Return wrapped error with URL + status code.

The `cm` binary uses a simpler version of this (`cmd/cm/main.go:post` and `:get`). Match that style when adding new calls.

## Reuse one client, not many

`http.Client` is safe for concurrent use. Create it **once** as a package-level var and share it. Do NOT `&http.Client{}` inside every call -- you'll skip connection pooling.

```go
// YES
var apiClient = &http.Client{Timeout: 15 * time.Second}

func call1(...) { ... apiClient.Do(req) ... }
func call2(...) { ... apiClient.Do(req) ... }

// NO
func call1(...) {
    c := &http.Client{Timeout: 15 * time.Second} // fresh per-call: no pooling
    ...
}
```

## Retries with exponential backoff

Retry idempotent calls only. A GET is idempotent. A POST that creates a resource is NOT -- retrying creates duplicates.

```go
func getWithRetry(ctx context.Context, url string) ([]byte, error) {
    var lastErr error
    backoff := 200 * time.Millisecond
    for attempt := 0; attempt < 3; attempt++ {
        if attempt > 0 {
            select {
            case <-time.After(backoff):
            case <-ctx.Done():
                return nil, ctx.Err()
            }
            backoff *= 2
        }
        body, err := doGet(ctx, url)
        if err == nil {
            return body, nil
        }
        // Only retry on transient errors (network, 5xx).
        if !isRetriable(err) {
            return nil, err
        }
        lastErr = err
    }
    return nil, fmt.Errorf("after 3 attempts: %w", lastErr)
}
```

`isRetriable` should return true for network errors and 502/503/504, false for 4xx. Do NOT retry on 400/404.

## Transport tuning (rarely needed)

The default `http.DefaultTransport` is tuned for most workloads. Only override `Transport` when you need:
- **Disabled keep-alive** for a short-lived debug tool.
- **TLS client certs** (use `Transport.TLSClientConfig`).
- **Custom dialer** for unix sockets.
- **Connection pool limits** (`MaxIdleConns`, `MaxIdleConnsPerHost`) when talking to a single heavy backend.

For everything else, the default is fine.

## Streaming large responses

Do NOT `io.ReadAll` a gigabyte. For large or streaming bodies:

```go
resp, err := client.Do(req)
if err != nil { return err }
defer resp.Body.Close()
if resp.StatusCode >= 400 {
    // Body might be small error; read a bounded prefix.
    prefix, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
    return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(prefix))
}
// Stream into a file
out, err := os.Create(dest)
if err != nil { return err }
defer out.Close()
_, err = io.Copy(out, resp.Body)
return err
```

## Anti-patterns

- **`http.Get(url)`** or **`http.Post(url, ...)`** -- these use `http.DefaultClient` which has NO timeout. Always use an explicit `http.Client` with a timeout.
- **`defer resp.Body.Close()`** BEFORE the `err != nil` check. If `resp` is nil, you segfault. Close only after the error check.
- **Ignoring the body on non-2xx.** You lose the error message. Read it (with a limit) and include it in the returned error.
- **Unlimited retries.** Cap at 3 with backoff. Otherwise you DDoS yourself during an outage.
- **Retrying non-idempotent POSTs.** Leads to duplicate creations.
- **Passing `context.Background()` from an HTTP handler.** Pass `r.Context()` so client disconnects propagate.
- **Hand-parsing JSON with `strings.Contains`.** `json.Unmarshal` into a struct or `map[string]any`.

## Verification

1. `go vet ./...` catches `rsp.Body` misuse via linters.
2. Kill the remote and confirm your client returns within `Timeout` seconds (not hangs forever).
3. Run the client under `-race` to catch shared-state bugs.
4. Watch `lsof -p <pid> | grep TCP` while the client is active -- if connections pile up, you forgot to close a body somewhere.
