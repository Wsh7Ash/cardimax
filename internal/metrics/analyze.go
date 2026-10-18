package metrics

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Severity levels, ordered.
type Severity string

const (
	SevHigh   Severity = "high"
	SevMedium Severity = "medium"
	SevLow    Severity = "low"
)

var severityRank = map[Severity]int{SevLow: 1, SevMedium: 2, SevHigh: 3}

// LabelStat summarizes one label of one metric.
type LabelStat struct {
	Name     string `json:"name"`
	Distinct int    `json:"distinct"`
	Samples  int    `json:"samples"`
}

// MetricStat summarizes one metric.
type MetricStat struct {
	Name       string      `json:"name"`
	Type       string      `json:"type"`
	Series     int         `json:"series"`
	Labels     []LabelStat `json:"labels"`
	Estimate   int64       `json:"estimate"`  // product of observed label cardinalities
	Projected  int64       `json:"projected"` // estimate including --add-label projections
	LabelCount int         `json:"label_count"`
}

// Finding is one cardinality problem with remediation advice.
type Finding struct {
	Severity    Severity `json:"severity"`
	Kind        string   `json:"kind"`
	Metric      string   `json:"metric,omitempty"`
	Label       string   `json:"label,omitempty"`
	Detail      string   `json:"detail"`
	Remediation string   `json:"remediation"`
}

// Projection estimates series growth from labels the caller intends to add.
type Projection struct {
	Metric   string `json:"metric"`
	Before   int64  `json:"before"`
	After    int64  `json:"after"`
	Added    string `json:"added"`
	Overflow bool   `json:"overflow"`
}

// Options control thresholds.
type Options struct {
	MaxSeriesPerMetric int64   // budget for one metric's series/estimate
	MaxTotalSeries     int64   // budget across all metrics
	LabelThreshold     int     // distinct-value threshold for one label
	MaxLabelsPerMetric int     // label-count threshold for one metric
	FailOn             string  // "high" | "medium" | "low" | "none"
	AddedLabels        []Label // labels to project ("name" -> "count")
	IgnoreMetric       *regexp.Regexp
}

// DefaultOptions returns sensible defaults for a Prometheus-style stack.
func DefaultOptions() Options {
	return Options{
		MaxSeriesPerMetric: 10000,
		MaxTotalSeries:     100000,
		LabelThreshold:     200,
		MaxLabelsPerMetric: 8,
		FailOn:             "high",
	}
}

// Report is the full analysis result.
type Report struct {
	Metrics     []MetricStat `json:"metrics"`
	TotalSeries int          `json:"total_series"`
	Findings    []Finding    `json:"findings"`
	Projections []Projection `json:"projections,omitempty"`
	AddedLabels []Label      `json:"added_labels,omitempty"`
	ParseErrors []string     `json:"parse_errors,omitempty"`
	Verdict     string       `json:"verdict"`
	MaxSeverity Severity     `json:"max_severity"`
}

// label risk model ------------------------------------------------------------

var (
	riskyLabelNames = regexp.MustCompile(
		`(?i)(^|_)(id|uuid|guid|hash|token|key|secret|email|address|phone|url|uri|query|path|file|filename|host|hostname|ip|client|user|customer|tenant|account|session|request|trace|span|thread|connection)(_|$)`)
	riskySuffixes = regexp.MustCompile(
		`(?i)(_id|_uuid|_guid|_hash|_token|_key|_email|_url|_path|_file|_host|_ip|_address)$`)
	freeFormNames = regexp.MustCompile(`(?i)(debug|stack|error_msg|exception|message|text|reason)`)
)

// IsRiskyLabel reports whether a label name is structurally suspicious.
func IsRiskyLabel(name string) bool {
	lower := strings.ToLower(name)
	return riskyLabelNames.MatchString(lower) || riskySuffixes.MatchString(lower) || freeFormNames.MatchString(lower)
}

// WhyLabelIsRisky explains a label name in one sentence.
func WhyLabelIsRisky(name string) string {
	lower := strings.ToLower(name)
	switch {
	case riskySuffixes.MatchString(lower):
		return fmt.Sprintf("%q ends with an identity-style suffix; its value set grows with users, requests or objects and is effectively unbounded", name)
	case riskyLabelNames.MatchString(lower):
		return fmt.Sprintf("%q looks identity- or location-shaped; value sets like this usually grow with traffic or data", name)
	case freeFormNames.MatchString(lower):
		return fmt.Sprintf("%q looks free-form; free-text labels are unbounded by construction", name)
	default:
		return fmt.Sprintf("%q is not identity-shaped; risk is determined by the measured distinct value count", name)
	}
}

// analyze ---------------------------------------------------------------------

// Analyze computes the cardinality report for a parsed dataset.
func Analyze(ds *Dataset, opts Options) *Report {
	rep := &Report{ParseErrors: ds.Errors}

	projection := map[string]int64{}
	for _, l := range opts.AddedLabels {
		projection[l.Name] = parseCount(l.Value)
	}

	var total int64
	for _, name := range ds.Order {
		info := ds.Metrics[name]
		if opts.IgnoreMetric != nil && opts.IgnoreMetric.MatchString(name) {
			continue
		}
		stat := MetricStat{Name: name, Type: info.Type, Series: len(info.Samples)}

		distinct := map[string]map[string]struct{}{}
		for _, s := range info.Samples {
			for _, l := range s.Labels {
				if distinct[l.Name] == nil {
					distinct[l.Name] = map[string]struct{}{}
				}
				distinct[l.Name][l.Value] = struct{}{}
			}
		}

		estimate := int64(1)
		for label, values := range distinct {
			stat.Labels = append(stat.Labels, LabelStat{Name: label, Distinct: len(values), Samples: stat.Series})
			estimate = mulCap(estimate, int64(len(values)))
		}
		sort.Slice(stat.Labels, func(i, j int) bool { return stat.Labels[i].Name < stat.Labels[j].Name })
		stat.LabelCount = len(stat.Labels)
		stat.Estimate = estimate

		projected := estimate
		for _, count := range projection {
			projected = mulCap(projected, count)
		}
		stat.Projected = projected

		total += int64(stat.Series)
		rep.Metrics = append(rep.Metrics, stat)
		rep.Findings = append(rep.Findings, analyzeMetric(stat, opts)...)
	}
	rep.TotalSeries = int(total)

	sort.Slice(rep.Metrics, func(i, j int) bool {
		if rep.Metrics[i].Projected == rep.Metrics[j].Projected {
			return rep.Metrics[i].Name < rep.Metrics[j].Name
		}
		return rep.Metrics[i].Projected > rep.Metrics[j].Projected
	})

	if opts.MaxTotalSeries > 0 && int64(rep.TotalSeries) > opts.MaxTotalSeries {
		rep.Findings = append(rep.Findings, Finding{
			Severity:    SevHigh,
			Kind:        "total-cardinality",
			Detail:      fmt.Sprintf("total series %d exceeds the budget of %d", rep.TotalSeries, opts.MaxTotalSeries),
			Remediation: "set per-metric budgets, drop debug labels, or reduce histogram buckets",
		})
	}

	if len(projection) > 0 {
		added := describeAdded(projection)
		for _, m := range rep.Metrics {
			rep.Projections = append(rep.Projections, Projection{
				Metric:   m.Name,
				Before:   m.Estimate,
				After:    m.Projected,
				Added:    added,
				Overflow: opts.MaxSeriesPerMetric > 0 && m.Projected > opts.MaxSeriesPerMetric,
			})
		}
	}
	rep.AddedLabels = opts.AddedLabels
	rep.Verdict, rep.MaxSeverity = verdict(rep.Findings, opts.FailOn)
	return rep
}

func analyzeMetric(m MetricStat, opts Options) []Finding {
	var out []Finding

	for _, l := range m.Labels {
		if opts.LabelThreshold > 0 && l.Distinct > opts.LabelThreshold {
			sev := SevMedium
			if IsRiskyLabel(l.Name) {
				sev = SevHigh
			}
			out = append(out, Finding{
				Severity:    sev,
				Kind:        "unbounded-label",
				Metric:      m.Name,
				Label:       l.Name,
				Detail:      fmt.Sprintf("label %q has %d distinct values (> %d)", l.Name, l.Distinct, opts.LabelThreshold),
				Remediation: remediationFor(l.Name),
			})
		} else if IsRiskyLabel(l.Name) && l.Distinct > 1 {
			out = append(out, Finding{
				Severity:    SevMedium,
				Kind:        "risky-label-name",
				Metric:      m.Name,
				Label:       l.Name,
				Detail:      fmt.Sprintf("label %q is identity-shaped (%d distinct values observed)", l.Name, l.Distinct),
				Remediation: remediationFor(l.Name),
			})
		}
	}

	if opts.MaxLabelsPerMetric > 0 && m.LabelCount > opts.MaxLabelsPerMetric {
		out = append(out, Finding{
			Severity:    SevLow,
			Kind:        "label-bloat",
			Metric:      m.Name,
			Detail:      fmt.Sprintf("%d labels on one metric (> %d); every extra label multiplies the series", m.LabelCount, opts.MaxLabelsPerMetric),
			Remediation: "move rarely-used dimensions into logs or exemplars",
		})
	}

	if opts.MaxSeriesPerMetric > 0 {
		switch {
		case int64(m.Series) > opts.MaxSeriesPerMetric:
			out = append(out, Finding{
				Severity:    SevHigh,
				Kind:        "metric-overflow",
				Metric:      m.Name,
				Detail:      fmt.Sprintf("metric has %d series (> budget %d)", m.Series, opts.MaxSeriesPerMetric),
				Remediation: "drop or bucket high-cardinality labels; consider recording-series alternatives",
			})
		case m.Estimate > opts.MaxSeriesPerMetric:
			out = append(out, Finding{
				Severity:    SevMedium,
				Kind:        "estimate-overflow",
				Metric:      m.Name,
				Detail:      fmt.Sprintf("estimated %d series from label combinations (> budget %d)", m.Estimate, opts.MaxSeriesPerMetric),
				Remediation: "cap label value sets (allowlists/buckets) before the metric ships",
			})
		case m.Projected > opts.MaxSeriesPerMetric:
			out = append(out, Finding{
				Severity:    SevHigh,
				Kind:        "projection-overflow",
				Metric:      m.Name,
				Detail:      fmt.Sprintf("projected %d series after adding labels (> budget %d)", m.Projected, opts.MaxSeriesPerMetric),
				Remediation: "do not add unbounded labels; keep the breakdown in logs",
			})
		}
	}
	return out
}

func remediationFor(label string) string {
	lower := strings.ToLower(label)
	switch {
	case strings.Contains(lower, "path") || strings.Contains(lower, "url") || strings.Contains(lower, "uri") || strings.Contains(lower, "query"):
		return "replace with a normalized route template (e.g. /orders/{id}) and keep the raw value in logs"
	case strings.Contains(lower, "user") || strings.Contains(lower, "customer") || strings.Contains(lower, "tenant") || strings.Contains(lower, "account") || strings.Contains(lower, "session"):
		return "drop the label from the metric; derive per-user breakdowns from logs or exemplars"
	case strings.Contains(lower, "status") || strings.Contains(lower, "code") || strings.Contains(lower, "error"):
		return "bucket into a fixed set (ok/4xx/5xx) instead of raw codes or messages"
	case strings.Contains(lower, "host") || strings.Contains(lower, "instance") || strings.Contains(lower, "ip"):
		return "group by role or zone instead of individual hosts/addresses"
	default:
		return "allowlist the value set, bucket it, or move it to logs"
	}
}

func verdict(findings []Finding, failOn string) (string, Severity) {
	maxRank := 0
	var max Severity
	for _, f := range findings {
		if severityRank[f.Severity] > maxRank {
			maxRank = severityRank[f.Severity]
			max = f.Severity
		}
	}
	if max == "" {
		return "WITHIN BUDGET", ""
	}
	if failOn == "none" {
		return "FINDINGS (gating disabled)", max
	}
	threshold, ok := severityRank[Severity(failOn)]
	if !ok {
		threshold = severityRank[SevHigh]
	}
	if maxRank >= threshold {
		return "OVER BUDGET", max
	}
	return "FINDINGS (below gate)", max
}

// helpers ---------------------------------------------------------------------

const estimateCap = int64(1) << 50

func mulCap(a, b int64) int64 {
	if a == 0 || b == 0 {
		return 0
	}
	if a > estimateCap/b {
		return estimateCap
	}
	return a * b
}

func parseCount(s string) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || v < 1 {
		return 1
	}
	return v
}

func describeAdded(added map[string]int64) string {
	parts := make([]string, 0, len(added))
	for _, name := range sortedKeys(added) {
		parts = append(parts, fmt.Sprintf("%s=%d", name, added[name]))
	}
	return strings.Join(parts, ",")
}

func sortedKeys(m map[string]int64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
