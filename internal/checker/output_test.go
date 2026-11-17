package checker

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// newRenderTestReport 构造最小报告：一项 fail、一项带 FixHint 的 warn、一项 pass。
func newRenderTestReport() *RunReport {
	return &RunReport{
		Version: "test",
		Role:    "master",
		Groups:  []string{"data"},
		Results: []Result{
			{Checker: "schema_diff", Group: "data", Severity: LevelMajor, Status: StatusFail, Message: "缺少字段", FixHint: "启动一次 master 触发 AutoMigrate"},
			{Checker: "db_migration", Group: "data", Severity: LevelMinor, Status: StatusWarn, Message: "迁移落后于代码", FixHint: "用当前版本 frpp 启动一次 master 即可应用未跑完的迁移"},
			{Checker: "soft_deleted", Group: "data", Severity: LevelInfo, Status: StatusPass, Message: "无软删除残留"},
		},
		Summary:    map[string]int{"pass": 1, "fail": 1, "warn": 1},
		BySeverity: map[string]int{},
	}
}

// warn 级别的检查项（如 data/db_migration）也应输出修复建议。
func TestRenderTextShowsWarnFixHint(t *testing.T) {
	text, err := renderText(newRenderTestReport(), Options{NoColor: true})
	assert.NoError(t, err)
	assert.Contains(t, text, "fix: 启动一次 master 触发 AutoMigrate")
	assert.Contains(t, text, "fix: 用当前版本 frpp 启动一次 master 即可应用未跑完的迁移")
}

func TestRenderMarkdownShowsWarnFixHint(t *testing.T) {
	text, err := renderMarkdown(newRenderTestReport())
	assert.NoError(t, err)
	assert.Contains(t, text, "> **修复建议**: 用当前版本 frpp 启动一次 master 即可应用未跑完的迁移")
	assert.NotContains(t, text, "fix: 无软删除残留")
}

// pass 项永远不输出修复建议，避免噪音。
func TestRenderTextPassNoFixHint(t *testing.T) {
	text, err := renderText(newRenderTestReport(), Options{NoColor: true})
	assert.NoError(t, err)
	assert.NotContains(t, text, "fix: 无软删除残留")
}

// JSON 报告结构：schema_version 与运行参数回显；凭证字段永不回显。
func TestRenderJSONSchemaVersionAndOpts(t *testing.T) {
	report := newRenderTestReport()
	report.SchemaVersion = reportSchemaVersion
	report.Opts = RunOptsSnapshot{
		Format: "json", FailOn: "major", Timeout: "2m0s", CheckerTimeout: "15s",
		Concurrency: 4, Destructive: true, Node: "node-a", HasCredentials: true,
	}
	text, err := renderJSON(report)
	assert.NoError(t, err)
	assert.Contains(t, text, `"schema_version": 1`)
	assert.Contains(t, text, `"fail_on": "major"`)
	assert.Contains(t, text, `"has_credentials": true`)
	assert.Contains(t, text, `"node": "node-a"`)
	assert.NotContains(t, text, "password")
	assert.NotContains(t, text, "skipped_notes")
}
