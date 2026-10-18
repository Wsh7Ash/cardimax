# Results

Honest record of what was actually executed for cardimax on 2026-10-18.

## Environment

- OS: Windows 11 (MINGW64_NT-10.0-19044, x86_64), MSYS2 MinGW shell
- Toolchain: go1.27.0 windows/amd64
- Dependency manager: Go modules (module `cardimax`, no external requirements)
- Network: none needed — no dependencies fetched, no services contacted

## Validation performed

| Gate | Result | Evidence |
|---|---|---|
| install/build | **PASS** | `go build -o cardimax .` → binary built; `go build ./...` clean |
| vet | **PASS** | `go vet ./...` → no output, exit 0 |
| format | **PASS** | `gofmt -l .` → no files listed (after `gofmt -w .`) |
| unit tests | **PASS** | `go test ./... -count=1` → `ok cardimax` + `ok cardimax/internal/metrics`, exit 0; **37 tests** (`go test -count=1 -v | grep -c '^=== RUN'` → 37) |
| race | **PASS** | `go test ./... -race -count=1` → ok, exit 0 |
| demo run | **PASS** | `bash examples/demo.sh` → exit 0, with all 8 asserted sub-exit-codes matching expectations |
| CLI gate check | **PASS** | clean fixture → exit 0; problem fixture → exit 1; `--fail-on none` → exit 0 (asserted by `main_test.go` and by the demo script) |
| secret scan | **PASS** | grep for credential/key/token patterns: no matches beyond ordinary prose; no `.env`, no key material |

## Commands executed

```shell
go vet ./...
gofmt -w . && gofmt -l .        # (nothing listed)
go build -o cardimax .
go test ./... -count=1          # 37 tests, ok, exit 0
go test ./... -race -count=1    # ok, exit 0
bash examples/demo.sh           # exit 0, all asserted exit codes matched
```

## Observed result

`bash examples/demo.sh` (offline; asserts every sub-command's exit code):

```
generated 1200 series
exit codes: [./cardimax-bin scan examples/metrics-clean.prom] exit 0 (want 0)
  [./cardimax-bin scan examples/metrics-problem.prom --label-threshold 4 --fail-on high] exit 1 (want 1)
  [./cardimax-bin scan examples/metrics-problem.prom --label-threshold 4 --json] exit 1 (want 1)
  [./cardimax-bin scan results/hot.prom --max-series 500 --fail-on high] exit 1 (want 1)
  [./cardimax-bin spec examples/spec-bad.json --add-label user_id=50000 --fail-on high] exit 1 (want 1)
  [./cardimax-bin spec examples/spec-good.json --max-series 1000] exit 0 (want 0)
  [./cardimax-bin explain user_id raw_path exception_text route status] exit 0 (want 0)
  [./cardimax-bin scan examples/metrics-problem.prom --label-threshold 4 --markdown -o ...] exit 1 (want 1)
verdicts:
cardimax — 6 metric(s), 14 series observed, verdict: WITHIN BUDGET
cardimax — 5 metric(s), 17 series observed, verdict: OVER BUDGET
cardimax — 1 metric(s), 1200 series observed, verdict: OVER BUDGET
DEMO_EXIT=0
```

Representative findings (from `results/report-problem.txt` and
`results/report-hot.txt`):

```
1. [HIGH] unbounded-label  http_requests_path_raw{path}
   label "path" has 5 distinct values (> 4)
   fix: replace with a normalized route template (e.g. /orders/{id}) and keep the raw value in logs
2. [MEDIUM] risky-label-name  api_orders_lookup{user_email}
   label "user_email" is identity-shaped (4 distinct values observed)
   fix: drop the label from the metric; derive per-user breakdowns from logs or exemplars
3. [HIGH] metric-overflow  synthetic_request_latency_seconds
   metric has 1200 series (> budget 500)
   fix: drop or bucket high-cardinality labels; consider recording-series alternatives
```

Projection (`results/report-projection.txt`) — the pre-merge question:

```
projection: adding user_id=50000
checkout_events  counter  9 series, estimate 24  ->  projected 1200000  [OVERFLOW]
queue_depth      gauge    6 series, estimate 6   ->  projected 300000   [OVERFLOW]
1. [HIGH] projection-overflow  checkout_events
   projected 1200000 series after adding labels (> budget 10000)
```

Behavior proven by tests, not just by the demo:

- parser: escaped quotes/backslashes in label values, unterminated label sets,
  invalid names/values collected as `parse_errors` without aborting
- analysis: threshold and risk-model behavior, `--fail-on none`, ignore
  regexes, ordering by projected cardinality, overflow-safe multiplication
- CLI: every flag validation path returns exit 2; multi-file merge; `-o` output

## Generated artifacts

- `results/report-clean.txt` — clean fixture, WITHIN BUDGET
- `results/report-problem.txt` / `.json` / `.md` — problem fixture, OVER BUDGET
- `results/report-hot.txt` + `results/hot.prom` — synthetic 1200-series metric
- `results/report-projection.txt` — spec + `--add-label user_id=50000`
- `results/report-spec-good.txt` — good spec within budget
- `results/explain.txt` — label risk explanations

Regenerate everything with `bash examples/demo.sh`.

## Known limitations

- The race detector run is meaningful on this codebase only insofar as the
  analyzer is single-threaded; concurrency coverage comes from the parser's
  scanner design, not from parallel analysis.
- Windows/MinGW was the local toolchain; CI runs Linux with Go 1.22.
- Estimates assume independent label dimensions (documented upper bound).
- OpenMetrics protobuf and remote-write payloads are not parsed (text format
  only).