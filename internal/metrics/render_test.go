package metrics

import (
	"encoding/json"
	"strings"
	"testing"
)

func sampleReport(t *testing.T) *Report {
	t.Helper()
	input := `# TYPE requests_total counter
requests_total{method="GET",route="/a"} 1
requests_total{method="POST",route="/a"} 2
`
	return Analyze(mustDataset(t, input), DefaultOptions())
}

func mustDataset(t *testing.T, input string) *Dataset {
	t.Helper()
	ds, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return ds
}

func TestRenderTextContainsVerdictAndTable(t *testing.T) {
	rep := sampleReport(t)
	out := RenderText(rep, DefaultOptions())
	if !strings.Contains(out, "verdict: WITHIN BUDGET") {
		t.Errorf("text report:\n%s", out)
	}
	if !strings.Contains(out, "requests_total") || !strings.Contains(out, "METRIC") {
		t.Errorf("table missing:\n%s", out)
	}
	if !strings.Contains(out, "findings: none") {
		t.Errorf("findings line missing:\n%s", out)
	}
}

func TestRenderTextListsFindingsWithFix(t *testing.T) {
	input := `m{user_email="a@x"} 1
m{user_email="b@x"} 1
`
	rep := Analyze(mustDataset(t, input), DefaultOptions())
	out := RenderText(rep, DefaultOptions())
	if !strings.Contains(out, "risky-label-name") || !strings.Contains(out, "fix:") {
		t.Errorf("findings rendering:\n%s", out)
	}
	if !strings.Contains(out, "<- identity-shaped") {
		t.Errorf("label marker missing:\n%s", out)
	}
}

func TestRenderMarkdownShape(t *testing.T) {
	rep := sampleReport(t)
	out := RenderMarkdown(rep, DefaultOptions())
	if !strings.HasPrefix(out, "# cardimax report") {
		t.Errorf("markdown:\n%s", out)
	}
	for _, want := range []string{"## Metrics", "| `requests_total`", "## Findings", "No findings"} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown missing %q:\n%s", want, out)
		}
	}
}

func TestToJSONRoundTrip(t *testing.T) {
	rep := sampleReport(t)
	var decoded Report
	if err := json.Unmarshal([]byte(ToJSON(rep, true)), &decoded); err != nil {
		t.Fatalf("json: %v", err)
	}
	if decoded.TotalSeries != rep.TotalSeries || decoded.Verdict != rep.Verdict {
		t.Errorf("roundtrip mismatch: %+v", decoded)
	}
}

func TestRenderProjectionSection(t *testing.T) {
	opts := DefaultOptions()
	opts.AddedLabels = []Label{{Name: "user_id", Value: "1000"}}
	rep := Analyze(mustDataset(t, `m{route="/a"} 1`), opts)
	out := RenderText(rep, opts)
	if !strings.Contains(out, "projection: adding user_id=1000") {
		t.Errorf("projection header missing:\n%s", out)
	}
	if !strings.Contains(out, "projection detail") {
		t.Errorf("projection detail missing:\n%s", out)
	}
}
