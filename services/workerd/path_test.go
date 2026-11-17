package workerd

import (
	"path/filepath"
	"testing"

	"github.com/VaalaCat/frp-panel/pb"
	"github.com/stretchr/testify/assert"
)

// 本文件是安全审计 P0-2 的攻击复现与修复验证：
// WorkerId/CodeEntry 曾被原样拼进节点文件路径，`../` 即可以 frpp 运行身份写任意文件。

func TestValidateWorkerPathSegment(t *testing.T) {
	valid := []string{"w-1", "index.js", "src/index.js", "a_b", "Worker.2"}
	for _, s := range valid {
		assert.True(t, ValidateWorkerPathSegment(s), "should accept %q", s)
	}

	invalid := []string{
		"",               // 空
		"..",             // 纯上跳
		"../../etc",      // 穿越
		"w/../x",         // 混入上跳
		"a..b",           // 含 ..
		"/abs",           // 绝对路径
		`\win`,           // windows 风格
		"C:\\Windows\\x", // windows 绝对路径
	}
	for _, s := range invalid {
		assert.False(t, ValidateWorkerPathSegment(s), "should reject %q", s)
	}
}

func TestEnsureWorkerPathsSafe(t *testing.T) {
	cwd := t.TempDir()

	normal := &pb.Worker{WorkerId: ptr("w-1"), CodeEntry: ptr("index.js")}
	assert.NoError(t, EnsureWorkerPathsSafe(nil, normal, cwd))

	for _, w := range []*pb.Worker{
		{WorkerId: ptr("../../etc"), CodeEntry: ptr("index.js")},
		{WorkerId: ptr("w-1"), CodeEntry: ptr("../../evil")},
		{WorkerId: ptr("w-1"), CodeEntry: ptr("/abs")},
		{WorkerId: ptr(""), CodeEntry: ptr("index.js")},
	} {
		assert.Error(t, EnsureWorkerPathsSafe(nil, w, cwd),
			"workerId=%q codeEntry=%q must be rejected", w.GetWorkerId(), w.GetCodeEntry())
	}
}

// containment 断言：路径必须解析回 workerd 目录内部。
func TestEnsureUnderCwd(t *testing.T) {
	base := t.TempDir()

	assert.NoError(t, ensureUnderCwd(base, filepath.Join(base, "sub")))
	assert.Error(t, ensureUnderCwd(base, filepath.Dir(base)), "上跳出 base 必须被拒")
	assert.Error(t, ensureUnderCwd(base, "/etc"))
}

func ptr(s string) *string { return &s }
