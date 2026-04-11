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

import puppeteer from 'puppeteer-core';

const url = process.argv[2];
const outputPath = process.argv[3] || '/tmp/headless-shell-screenshot.png';
const host = process.env.HEADLESS_BROWSER_HOST || 'headless-shell';
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
    // Resolve the actual webSocketDebuggerUrl from the CDP /json/version
    // endpoint -- the value baked into our env may not include the path.
    const versionRes = await fetch(`http://${host}:${port}/json/version`);
    if (!versionRes.ok) {
      throw new Error(`CDP /json/version returned HTTP ${versionRes.status}`);
    }
    const versionData = await versionRes.json();
    const wsEndpoint = versionData.webSocketDebuggerUrl || `ws://${host}:${port}`;

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
    console.error(`Is headless-shell running? Check: curl http://${host}:${port}/json/version`);
    process.exit(1);
  } finally {
    if (browser) {
      browser.disconnect();
    }
  }
}

takeScreenshot();
