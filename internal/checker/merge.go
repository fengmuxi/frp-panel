package checker

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// merge.go 多节点报告聚合：把多台机器各自产出的 report.json 合成一份总览。

// MergedNode 单节点的报告摘要。
type MergedNode struct {
	Host       string         `json:"host,omitempty"`
	Role       string         `json:"role,omitempty"`
	Version    string         `json:"version,omitempty"`
	StartedAt  time.Time      `json:"started_at"`
	Summary    map[string]int `json:"summary"`
	BySeverity map[string]int `json:"by_severity"`
	FailExit   bool           `json:"fail_exit"`
}

// MergedFailure 聚合后的异常/告警明细。
type MergedFailure struct {
	Host     string `json:"host,omitempty"`
	Group    string `json:"group"`
	Checker  string `json:"checker"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// MergedOverview 聚合总览。
type MergedOverview struct {
	SchemaVersion int             `json:"schema_version"`
	GeneratedAt   time.Time       `json:"generated_at"`
	NodeCount     int             `json:"node_count"`
	Nodes         []MergedNode    `json:"nodes"`
	TotalSummary  map[string]int  `json:"total_summary"`
	Failures      []MergedFailure `json:"failures"`
	Warnings      []MergedFailure `json:"warnings"`
}

// MergeReports 聚合多份报告：按节点汇总计数，抽取全部 fail/warn 明细。
func MergeReports(reports []*RunReport) *MergedOverview {
	m := &MergedOverview{
		SchemaVersion: reportSchemaVersion,
		GeneratedAt:   time.Now(),
		TotalSummary:  map[string]int{},
	}
	for _, r := range reports {
		if r == nil {
			continue
		}
		m.Nodes = append(m.Nodes, MergedNode{
			Host:       r.Host,
			Role:       r.Role,
			Version:    r.Version,
			StartedAt:  r.StartedAt,
			Summary:    r.Summary,
			BySeverity: r.BySeverity,
			FailExit:   r.FailExit,
		})
		for status, n := range r.Summary {
			m.TotalSummary[status] += n
		}
		for _, res := range r.Results {
			entry := MergedFailure{
				Host:     r.Host,
				Group:    res.Group,
				Checker:  res.Checker,
				Severity: res.Severity.String(),
				Message:  res.Message,
			}
			switch res.Status {
			case StatusFail:
				m.Failures = append(m.Failures, entry)
			case StatusWarn:
				m.Warnings = append(m.Warnings, entry)
			}
		}
	}
	m.NodeCount = len(m.Nodes)
	return m
}

// RenderMergedJSON 聚合总览的 JSON 输出。
func RenderMergedJSON(m *MergedOverview) (string, error) {
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal merged overview: %w", err)
	}
	return string(raw) + "\n", nil
}

// RenderMergedMarkdown 聚合总览的 Markdown 输出。
func RenderMergedMarkdown(m *MergedOverview) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "# frp-panel 多节点检查总览\n\n")
	fmt.Fprintf(&b, "- 生成时间: %s\n- 节点数: %d\n\n", m.GeneratedAt.Format("2006-01-02 15:04:05"), m.NodeCount)

	fmt.Fprintf(&b, "## 节点概览\n\n| 主机 | 角色 | 版本 | pass | fail | warn | skip |\n|---|---|---|---|---|---|---|\n")
	for _, n := range m.Nodes {
		fmt.Fprintf(&b, "| %s | %s | %s | %d | %d | %d | %d |\n",
			mdEscape(n.Host), mdEscape(n.Role), mdEscape(n.Version),
			n.Summary["pass"], n.Summary["fail"], n.Summary["warn"], n.Summary["skip"])
	}
	fmt.Fprintf(&b, "\n**汇总**: pass=%d fail=%d warn=%d skip=%d\n\n",
		m.TotalSummary["pass"], m.TotalSummary["fail"], m.TotalSummary["warn"], m.TotalSummary["skip"])

	list := func(title string, entries []MergedFailure) {
		fmt.Fprintf(&b, "## %s (%d)\n\n", title, len(entries))
		if len(entries) == 0 {
			fmt.Fprintf(&b, "无。\n\n")
			return
		}
		fmt.Fprintf(&b, "| 主机 | 检查项 | 级别 | 说明 |\n|---|---|---|---|\n")
		for _, e := range entries {
			fmt.Fprintf(&b, "| %s | `%s/%s` | %s | %s |\n",
				mdEscape(e.Host), e.Group, e.Checker, e.Severity, mdEscape(e.Message))
		}
		b.WriteString("\n")
	}
	list("失败明细", m.Failures)
	list("告警明细", m.Warnings)
	return b.String(), nil
}
