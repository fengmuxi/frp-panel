package proxy

import (
	"testing"
	"time"

	"github.com/fengmuxi/frp-panel/models"
	"github.com/stretchr/testify/assert"
)

var snapTestLoc = time.FixedZone("test", 8*3600)

// 行已跨天更新（UpdatedAt 在今天）：快照原样采用 today/history
func TestBuildHourlySnapshotFreshRow(t *testing.T) {
	now := time.Date(2026, 11, 13, 0, 0, 30, 0, snapTestLoc)
	row := &models.ProxyStatsEntity{
		ProxyID:           1,
		Name:              "emby",
		TodayTrafficIn:    30,
		TodayTrafficOut:   40,
		HistoryTrafficIn:  5000,
		HistoryTrafficOut: 4000,
	}
	row.UpdatedAt = now

	snap := buildHourlySnapshot(row, now.Unix()-now.Unix()%3600, now)
	assert.Equal(t, int64(30), snap.TodayIn)
	assert.Equal(t, int64(40), snap.TodayOut)
	assert.NotNil(t, snap.HistoryIn)
	assert.Equal(t, int64(5000), *snap.HistoryIn)
	assert.Equal(t, int64(4000), *snap.HistoryOut)
}

// 行尚未跨天翻转（UpdatedAt 在昨天，today_* 仍是昨日累计）：
// 快照必须按"翻转已发生"口径合成 today=0、history=history+today，
// 否则昨日全天流量会被记成今日 00:00 快照，趋势首点出现巨幅尖峰。
func TestBuildHourlySnapshotStaleRowSynthesizesRoll(t *testing.T) {
	now := time.Date(2026, 11, 13, 0, 0, 0, 0, snapTestLoc)
	row := &models.ProxyStatsEntity{
		ProxyID:           1,
		Name:              "emby",
		TodayTrafficIn:    300, // 昨日全天累计（尚未翻转）
		TodayTrafficOut:   200,
		HistoryTrafficIn:  5000,
		HistoryTrafficOut: 4000,
	}
	row.UpdatedAt = now.Add(-time.Minute) // 昨日 23:59

	snap := buildHourlySnapshot(row, now.Unix()-now.Unix()%3600, now)
	assert.Equal(t, int64(0), snap.TodayIn)
	assert.Equal(t, int64(0), snap.TodayOut)
	assert.NotNil(t, snap.HistoryIn)
	assert.Equal(t, int64(5300), *snap.HistoryIn)
	assert.Equal(t, int64(4200), *snap.HistoryOut)
}
