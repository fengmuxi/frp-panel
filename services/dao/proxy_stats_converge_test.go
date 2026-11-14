package dao

import (
	"testing"
	"time"

	"github.com/VaalaCat/frp-panel/models"
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

// 归属漂移产生的孤儿行（user_id 不等于当前节点归属者）不参与保留，
// 即使它更新时间更晚也不应被选中。
func TestPickKeptStatsRowPrefersOwnerRow(t *testing.T) {
	now := time.Now()
	owner := statsRow(2, 10, now.Add(-time.Minute))
	orphan := statsRow(1, 99, now)

	pick := pickKeptStatsRow([]*models.ProxyStats{orphan, owner}, 10)
	assert.Same(t, owner, pick)
}

// 两行都不属于当前归属者时（如节点整体转移），取最近更新的行。
func TestPickKeptStatsRowFallsBackToNewest(t *testing.T) {
	now := time.Now()
	old := statsRow(1, 99, now.Add(-time.Hour))
	newest := statsRow(2, 88, now)

	pick := pickKeptStatsRow([]*models.ProxyStats{old, newest}, 10)
	assert.Same(t, newest, pick)
}

// 同为归属者的多行取最近更新的行。
func TestPickKeptStatsRowAmongOwnerRows(t *testing.T) {
	now := time.Now()
	stale := statsRow(1, 10, now.Add(-time.Hour))
	fresh := statsRow(2, 10, now)

	pick := pickKeptStatsRow([]*models.ProxyStats{stale, fresh}, 10)
	assert.Same(t, fresh, pick)
}
