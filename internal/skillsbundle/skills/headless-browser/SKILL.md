---
name: headless-browser
description: Use the always-on chromedp/headless-shell container for visual UI verification, screenshots, and natural-language browser tests. Trigger when you need to verify a frontend change actually renders, capture an image of a page for review, run a quick UI smoke test, or automate clicks/fills in a real Chrome instance over the Chrome DevTools Protocol.
---

# Headless Browser (chromedp/headless-shell)

A real Chromium runs as a sibling Docker container next to Jarvis, exposing the Chrome DevTools Protocol on `ws://headless-shell:9222`. It is **always running** -- you do not need to start, stop, or check for it. Use it whenever you need to verify a frontend change rendered correctly, take a screenshot of a page, or drive a real browser to test UI behavior.

## When to use this skill

Use it when you need to:

- **Verify frontend work**: prove a CSS/JS change actually renders by capturing a screenshot
- **Smoke test a UI flow**: navigate, click, type, assert a result
- **Capture evidence for a code review or task completion**
- **Investigate visual bugs** that you can't reproduce by reading code

Do **not** use it for:

- Static HTML validation -- read the HTML directly
- Backend API testing -- use `curl` or write a Go test instead
- Anything that doesn't actually involve a rendered DOM

## Tools available

The Jarvis container ships with three pre-installed tools that all talk to the same headless-shell endpoint:

| Tool | Best for | Invocation |
|---|---|---|
| `browser-screenshot` | One-shot PNG capture of a URL | `browser-screenshot <url> [output.png]` |
| `expect-cli` | Natural-language UI assertions | `expect-cli -m "<test description>" -y` |
| `puppeteer-core` (Node) | Custom browser automation scripts | Write a `.mjs` and `puppeteer.connect({ browserWSEndpoint })` |

The CDP endpoint is exposed via env vars in every agent shell:

```
HEADLESS_BROWSER_HOST=headless-shell
HEADLESS_BROWSER_PORT=9222
EXPECT_BROWSER_WS_ENDPOINT=ws://headless-shell:9222
```

## Quick recipes

### 1. Screenshot a page (most common)

```bash
browser-screenshot http://localhost:3555/office.html /tmp/office.png
```

The script connects to headless-shell over CDP, navigates to the URL, waits for `domcontentloaded` plus a 1-second settle for SPA rendering, and saves a 1280x720 PNG. Exit code 0 = success.

After capturing, **read the image with the Read tool** to actually inspect what was rendered. A screenshot you don't look at proves nothing.

### 2. Natural-language UI test

```bash
expect-cli -m "Open http://localhost:3555/office.html, log in, and verify the office tab is visible" -y
```

`expect-cli` uses an LLM to drive the browser. The `-y` flag is required for non-interactive runs. Exit code 0 = pass, 1 = fail. Use this when you want to verify a flow rather than just capture state.

### 3. Custom puppeteer-core script

Write a small `.mjs` script when you need precise control (multiple steps, DOM assertions, network interception):

```javascript
import puppeteer from 'puppeteer-core';
import { lookup } from 'node:dns/promises';

const hostname = process.env.HEADLESS_BROWSER_HOST || 'headless-shell';
const port = process.env.HEADLESS_BROWSER_PORT || '9222';

// IMPORTANT: chromedp/headless-shell rejects CDP requests whose Host
// header is not an IP or "localhost" (DNS rebinding protection). Always
// resolve the service name to an IP and use the IP from here on.
const { address: ip } = await lookup(hostname);
const v = await (await fetch(`http://${ip}:${port}/json/version`)).json();
const wsEndpoint = (v.webSocketDebuggerUrl || `ws://${ip}:${port}`)
  .replace(`//${hostname}:`, `//${ip}:`);
const browser = await puppeteer.connect({ browserWSEndpoint: wsEndpoint });

try {
  const page = await browser.newPage();
  await page.setViewport({ width: 1280, height: 720 });
  await page.goto('http://localhost:3555/office.html', { waitUntil: 'networkidle2' });

  const title = await page.title();
  console.log('Title:', title);

  // Drive interactions
  await page.click('#someButton');
  await page.waitForSelector('.result', { timeout: 5000 });

  await page.screenshot({ path: '/tmp/after-click.png' });
  await page.close();
} finally {
  browser.disconnect(); // disconnect, don't close -- it's a shared container
}
```

Run with `node your-script.mjs`.

## Critical rules

1. **Disconnect, don't close**. The headless-shell container is shared across the office. Always call `browser.disconnect()`, **never** `browser.close()` -- closing kills the container's only browser instance.
2. **Address the container by IP, not hostname**. chromedp/headless-shell enforces DNS rebinding protection: any request whose Host header is not an IP address (or `localhost`) is rejected with `Host header is specified and is not an IP address or localhost.` Use `dns.lookup('headless-shell')` to resolve the IP, then build URLs with that IP. The pre-installed `browser-screenshot` and `expect-cli` already handle this.
3. **Resolve the WebSocket URL via `/json/version`**. Don't hardcode the WS path -- fetch `http://<ip>:9222/json/version` and use the returned `webSocketDebuggerUrl` (rewriting the host portion to your resolved IP).
4. **headless-shell is reachable inside the compose network only**. The hostname `headless-shell` works from inside the jarvis container (where you are). It does **not** work from the host or from outside containers.
5. **No browser launch**. Do not call `puppeteer.launch()` or install Chromium yourself. The container is already running -- just connect.
6. **Verify after capturing**. After running `browser-screenshot`, immediately Read the resulting PNG. A screenshot you didn't look at is not evidence.

## Troubleshooting

**`fetch failed` or `ECONNREFUSED`**: headless-shell isn't reachable. From the jarvis container, run `curl http://headless-shell:9222/json/version` -- if that fails, the sibling container is down (rare; docker-compose restarts it automatically). Report the failure rather than retrying in a loop.

**`Navigation timeout exceeded`**: the page didn't reach `domcontentloaded` within 15s. Either the URL is wrong, the server is down, or the page is slow. Try the URL with `curl` first to confirm it returns a 2xx.

**Blank screenshot**: SPA hasn't rendered yet. Increase the settle delay in a custom script (`await new Promise(r => setTimeout(r, 3000))`) or use `waitUntil: 'networkidle2'`.

**`Target closed`**: you called `browser.close()` somewhere. Don't. Use `disconnect()`.

## Verification checklist

Before claiming a UI task is done:

- [ ] Captured a screenshot **after** the change with `browser-screenshot`
- [ ] Read the screenshot file to confirm it shows the expected state
- [ ] If the change is interactive, ran an `expect-cli` smoke test or puppeteer script
- [ ] Exit code was 0
