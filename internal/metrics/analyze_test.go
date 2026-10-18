package metrics

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

func dataset(t *testing.T, input string) *Dataset {
	t.Helper()
	ds, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return ds
}

func TestCleanDatasetHasNoFindings(t *testing.T) {
	input := `# TYPE requests_total counter
requests_total{method="GET",route="/a"} 1
requests_total{method="POST",route="/a"} 2
requests_total{method="GET",route="/b"} 3
`
	rep := Analyze(dataset(t, input), DefaultOptions())
	if len(rep.Findings) != 0 {
		t.Errorf("findings: %+v", rep.Findings)
	}
	if rep.Verdict != "WITHIN BUDGET" {
		t.Errorf("verdict: %s", rep.Verdict)
	}
	if rep.TotalSeries != 3 {
		t.Errorf("total: %d", rep.TotalSeries)
	}
}

func TestUnboundedLabelFires(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 250; i++ {
		fmt.Fprintf(&b, "hits{path=\"/orders/%d\"} 1\n", i)
	}
	opts := DefaultOptions()
	rep := Analyze(dataset(t, b.String()), opts)
	if len(rep.Findings) == 0 {
		t.Fatal("expected findings")
	}
	f := rep.Findings[0]
	if f.Kind != "unbounded-label" || f.Severity != SevHigh {
		t.Errorf("finding: %+v", f)
	}
	if !strings.Contains(f.Remediation, "route template") {
		t.Errorf("remediation: %s", f.Remediation)
	}
	if rep.Verdict != "OVER BUDGET" {
		t.Errorf("verdict: %s", rep.Verdict)
	}
}

func TestRiskyNameSmallCountIsMedium(t *testing.T) {
	input := `m{user_id="u1"} 1
m{user_id="u2"} 1
`
	rep := Analyze(dataset(t, input), DefaultOptions())
	if len(rep.Findings) != 1 {
		t.Fatalf("findings: %+v", rep.Findings)
	}
	f := rep.Findings[0]
	if f.Kind != "risky-label-name" || f.Severity != SevMedium {
		t.Errorf("finding: %+v", f)
	}
	if rep.Verdict != "FINDINGS (below gate)" {
		t.Errorf("verdict: %s", rep.Verdict)
	}
}

func TestMetricOverflowHigh(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 120; i++ {
		fmt.Fprintf(&b, "big{n=\"%d\"} 1\n", i)
	}
	opts := DefaultOptions()
	opts.MaxSeriesPerMetric = 100
	opts.FailOn = "high"
	rep := Analyze(dataset(t, b.String()), opts)
	found := false
	for _, f := range rep.Findings {
		if f.Kind == "metric-overflow" && f.Severity == SevHigh {
			found = true
		}
	}
	if !found {
		t.Errorf("expected metric-overflow high, got %+v", rep.Findings)
	}
}

func TestLabelBloatLowSeverity(t *testing.T) {
	opts := DefaultOptions()
	opts.MaxLabelsPerMetric = 2
	input := `m{a="1",b="2",c="3"} 1
`
	rep := Analyze(dataset(t, input), opts)
	found := false
	for _, f := range rep.Findings {
		if f.Kind == "label-bloat" && f.Severity == SevLow {
			found = true
		}
	}
	if !found {
		t.Errorf("expected label-bloat, got %+v", rep.Findings)
	}
}

func TestTotalCardinalityFinding(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&b, "a{x=\"%d\"} 1\nb{y=\"%d\"} 1\n", i, i)
	}
	opts := DefaultOptions()
	opts.MaxTotalSeries = 60
	rep := Analyze(dataset(t, b.String()), opts)
	found := false
	for _, f := range rep.Findings {
		if f.Kind == "total-cardinality" {
			found = true
		}
	}
	if !found || rep.TotalSeries != 100 {
		t.Errorf("total=%d findings=%+v", rep.TotalSeries, rep.Findings)
	}
}

func TestProjectionMultipliesEstimate(t *testing.T) {
	input := `m{step="a",outcome="ok"} 1
m{step="b",outcome="ok"} 1
m{step="a",outcome="fail"} 1
`
	opts := DefaultOptions()
	opts.AddedLabels = []Label{{Name: "user_id", Value: "50000"}}
	rep := Analyze(dataset(t, input), opts)
	if len(rep.Projections) == 0 {
		t.Fatal("expected projections")
	}
	p := rep.Projections[0]
	if p.Before != 4 { // 2 steps x 2 outcomes
		t.Errorf("before: %d", p.Before)
	}
	if p.After != 4*50000 {
		t.Errorf("after: %d", p.After)
	}
	if !p.Overflow {
		t.Error("expected overflow flag")
	}
	found := false
	for _, f := range rep.Findings {
		if f.Kind == "projection-overflow" && f.Severity == SevHigh {
			found = true
		}
	}
	if !found {
		t.Errorf("expected projection-overflow finding, got %+v", rep.Findings)
	}
}

func TestExistingLabelProjectionMultiplies(t *testing.T) {
	input := `m{user_id="u1",route="/a"} 1
m{user_id="u2",route="/a"} 1
`
	opts := DefaultOptions()
	opts.AddedLabels = []Label{{Name: "route", Value: "25"}} // route exists with 1 distinct value
	rep := Analyze(dataset(t, input), opts)
	p := rep.Projections[0]
	if p.Before != 2 || p.After != 50 {
		t.Errorf("projection: %+v", p)
	}
}

func TestIgnoreMetric(t *testing.T) {
	input := `go_memstats_alloc_bytes 1
user_hits{path="x"} 1
`
	opts := DefaultOptions()
	opts.IgnoreMetric = regexp.MustCompile(`^go_`)
	rep := Analyze(dataset(t, input), opts)
	if len(rep.Metrics) != 1 || rep.Metrics[0].Name != "user_hits" {
		t.Errorf("metrics: %+v", rep.Metrics)
	}
}

func TestSortByProjectedDescending(t *testing.T) {
	input := `small{a="1"} 1
small{a="2"} 1
big{x="1"} 1
`
	rep := Analyze(dataset(t, input), DefaultOptions())
	if rep.Metrics[0].Name != "small" || rep.Metrics[1].Name != "big" {
		t.Errorf("order: %+v", rep.Metrics)
	}
}

func TestMulCapDoesNotOverflow(t *testing.T) {
	got := mulCap(estimateCap, estimateCap)
	if got != estimateCap {
		t.Errorf("expected cap, got %d", got)
	}
	if mulCap(0, 5) != 0 {
		t.Error("zero stays zero")
	}
}

func TestIsRiskyLabel(t *testing.T) {
	yes := []string{"user_id", "trace_id", "raw_path", "customer_email", "debug_message", "exception_text", "request_uri"}
	no := []string{"method", "status", "route", "queue", "le", "quantile", "version", "region"}
	for _, name := range yes {
		if !IsRiskyLabel(name) {
			t.Errorf("%q should be risky", name)
		}
	}
	for _, name := range no {
		if IsRiskyLabel(name) {
			t.Errorf("%q should not be risky", name)
		}
	}
}

func TestWhyLabelIsRisky(t *testing.T) {
	if !strings.Contains(WhyLabelIsRisky("user_id"), "identity") {
		t.Errorf("explanation: %s", WhyLabelIsRisky("user_id"))
	}
	if !strings.Contains(WhyLabelIsRisky("method"), "measured") {
		t.Errorf("explanation: %s", WhyLabelIsRisky("method"))
	}
}

func TestFailOnNoneNeverOverBudget(t *testing.T) {
	opts := DefaultOptions()
	opts.FailOn = "none"
	input := `m{user_id="a"} 1
m{user_id="b"} 1
`
	rep := Analyze(dataset(t, input), opts)
	if rep.Verdict == "OVER BUDGET" {
		t.Errorf("verdict: %s", rep.Verdict)
	}
	if rep.MaxSeverity != SevMedium {
		t.Errorf("max severity: %s", rep.MaxSeverity)
	}
}
