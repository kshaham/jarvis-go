---
name: expect-testing
description: Browser testing with expect-cli -- validate UI changes in a real headless browser using natural language test scenarios
---

# Browser Testing with expect-cli

Use `expect-cli` to validate UI changes, test user flows, and verify fixes in a real browser. The headless Chrome browser is already running and accessible.

## When to Use

- After making UI/frontend changes
- When fixing visual bugs or layout issues
- After adding new pages, forms, or interactive components
- When verifying authentication flows, navigation, or user journeys
- Before marking a frontend task as complete

## Quick Start

```bash
# Run a test with natural language
expect-cli -m "Navigate to the app, verify the homepage loads, check the nav bar has 5 links" -y

# Test a specific flow
expect-cli -m "Click login, enter test@example.com and password123, submit, verify dashboard appears" -y

# Reuse a saved flow
expect-cli -f login-flow -y
```

## Command Reference

```bash
expect-cli [options]
  -m, --message <text>   Natural language test instruction
  -f, --flow <slug>      Reuse a saved test flow by slug
  -y, --yes              Skip plan review, run immediately (headless/CI mode)
  -a, --agent <name>     Agent provider (claude, codex, copilot, gemini)
  -t, --target <type>    What to test (unstaged, branch, general)
  --verbose              Enable detailed logging
```

## Browser Environment

The headless Chrome browser is available at:
- **WebSocket:** `ws://headless-shell:9222`
- **CDP endpoint:** `http://headless-shell:9222`
- **Environment:** `EXPECT_BROWSER_WS_ENDPOINT` is pre-configured

No browser download needed -- `PUPPETEER_SKIP_DOWNLOAD=true` is set.

## Best Practices

1. **Always use `-y` flag** for automated/agent testing (no interactive prompts)
2. **Be specific** in test instructions -- mention exact elements, text, and expected outcomes
3. **Test one flow at a time** -- break complex scenarios into steps
4. **Save reusable flows** with `-f` for regression testing
5. **Check exit codes** -- 0 = pass, 1 = fail (use in CI/scripts)

## Example Test Scenarios

### Verify page loads
```bash
expect-cli -m "Navigate to http://localhost:3000, verify the page title contains 'MyApp', check there are no console errors" -y
```

### Test form submission
```bash
expect-cli -m "Go to /signup, fill in name 'Test User', email 'test@test.com', password 'Pass123!', click Submit, verify success message appears" -y
```

### Test responsive layout
```bash
expect-cli -m "Navigate to /dashboard, resize viewport to 375x667 (mobile), verify the hamburger menu appears, click it, verify navigation slides in" -y
```

### Verify API integration
```bash
expect-cli -m "Go to /api-test, click 'Fetch Data' button, wait for loading spinner to disappear, verify the data table has at least 3 rows" -y
```

## Integration with Task Workflow

After completing a UI task:
1. Run expect-cli to validate your changes
2. If tests pass: `cm task complete <taskId>`
3. If tests fail: fix the issue, re-test, then complete

```bash
# Validate then report
expect-cli -m "Verify the new feature works as expected" -y && cm task complete <taskId>
```
