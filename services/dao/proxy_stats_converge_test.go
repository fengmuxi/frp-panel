package dao

import (
	"testing"
	"time"

	"github.com/fengmuxi/frp-panel/models"
	"github.com/stretchr/testify/assert"
)

func statsRow(id int, userID int, updated time.Time) *models.ProxyStats {
	return &models.ProxyStats{ProxyStatsEntity: &models.ProxyStatsEntity{
		ProxyID:   id,
		UserID:    userID,
		Name:      "emby",
		UpdatedAt: updated,
	}}
}

// 归属漂移产生的孤儿行（user_id 不等于当前行归属者）不再接收推送、必然更旧，
// 保留行取最近更新的行时自然落选。
func TestPickKeptStatsRowPrefersNewest(t *testing.T) {
	now := time.Now()
	fresh := statsRow(2, 10, now)
	stale := statsRow(1, 10, now.Add(-time.Hour))
	drifted := statsRow(3, 99, now.Add(-2*time.Hour))

	pick := pickKeptStatsRow([]*models.ProxyStats{drifted, stale, fresh})
	assert.Same(t, fresh, pick)
}

// 多用户前缀去名：按所属用户的 "userName." 前缀剥离，返回前缀归属用户 ID
func TestStripProxyStatsPrefix(t *testing.T) {
	userNames := map[int]string{1: "admin", 2: "ab", 3: ""}

	name, owner := stripProxyStatsPrefix("admin.emby", userNames)
	assert.Equal(t, "emby", name)
	assert.Equal(t, 1, owner)

	// 短前缀误吞防护："ab.emby" 必须按 "ab" 剥离而非 "a"
	name, owner = stripProxyStatsPrefix("ab.emby", map[int]string{1: "a", 2: "ab"})
	assert.Equal(t, "emby", name)
	assert.Equal(t, 2, owner)

	// 无已知前缀（multiuser 插件未启用，frps 上报裸名）：原样返回、归属用户 0
	name, owner = stripProxyStatsPrefix("emby", userNames)
	assert.Equal(t, "emby", name)
	assert.Equal(t, 0, owner)

	// 空用户名不参与前缀匹配
	name, owner = stripProxyStatsPrefix(".emby", userNames)
	assert.Equal(t, ".emby", name)
	assert.Equal(t, 0, owner)
}
