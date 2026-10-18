package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVersionAndHelp(t *testing.T) {
	if code := run([]string{"version"}); code != 0 {
		t.Errorf("version exit: %d", code)
	}
	if code := run([]string{"help"}); code != 0 {
		t.Errorf("help exit: %d", code)
	}
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	if code := run([]string{"nope"}); code != 2 {
		t.Errorf("exit: %d", code)
	}
}

func TestExplain(t *testing.T) {
	if code := run([]string{"explain", "user_id", "method"}); code != 0 {
		t.Errorf("exit: %d", code)
	}
	if code := run([]string{"explain"}); code != 2 {
		t.Errorf("empty explain exit: %d", code)
	}
}

func TestScanFixtureExitCodes(t *testing.T) {
	clean := filepath.Join("examples", "metrics-clean.prom")
	problem := filepath.Join("examples", "metrics-problem.prom")
	if code := run([]string{"scan", clean}); code != 0 {
		t.Errorf("clean fixture should exit 0, got %d", code)
	}
	if code := run([]string{"scan", problem, "--label-threshold", "4"}); code != 1 {
		t.Errorf("problem fixture should exit 1, got %d", code)
	}
	if code := run([]string{"scan", problem, "--label-threshold", "4", "--fail-on", "none"}); code != 0 {
		t.Errorf("--fail-on none should exit 0, got %d", code)
	}
}

func TestScanMissingFileIsUsageError(t *testing.T) {
	if code := run([]string{"scan", "does/not/exist.prom"}); code != 2 {
		t.Errorf("exit: %d", code)
	}
}

func TestScanNeedsFile(t *testing.T) {
	if code := run([]string{"scan"}); code != 2 {
		t.Errorf("exit: %d", code)
	}
}

func TestSpecCommands(t *testing.T) {
	bad := filepath.Join("examples", "spec-bad.json")
	good := filepath.Join("examples", "spec-good.json")
	if code := run([]string{"spec", good, "--max-series", "1000"}); code != 0 {
		t.Errorf("good spec should exit 0, got %d", code)
	}
	if code := run([]string{"spec", bad, "--add-label", "user_id=50000"}); code != 1 {
		t.Errorf("projection should trip the gate, got %d", code)
	}
	if code := run([]string{"spec"}); code != 2 {
		t.Errorf("spec without file exit: %d", code)
	}
}

func TestSpecInvalidJSONIsUsageError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"spec", path}); code != 2 {
		t.Errorf("exit: %d", code)
	}
}

func TestOutputFile(t *testing.T) {
	out := filepath.Join(t.TempDir(), "report.md")
	code := run([]string{
		"scan", filepath.Join("examples", "metrics-problem.prom"),
		"--markdown", "-o", out, "--fail-on", "none",
	})
	if code != 0 {
		t.Fatalf("exit: %d", code)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("output file: %v", err)
	}
	if len(raw) == 0 {
		t.Error("empty report file")
	}
}

func TestBadFlagValues(t *testing.T) {
	if code := run([]string{"scan", "x.prom", "--fail-on", "sometimes"}); code != 2 {
		t.Errorf("exit: %d", code)
	}
	if code := run([]string{"scan", "x.prom", "--add-label", "user_id"}); code != 2 {
		t.Errorf("exit: %d", code)
	}
	if code := run([]string{"scan", "x.prom", "--max-series"}); code != 2 {
		t.Errorf("exit: %d", code)
	}
	if code := run([]string{"scan", "x.prom", "--ignore-metric", "(["}); code != 2 {
		t.Errorf("exit: %d", code)
	}
	if code := run([]string{"scan", "x.prom", "--nope"}); code != 2 {
		t.Errorf("exit: %d", code)
	}
}

func TestMergeMultipleFiles(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.prom")
	b := filepath.Join(dir, "b.prom")
	os.WriteFile(a, []byte("m{x=\"1\"} 1\n"), 0o644)
	os.WriteFile(b, []byte("m{x=\"2\"} 1\nn{y=\"1\"} 5\n"), 0o644)
	code := run([]string{"scan", a, b, "--json"})
	if code != 0 {
		t.Errorf("exit: %d", code)
	}
}
