package metrics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSamplesAndLabels(t *testing.T) {
	input := `# HELP http_requests_total Requests.
# TYPE http_requests_total counter
http_requests_total{method="GET",route="/a",status="2xx"} 12
http_requests_total{method="POST",route="/a",status="2xx"} 3
http_requests_total 7
`
	ds, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	info := ds.Metrics["http_requests_total"]
	if info == nil {
		t.Fatal("metric missing")
	}
	if info.Type != "counter" || info.Help != "Requests." {
		t.Errorf("metadata: %+v", info)
	}
	if len(info.Samples) != 3 {
		t.Fatalf("samples: %d", len(info.Samples))
	}
	if len(info.Samples[0].Labels) != 3 {
		t.Errorf("labels: %+v", info.Samples[0].Labels)
	}
	if info.Samples[0].Value != 12 {
		t.Errorf("value: %v", info.Samples[0].Value)
	}
	if len(ds.Errors) != 0 {
		t.Errorf("unexpected errors: %v", ds.Errors)
	}
}

func TestParseEscapedQuoteInLabel(t *testing.T) {
	input := `log_lines_total{message="said \"hi\" loudly"} 1`
	ds, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	labels := ds.Metrics["log_lines_total"].Samples[0].Labels
	if len(labels) != 1 || labels[0].Value != `said "hi" loudly` {
		t.Errorf("labels: %+v", labels)
	}
}

func TestParseInvalidLinesAreCollectedNotFatal(t *testing.T) {
	input := `good_metric{a="1"} 1
bad name{a="1"} 2
broken{a="1" 3
no_value{a="1"}
`
	ds, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse should not fail hard: %v", err)
	}
	if len(ds.Metrics["good_metric"].Samples) != 1 {
		t.Errorf("good line lost: %+v", ds.Metrics["good_metric"])
	}
	if len(ds.Errors) != 3 {
		t.Errorf("expected 3 errors, got %d: %v", len(ds.Errors), ds.Errors)
	}
}

func TestParseEscapesInValues(t *testing.T) {
	input := `weird{path="a\tb",note="x\ny"} 1`
	ds, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	labels := ds.Metrics["weird"].Samples[0].Labels
	if labels[0].Value != "a\tb" || labels[1].Value != "x\ny" {
		t.Errorf("labels: %+v", labels)
	}
}

func TestParseFileFixture(t *testing.T) {
	path := filepath.Join("..", "..", "examples", "metrics-clean.prom")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	ds, err := ParseFile(path)
	if err != nil {
		t.Fatalf("parse file: %v", err)
	}
	if len(ds.Errors) != 0 {
		t.Errorf("clean fixture has parse errors: %v", ds.Errors)
	}
	if ds.Metrics["http_requests_total"] == nil {
		t.Error("missing http_requests_total")
	}
	if ds.Metrics["build_info"].Type != "gauge" {
		t.Errorf("type: %s", ds.Metrics["build_info"].Type)
	}
}

func TestDatasetFromSpecs(t *testing.T) {
	specs := []Spec{
		{Metric: struct {
			Name   string             `json:"name"`
			Type   string             `json:"type"`
			Labels map[string]float64 `json:"labels"`
		}{Name: "m", Type: "counter", Labels: map[string]float64{"a": 3, "b": 4}}},
	}
	ds := DatasetFromSpecs(specs)
	if got := len(ds.Metrics["m"].Samples); got != 7 {
		t.Errorf("expected 3+4 synthetic samples, got %d", got)
	}
	if ds.Metrics["m"].Type != "counter" {
		t.Errorf("type: %s", ds.Metrics["m"].Type)
	}
}

func TestParseLabelNamesInvalid(t *testing.T) {
	input := `bad{1abc="x"} 1`
	ds, _ := Parse(strings.NewReader(input))
	if len(ds.Errors) != 1 {
		t.Errorf("expected one error, got %v", ds.Errors)
	}
}
