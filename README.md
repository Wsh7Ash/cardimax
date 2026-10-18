# cardimax

**Find the label that is about to explode your metrics backend — before it ships.**

`cardimax` audits Prometheus-style label cardinality. It reads text
exposition dumps (or a static planning spec), measures how many distinct
values every label actually takes, multiplies those into a series estimate per
metric, projects what happens if you add one more label, and turns the whole
thing into CI-gateable exit codes.

Most cardinality incidents follow the same script: a well-meaning
`user_id`/`path`/`debug_message` label goes to production, its value set grows
with traffic, the TSDB slows down at 3 a.m., and everyone discovers that
cardinality is multiplicative. cardimax is the review step that catches this
in a pull request.

## Key features

- **Exposition parser** — real Prometheus text format: `# HELP`/`# TYPE`,
  quoted and escaped label values, malformed lines collected as parse errors
  instead of aborting the run.
- **Measured cardinality** — per label, per metric: distinct value counts and
  the product estimate (overflow-safe).
- **Risk model** — identity-shaped labels (`*_id`, `user_*`, `raw_path`,
  `debug_*`, `exception_*`, …) are flagged even at low counts; anything above
  `--label-threshold` is flagged as unbounded regardless of name, with
  per-label remediation advice (route templates, bucketing, logs instead of labels).
- **Growth projection** — `--add-label user_id=50000` answers "what does this
  new label cost on every existing metric?" before it ships.
- **Static planning spec** — `cardimax spec` estimates cardinality from a JSON
  declaration (`{"labels": {"method": 3, "route": 25}}`) so you can budget a
  metric before writing a line of instrumentation code.
- **CI gates** — `--fail-on high|medium|low|none`, `--max-series` per metric,
  `--max-total` overall, `--ignore-metric` regex; `--json` and `--markdown`
  output (markdown drops straight into a PR comment).
- **Zero dependencies** — Go standard library only.

## Example session

```shell
$ cardimax scan examples/metrics-problem.prom --label-threshold 4
cardimax — 5 metric(s), 17 series observed, verdict: OVER BUDGET

METRIC                     TYPE     SERIES  LABELS  EST  PROJECTED
----------------------------------------  -------  ------  ---  ---------
api_orders_lookup          counter  4       2       8    8
http_requests_path_raw     counter  5       1       5    5
...

label detail
  api_orders_lookup{user_email} : 4 distinct values  <- identity-shaped
  http_requests_path_raw{path} : 5 distinct values  <- UNBOUNDED
  checkout_debug_events{debug_message} : 3 distinct values  <- identity-shaped

findings: 6
  1. [HIGH] unbounded-label  http_requests_path_raw{path}
     label "path" has 5 distinct values (> 4)
     fix: replace with a normalized route template (e.g. /orders/{id}) and keep the raw value in logs
  2. [MEDIUM] risky-label-name  api_orders_lookup{user_email}
     label "user_email" is identity-shaped (4 distinct values observed)
     fix: drop the label from the metric; derive per-user breakdowns from logs or exemplars
  ...
$ echo $?
1
```

Projection — the question you should ask before merging:

```shell
$ cardimax spec examples/spec-bad.json --add-label user_id=50000
cardimax — 2 metric(s), 15 series observed, verdict: OVER BUDGET
projection: adding user_id=50000
...
  1. [HIGH] projection-overflow  checkout_events
     projected 1200000 series after adding labels (> budget 10000)
     fix: do not add unbounded labels; keep the breakdown in logs
```

Explain a label name:

```shell
$ cardimax explain user_id method
user_id
  risky: yes
  "user_id" ends with an identity-style suffix; its value set grows with users, requests or objects and is effectively unbounded
method
  risky: no
  "method" is not identity-shaped; risk is determined by the measured distinct value count
```

The full offline walkthrough (`bash examples/demo.sh`) writes every artifact
to `results/`.

## Architecture summary

```
exposition dump ─► parse.go ─► Dataset{metric → samples/labels}
spec.json       ─► DatasetFromSpecs ─────────────┘
                                        │
                                        ▼
                                 analyze.go ──► Report{metrics, findings, projections}
                                        │           per-label distinct counts
                                        │           risk model + budgets
                                        ▼
                                 render.go ──► text / markdown / JSON
                                        │
                                   main.go ──► exit 0 / 1 / 2
```

Details in [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Requirements

- Go **1.22+** to build. No runtime dependencies (static binary).
- `python3` is needed only by `examples/demo.sh` to generate its synthetic
  1200-series fixture.

## Installation

```shell
git clone https://github.com/Wsh7Ash/cardimax
cd cardimax
go build -o cardimax .
./cardimax version
```

Or straight from the repo: `go run . scan FILE`.

## Quick start

```shell
# audit a scrape dump (curl -s localhost:9090/metrics > metrics.prom)
cardimax scan metrics.prom

# tighter budgets in CI
cardimax scan metrics.prom --max-series 5000 --max-total 50000 --fail-on medium --json

# what would this new label cost?
cardimax scan metrics.prom --add-label request_id=250000

# budget a metric that does not exist yet
cardimax spec new-metric.json --max-series 10000
```

## Testing

```shell
go vet ./...
gofmt -l .          # should print nothing
go test ./... -count=1
go test ./... -race -count=1
bash examples/demo.sh
```

37 tests cover the parser (escapes, malformed lines, HELP/TYPE, spec
synthesis), the analysis (thresholds, severities, projections, overflow cap,
ignore rules, ordering), the renderers (text/markdown/JSON) and the CLI's
exit codes for every gate level.

## Project structure

```
cardimax/
├─ main.go                    # CLI: scan / spec / explain / version, exit codes
├─ internal/metrics/
│  ├─ parse.go                # exposition + spec parsing
│  ├─ analyze.go              # budgets, risk model, projections, verdict
│  ├─ render.go               # text / markdown / JSON renderers
│  └─ *_test.go               # 37 tests
├─ examples/
│  ├─ metrics-clean.prom      # within-budget fixture
│  ├─ metrics-problem.prom    # identity labels, raw paths, free-text labels
│  ├─ spec-good.json / spec-bad.json
│  └─ demo.sh                 # reproducible offline demo → results/
├─ results/                   # demo evidence
├─ docs/                      # ARCHITECTURE.md, USAGE.md
└─ .github/workflows/ci.yml
```

## Limitations

- The parser targets the Prometheus **text exposition format**; OpenMetrics
  protobuf dumps and remote-write payloads are out of scope.
- Estimates assume label dimensions are independent (product of distinct
  counts). Real series counts can be lower (correlated labels) — the number is
  an upper bound, which is the right direction for a budget conversation.
- The risk model is name-plus-measurement. A label named `region` with 500
  values is flagged by threshold; a name like `user_id` is flagged at 2
  values. Neither is a substitute for reviewing *why* a label exists.
- Projections multiply the estimate by the assumed value count; they do not
  model retention, compaction, or per-tenant fan-out.
- Budgets are yours: cardimax's defaults (10k/metric, 100k total, 200/label)
  are opinionated starting points, not hard rules.

## Roadmap

- HTML report with a treemap of series per metric.
- `--trend` comparison between two dumps (yesterday vs today).
- OpenMetrics text input and histogram-bucket linting.
- Prometheus query check: flag PromQL whose output cardinality matches an
  unbounded label.

## Security notes

cardimax reads only the files you name, performs no network requests, and
executes nothing from those files. Reports quote your own metric and label
names. See [docs/USAGE.md](docs/USAGE.md) for the CI recipe.

## License

[MIT](LICENSE)