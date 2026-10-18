# Changelog

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/).

## [0.1.0] - 2026-10-18

### Added

- Prometheus text exposition parser: HELP/TYPE metadata, quote- and
  escape-aware label parsing, malformed lines collected as `parse_errors`.
- Static planning spec (`cardimax spec`) that budgets metrics before they are
  implemented.
- Cardinality analysis: per-label distinct counts, overflow-safe series
  estimates, budgets (`--max-series`, `--max-total`, `--label-threshold`,
  `--max-labels`), identity-shape risk model, seven finding kinds with
  remediation advice, verdict + `--fail-on` gating.
- Growth projection via `--add-label name=N` (repeatable), including
  `projection-overflow` findings and before/after rows.
- `cardimax explain` for label-name risk explanations.
- Text, markdown and JSON reports; `-o` file output; `--ignore-metric` regex.
- Fixtures (`examples/*.prom`, `examples/spec-*.json`), an exit-code-asserting
  offline demo (`examples/demo.sh`), 37 tests, CI workflow.