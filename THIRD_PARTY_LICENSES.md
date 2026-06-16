# Third-Party Licenses

jarvis-go incorporates third-party open-source material in three distinct
ways, and this file credits all three categories:

1. **Vendored content** — source files copied into this repository. Each
   vendored file carries an attribution comment near the top of its body
   (see e.g. `skills/ecc/*/SKILL.md`).
2. **Bundled runtime dependencies** — code compiled into the shipped binary
   or bundled into the shipped SPA JavaScript. Their full license notices are
   reproduced here as required by their licenses.
3. **Integrated external tools** — programs and MCP servers that jarvis spawns
   as separate processes (e.g. via `npx`, a CLI, or a bundled binary in the
   Docker image) but does not copy into this repository. These are credited
   with a short paragraph each.

## Vendored content

### ECC — Claude Code Plugin System

**Repository:** https://github.com/affaan-m/ECC
**Vendored content:** A curated subset of skill bundles under
`skills/ecc/<name>/SKILL.md` plus selected agent + command definitions
referenced from `internal/office/claudefiles_defaults.go`.
**License:** MIT

```
MIT License

Copyright (c) 2026 Affaan Mustafa

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

### Agent Skills — 7-Step Engineering Workflow Pack

**Repository:** https://github.com/addyosmani/agent-skills
**Commit:** 3a6fc6392823e31e2362091bd4e3cddf5b77af14
**Vendored content:** 24 skill bundles under `skills/agent-skills/<name>/SKILL.md`,
7 phase slash commands under `internal/office/agentskills_commands/`, and 4
specialist agent personas under `internal/office/agentskills_agents/`. Surfaced
to agents when the team's 7-step engineering mode is enabled.
**License:** MIT

```
MIT License

Copyright (c) 2025 Addy Osmani

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## Bundled runtime dependencies

These projects are shipped inside the compiled jarvis binary or the built SPA
JavaScript bundle. Because their code is redistributed, their full license
notice is reproduced below.

### Excalidraw

**Repository:** https://github.com/excalidraw/excalidraw
**Bundled content:** The `@excalidraw/excalidraw` npm package (^0.18.1) is
compiled into the SPA whiteboard JavaScript bundle shipped with jarvis.
**License:** MIT

```
MIT License

Copyright (c) 2020 Excalidraw

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

### claude-agent-sdk-go

**Repository:** https://github.com/shaharia-lab/claude-agent-sdk-go
**Bundled content:** The Go module (v0.3.1) is compiled into the jarvis
binary. It backs the persistent Claude Code SDK agent sessions.
**License:** MIT

```
MIT License

Copyright (c) 2025 Shaharia Lab

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## Integrated external tools (not vendored)

These tools are spawned by jarvis as separate external processes or MCP
servers. Their source is not copied into this repository; they are credited
here for completeness.

### Headroom

**Repository:** https://github.com/chopratejas/headroom
jarvis runs Headroom as a per-agent context-compression MCP server
(`headroom mcp serve`), bundled in the Docker image. Licensed under the
**Apache License, Version 2.0** (Copyright 2025 Headroom Contributors); see the
upstream `LICENSE` and `NOTICE` files for the full text and conditions.

### PageIndex

**Repository:** https://github.com/VectifyAI/PageIndex
jarvis integrates PageIndex as a vectorless reasoning-RAG MCP server, launched
on demand via `npx @pageindex/mcp`. Licensed under the **MIT License**
(Copyright (c) 2025 Vectify AI); see the upstream `LICENSE` for the full text.

### CodeGraphContext

**Repository:** https://github.com/CodeGraphContext/CodeGraphContext
jarvis runs CodeGraphContext as a per-agent code-graph MCP server (`cgc index`).
Licensed under the **MIT License** (the upstream `LICENSE` carries the line
"Copyright (c) 2025-2026" without a named holder); see the upstream `LICENSE`
for the full text.

### codegraph MCP (upstream repository unpinned)

jarvis can run a separate `codegraph` MCP server — a Rust CLI knowledge-graph
tool, distinct from CodeGraphContext above. The exact upstream repository is
not pinned in the jarvis source, so no specific license is asserted here;
consult the upstream project's `LICENSE` for its terms.

### rtk

**Repository:** https://github.com/rtk-ai/rtk
jarvis uses rtk to compress bash command output via a `PreToolUse` hook.
Licensed under the **Apache License, Version 2.0** (Copyright 2024 rtk-ai and
rtk-ai Labs); see the upstream `LICENSE` and `NOTICE` files for the full text
and conditions.

## Refreshing vendored content

When pulling new versions of any vendored skill/agent/command from upstream:

1. Re-fetch the source file from the upstream repo.
2. Preserve the attribution comment block injected after the YAML
   frontmatter (the `> Sourced from ...` line).
3. Update this file with the new commit / release tag if the upstream
   license text or copyright year changes.
4. Re-run the smoke tests (`go test ./internal/office/ -run TestECCSkillBundle`).
