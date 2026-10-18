// Package metrics parses Prometheus text exposition files and static metric
// specs, and analyzes label cardinality.
package metrics

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Sample is one exposition line: metric name, labels, value.
type Sample struct {
	Name   string
	Labels []Label
	Value  float64
}

// Label is a single name="value" pair.
type Label struct {
	Name  string
	Value string
}

// MetricInfo carries the HELP/TYPE metadata seen for a metric name.
type MetricInfo struct {
	Type    string
	Help    string
	Samples []Sample
}

// Dataset is everything parsed from one or more files.
type Dataset struct {
	Metrics map[string]*MetricInfo
	Order   []string
	Errors  []string
}

var (
	helpRe     = regexp.MustCompile(`^#\s*HELP\s+(\S+)\s+(.*)$`)
	typeRe     = regexp.MustCompile(`^#\s*TYPE\s+(\S+)\s+(\S+)$`)
	nameRe     = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)
	labelNameR = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
)

// ParseFile reads an exposition file from disk.
func ParseFile(path string) (*Dataset, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f)
}

// Parse reads Prometheus text exposition format from r.  Malformed lines are
// recorded in Errors instead of aborting, so one bad scrape line cannot hide
// the cardinality problem in the rest of the file.
func Parse(r io.Reader) (*Dataset, error) {
	ds := &Dataset{Metrics: map[string]*MetricInfo{}}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "# VERSION") {
			continue
		}
		if m := helpRe.FindStringSubmatch(line); m != nil {
			ds.info(m[1]).Help = m[2]
			continue
		}
		if m := typeRe.FindStringSubmatch(line); m != nil {
			ds.info(m[1]).Type = m[2]
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue // plain comment
		}
		sample, err := parseSampleLine(line)
		if err != nil {
			ds.Errors = append(ds.Errors, fmt.Sprintf("line %d: %v", lineNo, err))
			continue
		}
		info := ds.info(sample.Name)
		info.Samples = append(info.Samples, sample)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return ds, nil
}

func (ds *Dataset) info(name string) *MetricInfo {
	if m, ok := ds.Metrics[name]; ok {
		return m
	}
	m := &MetricInfo{Type: "untyped"}
	ds.Metrics[name] = m
	ds.Order = append(ds.Order, name)
	return m
}

func parseSampleLine(line string) (Sample, error) {
	s := Sample{}
	open := strings.IndexByte(line, '{')
	var namePart, labelPart, rest string
	if open < 0 {
		space := strings.IndexAny(line, " \t")
		if space < 0 {
			return s, fmt.Errorf("missing value: %q", line)
		}
		namePart, rest = line[:space], line[space:]
	} else {
		namePart = line[:open]
		end, err := findCloseBrace(line, open)
		if err != nil {
			return s, err
		}
		labelPart = line[open+1 : end]
		rest = line[end+1:]
	}
	s.Name = strings.TrimSpace(namePart)
	if !nameRe.MatchString(s.Name) {
		return s, fmt.Errorf("invalid metric name %q", s.Name)
	}
	labels, err := parseLabels(labelPart)
	if err != nil {
		return s, err
	}
	s.Labels = labels
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return s, fmt.Errorf("missing value for %q", s.Name)
	}
	value, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return s, fmt.Errorf("invalid value %q for %q", fields[0], s.Name)
	}
	s.Value = value
	return s, nil
}

// findCloseBrace returns the index of the '}' matching the '{' at open,
// skipping quoted label values.
func findCloseBrace(line string, open int) (int, error) {
	inQuote := false
	escaped := false
	for i := open + 1; i < len(line); i++ {
		c := line[i]
		switch {
		case escaped:
			escaped = false
		case c == '\\' && inQuote:
			escaped = true
		case c == '"':
			inQuote = !inQuote
		case c == '}' && !inQuote:
			return i, nil
		}
	}
	return 0, fmt.Errorf("unterminated label set in %q", line)
}

// parseLabels parses `a="1", b="x\"y"` (empty input yields no labels).
func parseLabels(part string) ([]Label, error) {
	part = strings.TrimSpace(part)
	if part == "" {
		return nil, nil
	}
	var labels []Label
	i := 0
	for i < len(part) {
		for i < len(part) && (part[i] == ' ' || part[i] == ',') {
			i++
		}
		if i >= len(part) {
			break
		}
		eq := strings.IndexByte(part[i:], '=')
		if eq < 0 {
			return nil, fmt.Errorf("label without '=' in %q", part)
		}
		name := strings.TrimSpace(part[i : i+eq])
		if !labelNameR.MatchString(name) {
			return nil, fmt.Errorf("invalid label name %q", name)
		}
		i += eq + 1
		for i < len(part) && part[i] == ' ' {
			i++
		}
		if i >= len(part) || part[i] != '"' {
			return nil, fmt.Errorf("label %q value is not quoted", name)
		}
		value, next, err := readQuoted(part, i)
		if err != nil {
			return nil, err
		}
		labels = append(labels, Label{Name: name, Value: value})
		i = next
	}
	return labels, nil
}

func readQuoted(s string, start int) (string, int, error) {
	var b strings.Builder
	escaped := false
	for i := start + 1; i < len(s); i++ {
		c := s[i]
		switch {
		case escaped:
			switch c {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			default:
				b.WriteByte(c)
			}
			escaped = false
		case c == '\\':
			escaped = true
		case c == '"':
			return b.String(), i + 1, nil
		default:
			b.WriteByte(c)
		}
	}
	return "", start, fmt.Errorf("unterminated quoted value in %q", s[start:])
}

// Spec is the static planning format: metric → labels → estimated distinct values.
type Spec struct {
	Metric struct {
		Name   string             `json:"name"`
		Type   string             `json:"type"`
		Labels map[string]float64 `json:"labels"`
	} `json:"metric"`
}

// DatasetFromSpecs builds a synthetic Dataset where every label's value set
// has the declared estimated size, so cardinality can be projected before a
// metric is ever shipped.
func DatasetFromSpecs(specs []Spec) *Dataset {
	ds := &Dataset{Metrics: map[string]*MetricInfo{}}
	for _, spec := range specs {
		info := ds.info(spec.Metric.Name)
		if spec.Metric.Type != "" {
			info.Type = spec.Metric.Type
		}
		for label, count := range spec.Metric.Labels {
			if count < 1 {
				count = 1
			}
			for i := 0; i < int(count); i++ {
				info.Samples = append(info.Samples, Sample{
					Name:   spec.Metric.Name,
					Labels: []Label{{Name: label, Value: fmt.Sprintf("~%d", i)}},
				})
			}
		}
		if len(spec.Metric.Labels) == 0 {
			info.Samples = append(info.Samples, Sample{Name: spec.Metric.Name})
		}
	}
	return ds
}
