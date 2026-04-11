#!/usr/bin/env node
/**
 * Browser Screenshot (chromedp/headless-shell)
 *
 * Captures a screenshot of a URL using the always-running headless-shell
 * container that lives alongside Jarvis in docker-compose. Agents call
 * this from inside the jarvis container; the headless-shell sibling is
 * reachable at ws://headless-shell:9222 over the compose network.
 *
 * Usage:
 *   browser-screenshot <url> [output-path]
 *
 * Env:
 *   HEADLESS_BROWSER_HOST  hostname of the headless-shell container
 *                          (default: headless-shell)
 *   HEADLESS_BROWSER_PORT  CDP port (default: 9222)
 *
 * Example:
 *   browser-screenshot http://localhost:3555/office.html /tmp/office.png
 *
 * Requires: puppeteer-core (installed globally in the jarvis container).
 */

import { createRequire } from 'node:module';
import { lookup } from 'node:dns/promises';

// puppeteer-core is installed globally in the jarvis container, not in
// any local node_modules near this script. ESM `import` does not search
// global paths -- but createRequire() uses CommonJS resolution which
// honors NODE_PATH (set by the bash wrapper to `npm root -g`).
const require = createRequire(import.meta.url);
const puppeteer = require('puppeteer-core');

const url = process.argv[2];
const outputPath = process.argv[3] || '/tmp/headless-shell-screenshot.png';
const hostname = process.env.HEADLESS_BROWSER_HOST || 'headless-shell';
const port = process.env.HEADLESS_BROWSER_PORT || '9222';

if (!url) {
  console.error('Usage: browser-screenshot <url> [output-path]');
  process.exit(1);
}

const PAGE_LOAD_SETTLE_MS = 1000;
const NAV_TIMEOUT_MS = 15000;

async function takeScreenshot() {
  let browser;
  try {
    // chromedp/headless-shell rejects CDP requests whose Host header is
    // not an IP or "localhost" (DNS rebinding protection). Resolve the
    // service name once and address the container by IP from here on so
    // every fetch + websocket carries an IP-based Host header.
    const { address: ip } = await lookup(hostname);
    const versionRes = await fetch(`http://${ip}:${port}/json/version`);
    if (!versionRes.ok) {
      throw new Error(`CDP /json/version returned HTTP ${versionRes.status}`);
    }
    const versionData = await versionRes.json();
    let wsEndpoint = versionData.webSocketDebuggerUrl || `ws://${ip}:${port}`;
    // Some chrome builds bake the hostname into webSocketDebuggerUrl --
    // rewrite to the resolved IP so the puppeteer WS handshake also
    // sends an IP Host header.
    wsEndpoint = wsEndpoint.replace(`//${hostname}:`, `//${ip}:`);

    browser = await puppeteer.connect({ browserWSEndpoint: wsEndpoint });

    const page = await browser.newPage();
    await page.setViewport({ width: 1280, height: 720 });
    await page.goto(url, { waitUntil: 'domcontentloaded', timeout: NAV_TIMEOUT_MS });

    // Give SPAs a moment to render after DOMContentLoaded.
    await new Promise(r => setTimeout(r, PAGE_LOAD_SETTLE_MS));

    await page.screenshot({ path: outputPath, fullPage: false });
    console.log(`Screenshot saved to ${outputPath}`);

    await page.close();
  } catch (error) {
    console.error(`Failed to take screenshot: ${error.message}`);
    console.error(`Diagnose: getent hosts ${hostname} && curl -s http://$(getent hosts ${hostname} | awk '{print $1}'):${port}/json/version`);
    process.exit(1);
  } finally {
    if (browser) {
      browser.disconnect();
    }
  }
}

takeScreenshot();
