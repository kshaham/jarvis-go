#!/usr/bin/env bash
# Statement-weighted coverage over the IN-SCOPE business-logic package set.
# Measures each package with its OWN single-package -coverprofile (the merged
# multi-package mode needs `covdata`, which this toolchain lacks), then sums
# covered/total statements across all profiles. Excludes pure glue + test
# helper packages (agreed scope).
set -uo pipefail
cd "$(dirname "$0")/.." || exit 1

EXCLUDE='^github.com/kshaham/jarvis/(cmd/cm|cmd/jarvis|cmd/ptyserver|cmd/fakeclaude|internal/web|internal/testutil/stubbin)$'
CP=/tmp/cp
mkdir -p "$CP"; rm -f "$CP"/*.out

n=0; failed=""
while IFS= read -r pkg; do
  [ -z "$pkg" ] && continue
  slug=$(printf '%s' "$pkg" | tr '/.' '__')
  if ! go test "$pkg" -count=1 -covermode=set -coverprofile="$CP/$slug.out" >/dev/null 2>"$CP/$slug.err"; then
    # ignore the harmless 'no such tool covdata' note; flag real failures
    if grep -qvE 'covdata' "$CP/$slug.err" && grep -qE 'FAIL|panic|setup failed' "$CP/$slug.err"; then
      failed="$failed $pkg"
    fi
  fi
  n=$((n+1))
done < <(go list ./... 2>/dev/null | grep -v node_modules | grep -Ev "$EXCLUDE")

echo "measured $n in-scope packages"
[ -n "$failed" ] && echo "REAL FAILURES:$failed"

python3 - "$CP" <<'PY'
import glob,os,re,sys
cp=sys.argv[1]
tot=cov=0
per=[]
for f in glob.glob(os.path.join(cp,'*.out')):
    pt=pc=0
    for line in open(f):
        m=re.match(r'.+:\d+\.\d+,\d+\.\d+ (\d+) (\d+)$', line.strip())
        if m:
            nstmt=int(m.group(1)); cnt=int(m.group(2)); pt+=nstmt
            if cnt>0: pc+=nstmt
    if pt:
        per.append((100.0*pc/pt, pc, pt, os.path.basename(f)[:-4]))
        tot+=pt; cov+=pc
print("=== lowest-covered in-scope packages ===")
for pctv,pc,pt,name in sorted(per)[:8]:
    print("  %5.1f%%  %s (%d/%d)" % (pctv, name.replace('__','/'), pc, pt))
print("============================================================")
print("WEIGHTED IN-SCOPE COVERAGE: %d/%d statements = %.2f%%" % (cov, tot, 100.0*cov/tot if tot else 0))
print("============================================================")
PY
