---
name: shell-scripting-safe
description: Safe bash for office agents -- set -euo pipefail, IFS hardening, quoting rules, mktemp + trap cleanup, exit codes, portable command substitution, avoiding word splitting. Agents run bash constantly via the Bash tool and a single unquoted variable can nuke a worktree. This is the checklist.
---

# Safe Shell Scripting

Office agents invoke bash on nearly every task -- `cm`, `git`, `go`, `docker`, `browser-screenshot`, `jq`, pipelines. Bash is sharp. This skill is the habits that prevent agents from shooting themselves.

## Cardinal rule

> Every script starts with `set -euo pipefail`, every variable expansion is quoted, every temp file is created with `mktemp` and cleaned up with `trap`. No exceptions for "quick scripts."

## The prologue every script needs

```bash
#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'
```

- `set -e` -- exit on any error (including inside pipelines via `-o pipefail`).
- `set -u` -- error on unset variables. `$FOO` when FOO is unset becomes `exit 1`, not silently empty.
- `set -o pipefail` -- a failing command in a pipeline fails the whole pipeline. Without this, `false | true` has exit 0.
- `IFS=$'\n\t'` -- default IFS includes space, which causes words in paths to split. Restricting to newline+tab prevents surprises in `for` loops.

One-liners run in the Bash tool get the same benefit by prefixing: `set -euo pipefail; ...`.

## Quote. Every. Variable.

```bash
# NO -- if $file contains spaces or * or newlines, you've lost
rm $file
cp $src $dst
for f in $files; do ...; done

# YES
rm "$file"
cp "$src" "$dst"
for f in "${files[@]}"; do ...; done
```

For arrays, use `"${arr[@]}"` (each element becomes one word) not `"${arr[*]}"` (all elements joined by IFS[0]).

Command substitution:
```bash
# NO -- word-splits the output
files=$(ls)

# YES -- captures as a single string, then explicit split if needed
files=$(ls)
# or for an array:
mapfile -t files < <(ls)
```

File-name arithmetic is where agents destroy worktrees. The examples above are not hypothetical -- a filename like `agent id.txt` or `-rf /` will ruin your day if you don't quote.

## Temp files with mktemp + trap

```bash
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT
# ... use $tmp ...
```

For a temp directory:
```bash
tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT
```

The `trap ... EXIT` runs on normal exit, `set -e` failure, and most signals. It's the only cleanup primitive you need for 99% of scripts.

Never use `/tmp/myscript-$$` or `/tmp/out.txt`. `mktemp` creates atomically and unique; hardcoded temp paths race with other processes.

## Exit codes

- `exit 0` -- success.
- `exit 1` -- generic failure.
- `exit 2` -- command-line usage error.
- `exit 126` -- command found but not executable.
- `exit 127` -- command not found.
- `exit 130` -- killed by SIGINT (Ctrl-C).

Check the exit code of the last command with `$?`:
```bash
if some-command; then echo ok; else echo "failed with $?"; fi
```

Avoid `command && other` if you care about which one failed -- use an explicit `if` block.

## Portable command substitution

Modern: `$(command)`. Old-style backticks `\`command\`` are deprecated -- they don't nest and have different quoting rules.

```bash
# YES
today=$(date +%Y-%m-%d)
files=$(find . -name "*.go" | wc -l)

# NO
today=`date +%Y-%m-%d`
```

## Reading a file into a variable

```bash
contents=$(cat file.txt)    # strips trailing newlines -- usually fine
```

Better for preserving bytes:
```bash
contents=$(< file.txt)       # same, but a tiny bit faster
```

Line-by-line:
```bash
while IFS= read -r line; do
    echo "got: $line"
done < file.txt
```

The `IFS= read -r` combo is the only way to read lines intact: `IFS=` preserves leading whitespace, `-r` preserves backslashes.

## Conditionals and comparisons

Use `[[ ... ]]` not `[ ... ]` -- it's bash-specific but much safer:

```bash
# YES
if [[ -f "$file" ]]; then ...; fi
if [[ "$a" == "$b" ]]; then ...; fi
if [[ "$string" =~ ^[0-9]+$ ]]; then ...; fi

# NO (POSIX sh, more quoting pitfalls)
if [ -f $file ]; then ...; fi
```

For numeric: `[[ "$a" -lt "$b" ]]` or `(( a < b ))`.

## Heredocs for multi-line content

Quoted `<<'EOF'` disables variable substitution (use for config files with literal `$`); unquoted `<<EOF` does substitute `$vars`. Pick based on whether you want the body rendered as a template or as a literal.

## The danger list (commands to double-check)

- **`rm -rf "$path"`** -- what if `$path` is empty? `set -u` catches unset, but `path=""` passes `-u`. Use `[[ -n "$path" ]] || exit 1` before `rm -rf`.
- **`eval`** -- don't. If you think you need `eval`, you need a different language.
- **`sudo`** -- office agents run as `jarvis` and shouldn't need sudo. If a command suggests sudo, stop and ask.
- **`curl | bash`** -- never pipe remote code directly to a shell. Download, inspect, then run.
- **`find ... -exec rm`** -- use `-delete` instead when possible, and always `find` first to see what you're about to delete.
- **`git push --force`** -- see `git-advanced` skill.

## Worked example: safe archive-and-upload script

```bash
#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

[[ $# -eq 2 ]] || { echo "usage: $0 <src-dir> <dest-url>" >&2; exit 2; }
src="$1"; dest="$2"
[[ -d "$src" ]] || { echo "not a directory: $src" >&2; exit 1; }

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

archive="$tmpdir/archive.tar.gz"
tar -czf "$archive" -C "$src" .
curl --fail --silent --show-error -X POST --data-binary "@$archive" "$dest"
echo "uploaded $archive to $dest"
```

Key elements: prologue, usage check with exit code, quoted vars, `mktemp -d` + `trap` cleanup, `curl --fail` so HTTP errors propagate.

## Anti-patterns

- **No prologue.** "It's just a one-liner" -- until the variable is empty.
- **Unquoted `$variables`.** One path with a space destroys your script.
- **Hardcoded temp paths.** Race conditions + permission issues.
- **`ls | grep foo`.** Parse `ls` output is unsafe; use `find` or globbing.
- **`while read` in a pipeline** without thinking about subshells. The loop runs in a subshell, so variable changes don't propagate out.
- **`if [ $var == foo ]`** without quoting. If `$var` is empty, becomes `if [ == foo ]` -- syntax error.
- **Ignoring exit codes in pipelines.** Without `set -o pipefail`, `false | true` passes.

## Verification

Run `shellcheck` on any script before committing it -- it catches most of the above automatically:

```bash
shellcheck script.sh
```

For quick inline checks, the prologue is your first line of defense: if the script runs at all with `set -euo pipefail`, most of the dangerous mistakes have been caught already.
