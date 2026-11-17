package checker

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// sqliteReadOnlyDSN 是 checker 只读打开 sqlite 的唯一入口，历史上曾因
// 分支拼接产出 `?mode=ro?_pragma=...` 畸形 URI（Windows 下 check 全 skip），
// 这组用例固化各形态输入的正确输出。
func TestSQLiteReadOnlyDSN(t *testing.T) {
	cases := []struct {
		in  string
		out string
	}{
		// 裸路径（无 query）：修复过的畸形拼接场景
		{"data.db", "file:data.db?mode=ro&_pragma=busy_timeout(5000)"},
		// 项目默认 DSN 形态：journal_mode(WAL) 必须剔除——只读连接上设置 WAL
		// 会报 "unable to open database file (14)"（WAL 设置需要写权限）
		{"/data/data.db?_pragma=journal_mode(WAL)",
			"file:/data/data.db?mode=ro&_pragma=busy_timeout(5000)"},
		// 已带 file: 前缀与 mode 参数：不重复追加 mode
		{"file:data.db?mode=ro", "file:data.db?mode=ro&_pragma=busy_timeout(5000)"},
		// file URI 三斜杠形态
		{"file:///data/data.db", "file:///data/data.db?mode=ro&_pragma=busy_timeout(5000)"},
		// 内存库原样返回
		{":memory:", ":memory:"},
		{"file::memory:?cache=shared", "file::memory:?cache=shared"},
	}
	for _, c := range cases {
		assert.Equal(t, c.out, SQLiteReadOnlyDSN(c.in), "input: %s", c.in)
	}
}
