package metrics

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ToJSON serializes a report.
func ToJSON(rep *Report, indent bool) string {
	var b []byte
	var err error
	if indent {
		b, err = json.MarshalIndent(rep, "", "  ")
	} else {
		b, err = json.Marshal(rep)
	}
	if err != nil { // Report is plain data; this cannot realistically fail
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	return string(b)
}

// RenderText renders a terminal-friendly report.
func RenderText(rep *Report, opts Options) string {
	var b strings.Builder
	fmt.Fprintf(&b, "cardimax — %d metric(s), %d series observed, verdict: %s\n",
		len(rep.Metrics), rep.TotalSeries, rep.Verdict)
	if len(rep.AddedLabels) > 0 {
		fmt.Fprintf(&b, "projection: adding %s\n", describeAdded(counts(rep.AddedLabels)))
	}
	if len(rep.ParseErrors) > 0 {
		fmt.Fprintf(&b, "parse errors: %d (first: %s)\n", len(rep.ParseErrors), rep.ParseErrors[0])
	}
	b.WriteString("\n")

	rows := make([][]string, 0, len(rep.Metrics)+1)
	rows = append(rows, []string{"METRIC", "TYPE", "SERIES", "LABELS", "EST", "PROJECTED"})
	for _, m := range rep.Metrics {
		labelNames := make([]string, 0, len(m.Labels))
		for _, l := range m.Labels {
			labelNames = append(labelNames, fmt.Sprintf("%s(%d)", l.Name, l.Distinct))
		}
		rows = append(rows, []string{
			m.Name, m.Type, fmt.Sprint(m.Series), fmt.Sprint(m.LabelCount),
			fmt.Sprint(m.Estimate), fmt.Sprint(m.Projected),
		})
		if len(labelNames) > 0 {
			_ = labelNames
		}
	}
	b.WriteString(table(rows))
	b.WriteString("\n")

	b.WriteString("label detail\n")
	for _, m := range rep.Metrics {
		for _, l := range m.Labels {
			marker := ""
			if IsRiskyLabel(l.Name) {
				marker = "  <- identity-shaped"
			}
			if opts.LabelThreshold > 0 && l.Distinct > opts.LabelThreshold {
				marker = "  <- UNBOUNDED"
			}
			fmt.Fprintf(&b, "  %s{%s} : %d distinct values%s\n", m.Name, l.Name, l.Distinct, marker)
		}
	}
	b.WriteString("\n")

	if len(rep.Findings) == 0 {
		b.WriteString("findings: none — every metric is within budget\n")
	} else {
		fmt.Fprintf(&b, "findings: %d\n", len(rep.Findings))
		for i, f := range rep.Findings {
			fmt.Fprintf(&b, "  %d. [%s] %s", i+1, strings.ToUpper(string(f.Severity)), f.Kind)
			if f.Metric != "" {
				fmt.Fprintf(&b, "  %s", f.Metric)
			}
			if f.Label != "" {
				fmt.Fprintf(&b, "{%s}", f.Label)
			}
			b.WriteString("\n")
			fmt.Fprintf(&b, "     %s\n", f.Detail)
			fmt.Fprintf(&b, "     fix: %s\n", f.Remediation)
		}
	}

	if len(rep.Projections) > 0 {
		b.WriteString("\nprojection detail\n")
		for _, p := range rep.Projections {
			state := "ok"
			if p.Overflow {
				state = "OVERFLOW"
			}
			fmt.Fprintf(&b, "  %s: %d -> %d  (+%s)  [%s]\n", p.Metric, p.Before, p.After, p.Added, state)
		}
	}
	return b.String()
}

// RenderMarkdown renders a report as a GitHub-friendly document.
func RenderMarkdown(rep *Report, opts Options) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# cardimax report\n\n")
	fmt.Fprintf(&b, "- metrics: **%d**\n- series observed: **%d**\n- verdict: **%s**\n",
		len(rep.Metrics), rep.TotalSeries, rep.Verdict)
	if len(rep.AddedLabels) > 0 {
		fmt.Fprintf(&b, "- projection: `%s`\n", describeAdded(counts(rep.AddedLabels)))
	}
	b.WriteString("\n## Metrics\n\n| Metric | Type | Series | Labels | Estimate | Projected |\n|---|---|---|---|---|---|\n")
	for _, m := range rep.Metrics {
		fmt.Fprintf(&b, "| `%s` | %s | %d | %d | %d | %d |\n",
			m.Name, m.Type, m.Series, m.LabelCount, m.Estimate, m.Projected)
	}
	b.WriteString("\n## Findings\n\n")
	if len(rep.Findings) == 0 {
		b.WriteString("No findings — every metric is within budget.\n")
	}
	for i, f := range rep.Findings {
		target := f.Metric
		if f.Label != "" {
			target = f.Metric + "{" + f.Label + "}"
		}
		fmt.Fprintf(&b, "%d. **%s** · `%s` — %s\n   - fix: %s\n", i+1, f.Severity, target, f.Detail, f.Remediation)
	}
	if len(rep.Projections) > 0 {
		b.WriteString("\n## Projections\n\n| Metric | Before | After | Added | Overflow |\n|---|---|---|---|---|\n")
		for _, p := range rep.Projections {
			fmt.Fprintf(&b, "| `%s` | %d | %d | `%s` | %t |\n", p.Metric, p.Before, p.After, p.Added, p.Overflow)
		}
	}
	return b.String()
}

func counts(labels []Label) map[string]int64 {
	out := map[string]int64{}
	for _, l := range labels {
		out[l.Name] = parseCount(l.Value)
	}
	return out
}

func table(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for i, cell := range row {
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}
	var b strings.Builder
	for r, row := range rows {
		for i, cell := range row {
			if i > 0 {
				b.WriteString("  ")
			}
			if r == 0 || i == len(row)-1 {
				b.WriteString(cell)
			} else {
				fmt.Fprintf(&b, "%-*s", widths[i], cell)
			}
		}
		b.WriteString("\n")
		if r == 0 {
			for i, w := range widths {
				if i > 0 {
					b.WriteString("  ")
				}
				b.WriteString(strings.Repeat("-", w))
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}
