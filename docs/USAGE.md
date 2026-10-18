# Usage

Exit codes: `0` within budget, `1` over the `--fail-on` gate, `2` usage error.

## Getting a dump to audit

```shell
curl -s http://localhost:9090/metrics > metrics.prom          # Prometheus
curl -s http://localhost:8080/metrics > app.prom              # a Go service
curl -s http://localhost:8000/actuator/prometheus > app.prom  # Spring
```

## scan

```shell
cardimax scan metrics.prom
cardimax scan a.prom b.prom c.prom            # merged by metric name
cardimax scan metrics.prom --json
cardimax scan metrics.prom --markdown -o review.md
```

Flags:

| Flag | Default | Meaning |
|---|---|---|
| `--max-series N` | 10000 | per-metric series budget |
| `--max-total N` | 100000 | total series budget |
| `--label-threshold N` | 200 | distinct values before a label is "unbounded" |
| `--max-labels N` | 8 | labels-per-metric budget |
| `--fail-on LEVEL` | high | gate level: `high`, `medium`, `low`, `none` |
| `--add-label name=N` | — | project growth from an extra label (repeatable, comma-separated) |
| `--ignore-metric REGEX` | — | skip matching metric families (e.g. `^go_`) |
| `--json` / `--markdown` / `-o FILE` | — | output mode |

### CI example

```yaml
- name: Cardinality budget
  run: |
    curl -s http://localhost:9090/metrics > metrics.prom
    go run . scan metrics.prom --fail-on medium --max-series 5000 --json > cardimax.json
```

Exit 1 fails the job; attach `cardimax.json` as an artifact for the reviewer.

## spec — budget a metric before writing it

`examples/spec-bad.json`:

```json
[
  {
    "metric": {
      "name": "checkout_events",
      "type": "counter",
      "labels": { "step": 4, "outcome": 3, "device": 2 }
    }
  }
]
```

```shell
cardimax spec examples/spec-bad.json --max-series 1000        # 24 series: fine
cardimax spec examples/spec-bad.json --add-label user_id=50000
# projection-overflow: 24 -> 1,200,000
```

## Projections on real dumps

```shell
cardimax scan metrics.prom --add-label request_id=250000
cardimax scan metrics.prom --add-label user_id=50000,region=12
```

Every metric row shows `EST` (today) and `PROJECTED` (after the added label),
and any metric whose projection crosses `--max-series` gets a
`projection-overflow` finding.

## explain

```shell
cardimax explain user_id raw_path exception_text route
```

Prints the risk model's one-line reasoning per label — useful when reviewing a
proposal that renames or adds labels.

## Typical workflows

**PR review of new instrumentation**

```shell
git show main:observability/metrics.go | grep -o '{[^}]*}'   # what labels am I adding?
cardimax spec new-metric.json --add-label user_id=50000 --fail-on high
```

**Post-incident audit**

```shell
curl -s http://prometheus:9090/metrics > incident.prom
cardimax scan incident.prom --max-series 5000 --markdown -o incident-cardinality.md
```

**Budget tracking over time**

```shell
cardimax scan metrics.prom --json > "reports/$(date +%F).json"
```

## Notes

- Estimates are upper bounds (label dimensions are assumed independent), which
  is the conservative direction for budgeting.
- `parse_errors` in the report mean some lines were malformed; fix the dump or
  the exporter, don't ignore the section.
- cardimax performs no network requests and executes nothing from the files it
  reads.