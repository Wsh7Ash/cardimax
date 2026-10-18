#!/usr/bin/env bash
# Reproducible cardimax demo — offline, everything stays on this machine.
# Run from the repository root:  bash examples/demo.sh
# Requires: go (>=1.22), python3 (only for the synthetic hot-metrics fixture).
set -uo pipefail

BIN=./cardimax-bin
RESULTS=results
mkdir -p "$RESULTS"

go build -o "$BIN" . || exit 2

codes=()
run() { # run <expected-exit> <logfile> <command...>
  local expected="$1" logfile="$2"; shift 2
  local label; label="$*"
  "$@" >"$logfile" 2>&1
  local actual=$?
  codes+=("[$label] exit $actual (want $expected)")
  if [ "$actual" -ne "$expected" ]; then
    echo "UNEXPECTED: '$*' exited $actual, expected $expected" >&2
    sed -n '1,20p' "$logfile" >&2
    exit 3
  fi
}

# 1. clean exposition dump: within budget
run 0 "$RESULTS/report-clean.txt" "$BIN" scan examples/metrics-clean.prom

# 2. problem dump: identity-shaped labels + raw paths + free text
run 1 "$RESULTS/report-problem.txt" "$BIN" scan examples/metrics-problem.prom \
  --label-threshold 4 --fail-on high
run 1 "$RESULTS/report-problem.json" "$BIN" scan examples/metrics-problem.prom \
  --label-threshold 4 --json

# 3. synthetic hot metric: 60 user emails x 20 paths = 1200 series in one metric
python3 - "$RESULTS/hot.prom" <<'PY'
import sys
out = open(sys.argv[1], "w")
out.write("# HELP synthetic_request_latency_seconds Latency by user and raw path.\n")
out.write("# TYPE synthetic_request_latency_seconds summary\n")
n = 0
for u in range(60):
    for p in range(20):
        n += 1
        val = (0.01 * (u % 300) + 0.001 * p)
        out.write(f'synthetic_request_latency_seconds{{user_id="u{u:04d}",path="/a/{p:03d}",quantile="0.99"}} {val:.3f}\n')
out.close()
print("generated", n, "series")
PY
run 1 "$RESULTS/report-hot.txt" "$BIN" scan "$RESULTS/hot.prom" \
  --max-series 500 --fail-on high

# 4. planning spec: project the cost of a new label before shipping it
run 1 "$RESULTS/report-projection.txt" "$BIN" spec examples/spec-bad.json \
  --add-label user_id=50000 --fail-on high
run 0 "$RESULTS/report-spec-good.txt" "$BIN" spec examples/spec-good.json \
  --max-series 1000

# 5. label risk explanations
run 0 "$RESULTS/explain.txt" "$BIN" explain user_id raw_path exception_text route status

# 6. markdown output of the problem dump (for PR comments)
run 1 "$RESULTS/report-problem.md" "$BIN" scan examples/metrics-problem.prom \
  --label-threshold 4 --markdown -o "$RESULTS/report-problem.md"

rm -f "$BIN"
echo "exit codes: ${codes[*]}"
echo "verdicts:"
grep -h "verdict:" "$RESULTS"/report-clean.txt "$RESULTS"/report-problem.txt "$RESULTS"/report-hot.txt
echo "artifacts:"; ls "$RESULTS"