package checker

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// 分组不存在时必须报错——避免用户敲错命令却得到"全绿"报告。
func TestCollectCheckersUnknownGroup(t *testing.T) {
	_, err := CollectCheckers(&Env{}, []string{"nonexistent"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown check group")
}

// 重复分组去重，每个 Checker 只出现一次。
func TestCollectCheckersDedupGroups(t *testing.T) {
	checkers, err := CollectCheckers(&Env{}, []string{"data", "data"})
	assert.NoError(t, err)
	seen := map[string]int{}
	for _, c := range checkers {
		seen[c.Name()]++
	}
	for name, cnt := range seen {
		assert.Equal(t, 1, cnt, "checker %s duplicated", name)
	}
	assert.NotEmpty(t, seen)
}

// 空集合必须报错。
func TestCollectCheckersEmpty(t *testing.T) {
	_, err := CollectCheckers(&Env{}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no checker collected")
}

// 注册表完整性：每个分组都能构建且非空；--list 输出覆盖全部分组。
func TestAllCheckerNamesCoversGroups(t *testing.T) {
	for _, group := range GroupNames {
		checkers, err := CollectCheckers(&Env{}, []string{group})
		assert.NoError(t, err, "group %s", group)
		assert.NotEmpty(t, checkers, "group %s", group)
	}
	names := AllCheckerNames()
	assert.NotEmpty(t, names)
	for _, group := range GroupNames {
		found := false
		for _, line := range names {
			if len(line) >= len(group) && line[:len(group)] == group {
				found = true
				break
			}
		}
		assert.True(t, found, "--list 缺少分组 %s", group)
	}
}
