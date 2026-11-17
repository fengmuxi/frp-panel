package checker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// diff.go 实现两次报告的对比（check diff）与报告历史目录管理（--history-dir）。

// diffEntry 一条状态变化的明细。
type diffEntry struct {
	Group    string `json:"group"`
	Checker  string `json:"checker"`
	Severity string `json:"severity"`
	Old      string `json:"old_status"`
	New      string `json:"new_status"`
	Message  string `json:"message,omitempty"`
}

// DiffReport 两次报告的对比结论。
type DiffReport struct {
	SchemaVersion int         `json:"schema_version"`
	Old           string      `json:"old_report"`
	New           string      `json:"new_report"`
	NewFailures   []diffEntry `json:"new_failures"`        // 健康 → fail
	Resolved      []diffEntry `json:"resolved"`            // fail → 健康
	Worsened      []diffEntry `json:"worsened"`            // 健康 → warn 或 warn → fail
	Improved      []diffEntry `json:"improved"`            // warn → 健康
	Persistent    []diffEntry `json:"persistent_failures"` // 持续 fail
}

// LoadReport 从 JSON 文件读取一份报告。
func LoadReport(path string) (*RunReport, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read report %s: %w", path, err)
	}
	report := &RunReport{}
	if err := json.Unmarshal(raw, report); err != nil {
		return nil, fmt.Errorf("parse report %s: %w（schema_version=1 格式）", path, err)
	}
	return report, nil
}

// diffRank 把状态映射为健康度序（越小越健康），用于判定转好/恶化方向。
func diffRank(status string) int {
	switch Status(status) {
	case StatusPass, StatusSkip:
		return 0
	case StatusWarn:
		return 1
	case StatusFail:
		return 2
	}
	return 0
}

// CompareReports 按 (group, checker) 对账两次报告；新报告中首次出现且异常的项计入新增失败。
func CompareReports(old, new *RunReport) *DiffReport {
	key := func(r Result) string { return r.Group + "/" + r.Checker }

	oldStatus := map[string]Result{}
	for _, r := range old.Results {
		oldStatus[key(r)] = r
	}

	d := &DiffReport{
		SchemaVersion: reportSchemaVersion,
	}
	for _, r := range new.Results {
		entry := diffEntry{
			Group:    r.Group,
			Checker:  r.Checker,
			Severity: r.Severity.String(),
			Old:      string(oldStatus[key(r)].Status),
			New:      string(r.Status),
			Message:  r.Message,
		}
		oldEntry, existed := oldStatus[key(r)]
		oldRank := 0
		if existed {
			oldRank = diffRank(string(oldEntry.Status))
		}
		newRank := diffRank(string(r.Status))
		switch {
		case newRank == 2 && oldRank < 2:
			d.NewFailures = append(d.NewFailures, entry)
		case newRank == 2 && oldRank == 2:
			d.Persistent = append(d.Persistent, entry)
		case newRank == 1 && oldRank < 1:
			d.Worsened = append(d.Worsened, entry)
		case oldRank == 2 && newRank < 2:
			d.Resolved = append(d.Resolved, entry)
		case oldRank == 1 && newRank < 1:
			d.Improved = append(d.Improved, entry)
		}
	}
	return d
}

// RenderDiffText 输出对比报告的终端文本；仅列出需要关注的类别。
func RenderDiffText(d *DiffReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "报告对比: %s → %s\n", d.Old, d.New)
	fmt.Fprintf(&b, "新增失败=%d 已修复=%d 恶化=%d 转好=%d 持续失败=%d\n\n",
		len(d.NewFailures), len(d.Resolved), len(d.Worsened), len(d.Improved), len(d.Persistent))

	list := func(title string, entries []diffEntry) {
		if len(entries) == 0 {
			return
		}
		fmt.Fprintf(&b, "%s (%d):\n", title, len(entries))
		for _, e := range entries {
			fmt.Fprintf(&b, "  [%s/%s] %s → %s: %s\n", e.Group, e.Checker, e.Old, e.New, e.Message)
		}
		b.WriteString("\n")
	}
	list("新增失败", d.NewFailures)
	list("恶化", d.Worsened)
	list("已修复", d.Resolved)
	list("转好", d.Improved)
	list("持续失败", d.Persistent)
	if len(d.NewFailures) == 0 && len(d.Worsened) == 0 && len(d.Persistent) == 0 {
		b.WriteString("RESULT: 无恶化项\n")
	} else {
		b.WriteString("RESULT: 存在恶化项\n")
	}
	return b.String()
}

// RenderDiffSummary 输出一行式对比摘要（check 运行尾部自动附上）。
func RenderDiffSummary(d *DiffReport) string {
	return fmt.Sprintf("\n与上次相比: +%d 新增失败 / %d 已修复 / %d 持续失败\n",
		len(d.NewFailures), len(d.Resolved), len(d.Persistent))
}

// historyKeep 每个历史目录保留的报告份数。
const historyKeep = 30

// SaveHistoryReport 把报告写入历史目录（check-<时间戳>.json），
// 返回保存前目录内最新一份报告的路径（用于对比）；目录不存在时自动创建。
func SaveHistoryReport(dir string, report *RunReport) (prevPath string, err error) {
	if dir == "" {
		return "", nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create history dir %s: %w", dir, err)
	}
	if prevPath, err = latestHistoryFile(dir); err != nil {
		return "", err
	}

	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return prevPath, fmt.Errorf("marshal report: %w", err)
	}
	path := filepath.Join(dir, fmt.Sprintf("check-%s.json", time.Now().Format("20060102-150405")))
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return prevPath, fmt.Errorf("write history report %s: %w", path, err)
	}
	pruneHistory(dir, historyKeep)
	return prevPath, nil
}

// latestHistoryFile 返回目录内修改时间最新的报告路径；目录为空时返回空串。
func latestHistoryFile(dir string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "check-*.json"))
	if err != nil || len(matches) == 0 {
		return "", nil
	}
	var newestPath string
	var newest time.Time
	for _, p := range matches {
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		if st.ModTime().After(newest) {
			newest, newestPath = st.ModTime(), p
		}
	}
	return newestPath, nil
}

// pruneHistory 按修改时间滚动清理，仅保留最近 keep 份报告。
func pruneHistory(dir string, keep int) {
	matches, err := filepath.Glob(filepath.Join(dir, "check-*.json"))
	if err != nil || len(matches) <= keep {
		return
	}
	type fileTime struct {
		path string
		t    time.Time
	}
	files := make([]fileTime, 0, len(matches))
	for _, p := range matches {
		if st, err := os.Stat(p); err == nil {
			files = append(files, fileTime{p, st.ModTime()})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].t.After(files[j].t) })
	for _, f := range files[keep:] {
		os.Remove(f.path)
	}
}
