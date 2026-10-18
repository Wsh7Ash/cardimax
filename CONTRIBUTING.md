# Contributing

Thanks for helping improve cardimax.

## Setup

```shell
go build -o cardimax .
go test ./... -count=1
go test ./... -race -count=1
bash examples/demo.sh
```

Standard library only — cardimax should stay a single dependency-free binary.

## Ground rules

1. **Budgets are opinions, measurements are facts.** Thresholds must stay
   flags; the report must always show the measured numbers.
2. **Every finding needs a fix line.** An alarm without remediation just
   trains people to ignore the tool.
3. **Malformed input is data.** Parse errors go in the report; never abort the
   whole run because one exporter emits a bad line.
4. **gofmt + vet clean**, tests for new behavior, `examples/demo.sh` keeps
   asserting its exit codes.

## Adding a finding kind

1. Add the rule to `analyzeMetric()` in `internal/metrics/analyze.go` with a
   severity and a remediation.
2. Cover it with a positive **and** a negative test in `analyze_test.go`.
3. Document it in README (table) and USAGE, and note it in CHANGELOG.

## Commit style

Any clear imperative message is fine; reference the issue when there is one.