# Third-Party Licenses

jarvis-go vendors content from the following open-source projects. Each
vendored file carries an attribution comment near the top of its body
(see e.g. `skills/ecc/*/SKILL.md`).

## ECC — Claude Code Plugin System

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

## Agent Skills — 7-Step Engineering Workflow Pack

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

## Refreshing vendored content

When pulling new versions of any vendored skill/agent/command from upstream:

1. Re-fetch the source file from the upstream repo.
2. Preserve the attribution comment block injected after the YAML
   frontmatter (the `> Sourced from ...` line).
3. Update this file with the new commit / release tag if the upstream
   license text or copyright year changes.
4. Re-run the smoke tests (`go test ./internal/office/ -run TestECCSkillBundle`).
