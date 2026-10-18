# Architecture

## Goals

1. **Measure, don't guess.** Cardinality claims come from the dump: distinct
   value counts per label, multiplied into an upper-bound estimate.
2. **Review-time speed.** One binary, no dependencies, no network — it runs in
   a CI job on a scraped snapshot or a static spec in milliseconds.
3. **Advice, not just alarms.** Every finding carries a concrete remediation
   (route template, bucket, allowlist, move-to-logs) tied to the label's shape.
4. **Honest failure.** Malformed exposition lines become `parse_errors` in the
   report — visible, but never allowed to hide the rest of the analysis.

## Components

### parse.go — text → `Dataset`

`Dataset{Metrics map[string]*MetricInfo, Order []string, Errors []string}`.

- `# HELP` / `# TYPE` metadata attach to the following metric family.
- Sample lines are parsed with a hand-written scanner: metric name regex,
  label sets scanned with a quote-aware brace matcher (so `label="a}b"` and
  escaped quotes don't break it), escaped values (`\"`, `\\`, `\n`, `\t`)
  decoded, float value parsed with `strconv`.
- Every failure records `line N: reason` and parsing continues.
- `DatasetFromSpecs` turns a JSON planning spec
  (`{"metric": {"name": …, "labels": {"method": 3}}}`) into a synthetic
  dataset where each label has the declared number of distinct values — the
  same analyzer then budgets metrics that don't exist yet.

### analyze.go — `Dataset` + `Options` → `Report`

Per metric:

- `Series` = number of sample lines (what a scrape actually holds today).
- Per label `Distinct` = count of distinct observed values.
- `Estimate` = product of all label cardinalities (series upper bound if label
  combinations were fully crossed), computed with `mulCap` (capped at 2^50) so
  runaway projections never overflow.
- `Projected` = `Estimate × ∏(--add-label counts)`.

Findings, each with a remediation string:

| Kind | Trigger | Severity |
|---|---|---|
| `unbounded-label` | distinct > `--label-threshold` | high if the name is identity-shaped, else medium |
| `risky-label-name` | identity/free-form name, distinct > 1 but under threshold | medium |
| `metric-overflow` | actual series > `--max-series` | high |
| `estimate-overflow` | estimate > `--max-series` | medium |
| `projection-overflow` | projected > `--max-series` | high |
| `label-bloat` | label count > `--max-labels` | low |
| `total-cardinality` | total series > `--max-total` | high |

Risk model (`IsRiskyLabel`): word-boundary match on identity-ish tokens
(`id`, `uuid`, `user`, `path`, `host`, `email`, `trace`, `session`, …),
identity suffixes (`_id`, `_hash`, `_url`, `_path`, …), and free-form tokens
(`debug`, `stack`, `error_msg`, `exception`, `message`, `reason`).
`WhyLabelIsRisky` renders the same model as one sentence for `cardimax explain`.

Verdict: the maximum severity, then the gate —
`rank(failOn) <= maxRank` ⇒ `OVER BUDGET` (CLI exits 1), else
`FINDINGS (below gate)` or `WITHIN BUDGET`. `--fail-on none` reports findings
and always exits 0.

Metrics sort by `Projected` descending so the most dangerous line is on top.

### render.go — `Report` → text / markdown / JSON

Pure functions. Text output has four blocks: header (verdict, totals,
projection), metric table, per-label detail with `<- identity-shaped` /
`<- UNBOUNDED` markers, findings with `fix:` lines, and projection detail.
Markdown mirrors it with tables for PR comments; JSON is the report struct.

### main.go — CLI

| Command | Purpose |
|---|---|
| `scan FILE...` | parse one or more dumps (merged by metric name) and analyze |
| `spec SPEC.json` | analyze a static planning spec |
| `explain LABEL...` | print the risk model's verdict for label names |
| `version` / `help` | metadata |

Exit codes: `0` within budget, `1` `OVER BUDGET`, `2` usage error (missing
file, bad flag value, invalid spec JSON, unreadable input).

## Failure handling

| Failure | Behavior |
|---|---|
| malformed exposition line | recorded in `parse_errors`, run continues (exit unchanged) |
| unreadable file / invalid spec / bad flag | message on stderr, exit 2 |
| estimate explosion | `mulCap` clamps at 2^50 so arithmetic never wraps |

## Testing strategy

- **Parser**: happy path with HELP/TYPE, escaped quotes and backslashes,
  invalid names/values collected as errors, unterminated label sets, file
  fixtures, spec synthesis.
- **Analysis**: clean dataset (no findings), threshold behavior for both
  risky and boring names, metric/estimate/projection overflow, label bloat,
  total budget, ignore regex, ordering, `--fail-on none`, overflow-safe
  multiplication, risk-model positive/negative lists.
- **Render**: verdict/table/markdown shapes, finding + fix rendering,
  projection section, JSON roundtrip.
- **CLI**: version/help/unknown command, fixture exit codes (clean 0, problem
  1, gating off 0), spec flows, output file, every bad-flag path, multi-file
  merge.