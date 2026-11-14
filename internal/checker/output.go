package checker

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mattn/go-isatty"
)

// Render 按格式输出报告：写终端（若 Out 为空）并写文件（若 Out 非空）。
func Render(report *RunReport, opts Options) error {
	var (
		text string
		err  error
	)
	switch opts.Format {
	case "json":
		text, err = renderJSON(report)
	case "markdown", "md":
		text, err = renderMarkdown(report)
	case "text", "":
		text, err = renderText(report, opts)
	default:
		return fmt.Errorf("unknown format %q (valid: text|json|markdown)", opts.Format)
	}
	if err != nil {
		return err
	}

	fmt.Print(text)

	if opts.Out != "" {
		if err := os.WriteFile(opts.Out, []byte(text), 0o644); err != nil {
			return fmt.Errorf("write report file %s: %w", opts.Out, err)
		}
		fmt.Printf("\nreport written to %s\n", opts.Out)
	}
	return nil
}

func renderJSON(report *RunReport) (string, error) {
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal report: %w", err)
	}
	return string(raw) + "\n", nil
}

var useColor = isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())

func colorize(s, code string) string {
	if !useColor {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func statusColor(r Result) string {
	switch r.Status {
	case StatusPass:
		return "32" // green
	case StatusFail:
		switch r.Severity {
		case LevelCritical:
			return "1;31" // bold red
		case LevelMajor:
			return "31" // red
		default:
			return "33" // yellow
		}
	case StatusWarn:
		return "33" // yellow
	default:
		return "90" // gray
	}
}

func renderText(report *RunReport, opts Options) (string, error) {
	useColor = useColor && !opts.NoColor

	var b strings.Builder
	fmt.Fprintf(&b, "frp-panel check report  version=%s  role=%s\n", report.Version, report.Role)
	fmt.Fprintf(&b, "started=%s  duration=%s\n",
		report.StartedAt.Format(time.RFC3339), FormatDuration(report.Duration))
	fmt.Fprintf(&b, "groups: %s\n\n", strings.Join(report.Groups, ", "))

	for _, r := range report.Results {
		line := fmt.Sprintf("[%s] %-8s %-32s %-8s %-24s %s",
			strings.ToUpper(string(r.Status)), r.Severity.String(), r.Group, r.Checker,
			FormatDuration(r.Duration), r.Message)
		b.WriteString(colorize(line, statusColor(r)) + "\n")
		for _, d := range r.Details {
			fmt.Fprintf(&b, "    - %s\n", d)
		}
		if r.Status == StatusFail && r.FixHint != "" {
			fmt.Fprintf(&b, "    fix: %s\n", r.FixHint)
		}
	}

	fmt.Fprintf(&b, "\nsummary: pass=%d fail=%d warn=%d skip=%d\n",
		report.Summary["pass"], report.Summary["fail"], report.Summary["warn"], report.Summary["skip"])

	if report.FailExit {
		b.WriteString(colorize("RESULT: FAILED (--fail-on "+opts.FailOn.String()+" triggered)\n", "1;31"))
	} else {
		b.WriteString(colorize("RESULT: PASSED\n", "1;32"))
	}
	return b.String(), nil
}

func renderMarkdown(report *RunReport) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "# frp-panel check report\n\n")
	fmt.Fprintf(&b, "- 版本: `%s`\n- 角色: `%s`\n- 开始时间: %s\n- 总耗时: %s\n",
		report.Version, report.Role, report.StartedAt.Format(time.RFC3339), FormatDuration(report.Duration))
	fmt.Fprintf(&b, "- 分组: %s\n\n", strings.Join(report.Groups, ", "))

	fmt.Fprintf(&b, "| 组 | 检查项 | 级别 | 结论 | 耗时 | 说明 |\n|---|---|---|---|---|---|\n")
	for _, r := range report.Results {
		fmt.Fprintf(&b, "| %s | `%s` | %s | %s | %s | %s |\n",
			r.Group, r.Checker, r.Severity.String(), string(r.Status),
			FormatDuration(r.Duration), mdEscape(r.Message))
	}

	b.WriteString("\n## 详情\n")
	for _, r := range report.Results {
		if r.Status == StatusPass && len(r.Details) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n### %s/%s — %s (%s)\n\n%s\n\n", r.Group, r.Checker, string(r.Status), r.Severity.String(), mdEscape(r.Message))
		if len(r.Details) > 0 {
			b.WriteString("| 明细 |\n|---|\n")
			for _, d := range r.Details {
				fmt.Fprintf(&b, "| %s |\n", mdEscape(d))
			}
			b.WriteString("\n")
		}
		if r.Status == StatusFail && r.FixHint != "" {
			fmt.Fprintf(&b, "> **修复建议**: %s\n\n", mdEscape(r.FixHint))
		}
		if len(r.ConfigRefList) > 0 {
			fmt.Fprintf(&b, "> 相关配置: %s\n\n", strings.Join(mdEscapeAll(r.ConfigRefList), ", "))
		}
	}

	fmt.Fprintf(&b, "\n## 汇总\n\n| 状态 | 数量 |\n|---|---|\n")
	fmt.Fprintf(&b, "| pass | %d |\n| fail | %d |\n| warn | %d |\n| skip | %d |\n",
		report.Summary["pass"], report.Summary["fail"], report.Summary["warn"], report.Summary["skip"])

	return b.String(), nil
}

func mdEscape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "|", "\\|"), "\n", "<br>")
}

func mdEscapeAll(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		out = append(out, mdEscape(s))
	}
	return out
}
