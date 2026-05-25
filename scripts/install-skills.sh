#!/usr/bin/env bash
# install-skills.sh -- install jarvis-go's bundled skills into ~/.claude/skills/.
#
# Jarvis-go's built-in team templates and onboarding text reference skills
# by name (e.g. "office-protocols", "go-concurrency"). At runtime, jarvis
# reads them from $HOME/.claude/skills/<name>/SKILL.md. This script copies
# (or symlinks) each skill in ./skills/ into that location.
#
# Usage:
#   scripts/install-skills.sh                # symlink, skip existing
#   scripts/install-skills.sh --copy         # copy instead of symlink
#   scripts/install-skills.sh --force        # overwrite existing
#   scripts/install-skills.sh --dry-run      # show what would happen, do nothing
#   scripts/install-skills.sh --target DIR   # install into DIR instead of ~/.claude/skills
#
# Combinations are allowed (e.g. --copy --force, --dry-run --force).
# Exit codes: 0 on success, 1 on argument error, 2 if any skill failed to install.

set -euo pipefail

mode="symlink"
force=0
dry_run=0
target="${HOME}/.claude/skills"

usage() {
    sed -n '2,17p' "$0" | sed 's/^# \{0,1\}//'
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --copy)     mode="copy"; shift ;;
        --symlink)  mode="symlink"; shift ;;
        --force)    force=1; shift ;;
        --dry-run)  dry_run=1; shift ;;
        --target)   target="${2:-}"; shift 2 ;;
        -h|--help)  usage; exit 0 ;;
        *) echo "unknown flag: $1" >&2; usage >&2; exit 1 ;;
    esac
done

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
src_dir="$(cd "$script_dir/.." && pwd)/skills"

if [[ ! -d "$src_dir" ]]; then
    echo "error: source directory not found: $src_dir" >&2
    exit 1
fi

if (( dry_run == 0 )); then
    mkdir -p "$target"
fi

installed=0
skipped=0
overwrote=0
failed=0
missing_skillmd=0

run() {
    if (( dry_run )); then
        echo "DRY-RUN: $*"
    else
        "$@"
    fi
}

# P10.1 + P11.3.13 · Walk top-level skill bundles AND one level of
# vendor subdirectories so cherry-picked third-party content ships
# alongside jarvis's own. Each leaf must contain SKILL.md; vendor
# parent dirs themselves are skipped because they have no SKILL.md.
#
# To add a future vendor: append its directory name to vendor_dirs
# below. The script tracks which paths came from a vendor walk so the
# "no SKILL.md" warning doesn't fire on the vendor parent directory.
shopt -s nullglob
vendor_dirs=( "ecc" )
candidate_paths=( "$src_dir"/*/ )
declare -A vendor_parent_paths
for vd in "${vendor_dirs[@]}"; do
    vp="$src_dir/$vd/"
    if [[ -d "$vp" ]]; then
        candidate_paths+=( "$vp"*/ )
        vendor_parent_paths["$vp"]=1
    fi
done

for src in "${candidate_paths[@]}"; do
    name="$(basename "$src")"
    dst="$target/$name"

    if [[ ! -f "$src/SKILL.md" ]]; then
        # vendor parent dirs (e.g. skills/ecc/) intentionally have no
        # SKILL.md; quiet-skip them rather than counting as missing.
        if [[ -n "${vendor_parent_paths[$src]:-}" ]]; then
            continue
        fi
        echo "skip $name (no SKILL.md in bundle)" >&2
        missing_skillmd=$((missing_skillmd + 1))
        continue
    fi

    if [[ -e "$dst" || -L "$dst" ]]; then
        if (( force == 0 )); then
            echo "skip $name (already present, use --force to overwrite)"
            skipped=$((skipped + 1))
            continue
        fi
        run rm -rf "$dst"
        overwrote=$((overwrote + 1))
    fi

    if [[ "$mode" == "symlink" ]]; then
        if run ln -s "$src" "$dst"; then
            echo "link $name"
            installed=$((installed + 1))
        else
            echo "FAIL $name (symlink)" >&2
            failed=$((failed + 1))
        fi
    else
        if run cp -R "$src" "$dst"; then
            echo "copy $name"
            installed=$((installed + 1))
        else
            echo "FAIL $name (copy)" >&2
            failed=$((failed + 1))
        fi
    fi
done

echo
echo "summary: installed=$installed skipped=$skipped overwrote=$overwrote failed=$failed missing_skillmd=$missing_skillmd target=$target mode=$mode"

if (( failed > 0 )); then
    exit 2
fi
