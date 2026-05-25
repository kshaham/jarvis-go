---
name: office-focused-search
description: Tool routing for office agents searching code -- ast-grep for structural patterns, Drift/Gnapsis for pattern intelligence, Grep for plain text, Glob for file paths. Always set head_limit, prefer files_with_matches, never cat files larger than 300 lines. Search-then-read, not browse.
---

# Office Focused Search

The single biggest source of context bloat for office agents is **browsing the codebase instead of searching it**. This skill is the routing table.

## The rule

> Search first. Read only the file you actually need. Read only the lines you actually need. Never `cat` a file larger than 300 lines.

## Tool routing table

| You want to find... | Use this | Don't use this |
|---|---|---|
| Files matching a name pattern (`*.tsx`, `*_test.go`) | `Glob` | `find`, `ls -R` |
| A literal string (`TODO`, error message, env var name) | `Grep` with `files_with_matches` | `grep -r`, `cat \| grep` |
| A code structure (`func Foo`, `type X struct`, all React hooks) | `mcp__ast-grep__find_code` | text Grep with regex |
| "All callers of X" / "everything that imports Y" | `mcp__drift__*` or `mcp__gnapsis__find_references` | manual grep |
| Symbol definitions in a single file | `Read` with `offset` + `limit` | `Read` whole file |
| The contents of a SKILL.md or CLAUDE.md you know exists | `Read` | `Grep` then `Read` |

## Required defaults

For every search command, set defaults that bound the cost:

### Grep
- **Default mode: `files_with_matches`.** Get the list of files first; only switch to `content` after you've narrowed.
- **Always set `head_limit`** if you suspect more than ~30 hits. `head_limit: 30` is usually plenty.
- Use `glob` or `type` to narrow (`type: "go"`, `glob: "*.tsx"`).
- For multi-line patterns, set `multiline: true` -- otherwise `.` won't cross newlines.

### Glob
- Be specific. `**/*.go` is fine. `**/*` is a code smell -- use a better pattern.
- Add a `path:` to scope to a directory.

### ast-grep
- For structural queries that text grep can't handle. Examples: "find all Go interfaces with one method," "find all React components without a key prop in a `.map`."
- The MCP tools are `mcp__ast-grep__find_code` and `mcp__ast-grep__find_code_by_rule`.
- Cheaper than `Grep` because it only returns AST matches, not entire files.

### Read
- **Never read a file > 300 lines without `offset`/`limit`.** If you don't know the line range, search first to find it.
- After reading, write the relevant facts to memory immediately (`cm memory append`).

## Worked examples

### "Find where the login route is wired"

Bad:
```
ls -R internal/        # 5KB of context wasted
Read internal/web/server.go     # 800-line file
```

Good:
```
Grep pattern="login" path="internal/web/server.go" output_mode="content" -n=true head_limit=10
```

You get back 10 lines with line numbers. Now `Read` only the relevant function.

### "Find all places that call SaveAgentMemory"

Bad: regex grep across the whole repo, then read 4 large files.

Good:
```
Grep pattern="SaveAgentMemory" type="go" output_mode="files_with_matches" head_limit=20
```

Then for each file: a tightly-bounded `Read` or another `Grep` with `-n` to find the line.

### "Find all Go functions named Build*Section"

Bad:
```
Grep pattern="func Build.*Section" type="go" output_mode="content"
```
Works, but if there are 50 hits you've burned context on the function bodies.

Good:
```
mcp__ast-grep__find_code language="go" pattern="func Build$NAME($$$) string"
```
Returns AST matches with file:line. Cheaper, more precise.

### "What does the existing test for X look like?"

Bad: `ls test/`, then `cat` three files.

Good:
```
Glob pattern="**/x_test.go"
# Pick one
Read offset=1 limit=80 file_path="..."
```

## When you DO need to read a big file

Sometimes the file you need is large and you legitimately need to understand its shape. The disciplined sequence:

1. `Grep -n` to find section headers / function signatures.
2. `Read` only the headers section first (`offset=0 limit=80`).
3. From the headers, decide which 1-2 functions you actually care about.
4. `Read` each function with a tight `offset`/`limit`.
5. **Write a one-paragraph summary to memory** so you don't have to do this again:
   ```
   cm memory append "internal/foo/big.go (800 lines): main entry is FooHandler at L120, RegisterRoutes at L300, helper TrimX at L600. Authoritative impl of foo logic."
   ```

## Anti-patterns (these are real ones we've seen)

- **`git log` with no `-N` or `--oneline`.** Costs 5-15% of context. Use `git log --oneline -20`.
- **Reading a config file in full to find one key.** Use `Grep pattern="<key>" output_mode="content" -n=true`.
- **`cat $(find . -name '*.go')`.** Just don't.
- **Re-running the same search later.** Write the result to memory the first time.
- **Reading an entire test file to copy one assertion pattern.** Use `Grep` to find the pattern; `Read` only that block.

## Verification

For every code-search task, you should be able to answer "how many bytes did I add to my context?" with a small number. If you don't know, you didn't constrain your tool calls enough.
