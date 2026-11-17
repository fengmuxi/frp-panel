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
// 渲染前统一脱敏：报告可能被落盘或粘贴到工单，失败消息中的 DSN/凭证类内容一律遮盖。
func Render(report *RunReport, opts Options) error {
	redactReport(report)

	var (
		text string
		err  error
	)
	switch opts.Format {
	case "json":
		text, err = renderJSON(report)
	case "markdown", "md":
		text, err = renderMarkdown(report)
	case "prometheus", "prom":
		text, err = renderPrometheus(report)
	case "text", "":
		text, err = renderText(report, opts)
	default:
		return fmt.Errorf("unknown format %q (valid: text|json|markdown|prometheus)", opts.Format)
	}
	if err != nil {
		return err
	}

	fmt.Print(text)

	if opts.Out != "" {
		// 0600：报告含主机与数据细节，避免同机其他用户可读
		if err := os.WriteFile(opts.Out, []byte(text), 0o600); err != nil {
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

// renderPrometheus 输出 Prometheus textfile collector 兼容的指标文本：
// 每个检查项一行 gauge（状态在 status 标签中，供告警规则按标签过滤），
// 另附汇总计数与最近运行时间戳。
// 适配 node_exporter textfile collector：`--format prometheus --out /var/lib/node_exporter/textfile/frpp.prom`
func renderPrometheus(report *RunReport) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "# HELP frpp_check_result frp-panel 检查项结果（恒为 1，结论见 status 标签）\n")
	fmt.Fprintf(&b, "# TYPE frpp_check_result gauge\n")
	for _, r := range report.Results {
		fmt.Fprintf(&b, "frpp_check_result{group=%q,checker=%q,severity=%q,status=%q} 1\n",
			r.Group, r.Checker, r.Severity.String(), string(r.Status))
	}
	fmt.Fprintf(&b, "# HELP frpp_check_summary 按状态汇总的检查项计数\n")
	fmt.Fprintf(&b, "# TYPE frpp_check_summary gauge\n")
	for _, status := range []string{"pass", "fail", "warn", "skip"} {
		fmt.Fprintf(&b, "frpp_check_summary{status=%q} %d\n", status, report.Summary[status])
	}
	fmt.Fprintf(&b, "# HELP frpp_check_last_run_timestamp 最近一次检查的 Unix 时间戳\n")
	fmt.Fprintf(&b, "# TYPE frpp_check_last_run_timestamp gauge\n")
	fmt.Fprintf(&b, "frpp_check_last_run_timestamp %d\n", report.StartedAt.Unix())
	return b.String(), nil
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
	fmt.Fprintf(&b, "frp-panel check report  version=%s  role=%s  schema=v%d\n", report.Version, report.Role, report.SchemaVersion)
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
		// fail 与 warn 都输出修复建议：db_migration 等以 warn 结束的检查项最需要提示
		if (r.Status == StatusFail || r.Status == StatusWarn) && r.FixHint != "" {
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
	fmt.Fprintf(&b, "- 版本: `%s`\n- 角色: `%s`\n- 报告结构版本: v%d\n- 开始时间: %s\n- 总耗时: %s\n",
		report.Version, report.Role, report.SchemaVersion, report.StartedAt.Format(time.RFC3339), FormatDuration(report.Duration))
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
		if (r.Status == StatusFail || r.Status == StatusWarn) && r.FixHint != "" {
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
