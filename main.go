// cardimax estimates and audits Prometheus-style label cardinality.
//
// It reads text exposition dumps (or a static planning spec), reports which
// labels are unbounded, projects series growth for labels you intend to add,
// and gates CI with exit codes.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"cardimax/internal/metrics"
)

const version = "0.1.0"

const usage = `cardimax ` + version + ` — Prometheus label cardinality auditor

Usage:
  cardimax scan FILE [FILE...] [flags]     analyze exposition dumps
  cardimax spec SPEC.json [flags]          analyze a static planning spec
  cardimax explain LABEL [LABEL...]        why a label name is risky
  cardimax version                         print version

Flags:
  --json                 machine-readable output
  --markdown             markdown report
  -o FILE                write the report to a file
  --max-series N         per-metric series budget (default 10000)
  --max-total N          total series budget (default 100000)
  --label-threshold N    distinct values before a label is unbounded (default 200)
  --max-labels N         labels per metric budget (default 8)
  --fail-on LEVEL        high|medium|low|none (default high)
  --add-label name=N     project growth from an extra label (repeatable/comma-separated)
  --ignore-metric REGEX  skip metrics whose name matches

Exit codes: 0 within budget, 1 over the --fail-on gate, 2 usage error.

Spec format (spec subcommand):
  [ {"metric": {"name": "http_requests_total", "type": "counter",
                "labels": {"method": 3, "route": 25, "status": 4}}}, ... ]
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	if len(argv) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	switch argv[0] {
	case "version", "--version":
		fmt.Println("cardimax", version)
		return 0
	case "help", "--help", "-h":
		fmt.Print(usage)
		return 0
	case "explain":
		return explain(argv[1:])
	case "scan", "spec":
		return analyzeCmd(argv[0], argv[1:])
	default:
		fmt.Fprintf(os.Stderr, "error: unknown command %q\n\n%s", argv[0], usage)
		return 2
	}
}

type flags struct {
	json      bool
	markdown  bool
	out       string
	failOn    string
	addLabels []string
	ignore    string
	opts      metrics.Options
	rest      []string
}

func parseFlags(argv []string) (flags, error) {
	f := flags{failOn: "high", opts: metrics.DefaultOptions()}
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		next := func() (string, error) {
			if i+1 >= len(argv) {
				return "", fmt.Errorf("flag %s needs a value", arg)
			}
			i++
			return argv[i], nil
		}
		var err error
		switch arg {
		case "--json":
			f.json = true
		case "--markdown":
			f.markdown = true
		case "-o", "--output":
			if f.out, err = next(); err != nil {
				return f, err
			}
		case "--max-series":
			if f.opts.MaxSeriesPerMetric, err = intFlag(next); err != nil {
				return f, err
			}
		case "--max-total":
			if f.opts.MaxTotalSeries, err = intFlag(next); err != nil {
				return f, err
			}
		case "--label-threshold":
			v, err := intFlag(next)
			if err != nil {
				return f, err
			}
			f.opts.LabelThreshold = int(v)
		case "--max-labels":
			v, err := intFlag(next)
			if err != nil {
				return f, err
			}
			f.opts.MaxLabelsPerMetric = int(v)
		case "--fail-on":
			if f.failOn, err = next(); err != nil {
				return f, err
			}
			switch f.failOn {
			case "high", "medium", "low", "none":
			default:
				return f, fmt.Errorf("--fail-on must be high|medium|low|none, got %q", f.failOn)
			}
			f.opts.FailOn = f.failOn
		case "--add-label":
			v, err := next()
			if err != nil {
				return f, err
			}
			f.addLabels = append(f.addLabels, strings.Split(v, ",")...)
		case "--ignore-metric":
			if f.ignore, err = next(); err != nil {
				return f, err
			}
			rx, err := regexp.Compile(f.ignore)
			if err != nil {
				return f, fmt.Errorf("invalid --ignore-metric regex: %v", err)
			}
			f.opts.IgnoreMetric = rx
		case "--help", "-h":
			fmt.Print(usage)
			os.Exit(0)
		default:
			if strings.HasPrefix(arg, "-") {
				return f, fmt.Errorf("unknown flag %q", arg)
			}
			f.rest = append(f.rest, arg)
		}
	}
	for _, spec := range f.addLabels {
		name, value, ok := strings.Cut(spec, "=")
		if !ok || name == "" {
			return f, fmt.Errorf("--add-label must look like name=count, got %q", spec)
		}
		f.opts.AddedLabels = append(f.opts.AddedLabels, metrics.Label{Name: name, Value: value})
	}
	return f, nil
}

func intFlag(next func() (string, error)) (int64, error) {
	raw, err := next()
	if err != nil {
		return 0, err
	}
	var v int64
	if _, err := fmt.Sscanf(raw, "%d", &v); err != nil {
		return 0, fmt.Errorf("expected an integer, got %q", raw)
	}
	return v, nil
}

func analyzeCmd(command string, argv []string) int {
	f, err := parseFlags(argv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}

	var report *metrics.Report
	if command == "scan" {
		if len(f.rest) == 0 {
			fmt.Fprintln(os.Stderr, "error: scan needs at least one FILE")
			fmt.Fprint(os.Stderr, usage)
			return 2
		}
		ds := &metrics.Dataset{Metrics: map[string]*metrics.MetricInfo{}}
		for _, path := range f.rest {
			parsed, err := metrics.ParseFile(path)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				return 2
			}
			merge(ds, parsed, path)
		}
		report = metrics.Analyze(ds, f.opts)
	} else {
		if len(f.rest) != 1 {
			fmt.Fprintln(os.Stderr, "error: spec needs exactly one SPEC.json")
			return 2
		}
		specs, err := loadSpec(f.rest[0])
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 2
		}
		report = metrics.Analyze(metrics.DatasetFromSpecs(specs), f.opts)
	}

	text := emit(report, f)
	if f.out != "" {
		if err := os.WriteFile(f.out, []byte(text), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 2
		}
		fmt.Println("wrote", f.out)
	} else {
		fmt.Print(text)
	}

	if report.Verdict == "OVER BUDGET" {
		return 1
	}
	return 0
}

func emit(report *metrics.Report, f flags) string {
	switch {
	case f.json:
		return metrics.ToJSON(report, true) + "\n"
	case f.markdown:
		return metrics.RenderMarkdown(report, f.opts)
	default:
		return metrics.RenderText(report, f.opts) + "\n"
	}
}

func merge(dst, src *metrics.Dataset, source string) {
	for name, info := range src.Metrics {
		existing, ok := dst.Metrics[name]
		if !ok {
			dst.Metrics[name] = info
			dst.Order = append(dst.Order, name)
			continue
		}
		existing.Samples = append(existing.Samples, info.Samples...)
		if existing.Type == "untyped" && info.Type != "untyped" {
			existing.Type = info.Type
		}
		if existing.Help == "" {
			existing.Help = info.Help
		}
	}
	for _, e := range src.Errors {
		dst.Errors = append(dst.Errors, source+": "+e)
	}
}

func loadSpec(path string) ([]metrics.Spec, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var specs []metrics.Spec
	if err := json.Unmarshal(raw, &specs); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for _, s := range specs {
		if s.Metric.Name == "" {
			return nil, fmt.Errorf("%s: every spec entry needs metric.name", path)
		}
	}
	return specs, nil
}

func explain(argv []string) int {
	if len(argv) == 0 {
		fmt.Fprintln(os.Stderr, "error: explain needs at least one LABEL")
		return 2
	}
	for _, label := range argv {
		risk := "no"
		if metrics.IsRiskyLabel(label) {
			risk = "yes"
		}
		fmt.Printf("%s\n  risky: %s\n  %s\n", label, risk, metrics.WhyLabelIsRisky(label))
	}
	return 0
}
