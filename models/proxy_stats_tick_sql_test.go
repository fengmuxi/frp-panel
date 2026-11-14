package models_test

import (
	"fmt"
	"testing"

	"github.com/VaalaCat/frp-panel/models"
	"github.com/stretchr/testify/assert"
)

// 验证 tick 桶聚合 SQL（ts - (ts % 300) 取模分组）在 sqlite 上可用且聚合正确。
// 该表达式为方言无关写法（mysql/postgres 同样支持整数取模），与服务端
// dao.AdminGetProxyStatTickBucketsInRange 的聚合方式保持一致。
func TestProxyStatsTickBucketSQLAggregation(t *testing.T) {
	db := newTestDB(t)
	assert.NoError(t, db.AutoMigrate(&models.ProxyStatsTick{}))

	base := int64(1760000000)
	ticks := []*models.ProxyStatsTick{
		{ProxyID: 1, Ts: base, DeltaIn: 10, DeltaOut: 5, UserID: 1, TenantID: 0},
		{ProxyID: 1, Ts: base + 30, DeltaIn: 20, DeltaOut: 5, UserID: 1, TenantID: 0},
		{ProxyID: 1, Ts: base + 400, DeltaIn: 7, DeltaOut: 1, UserID: 1, TenantID: 0},  // 下一桶
		{ProxyID: 1, Ts: base + 600, DeltaIn: 99, DeltaOut: 1, UserID: 2, TenantID: 0}, // 其他用户 → 过滤
	}
	for _, tick := range ticks {
		assert.NoError(t, db.Create(tick).Error)
	}

	var buckets []struct {
		Ts       int64
		DeltaIn  int64
		DeltaOut int64
	}
	bucket := int64(300)
	groupExpr := fmt.Sprintf("(ts - (ts %% %d))", bucket)
	err := db.Model(&models.ProxyStatsTick{}).
		Select(fmt.Sprintf("ts - (ts %% %d) AS ts, SUM(delta_in) AS delta_in, SUM(delta_out) AS delta_out", bucket)).
		Where("ts >= ? AND ts <= ?", base, base+1000).
		Where("user_id IN ?", []int{1, 0}).
		Where("tenant_id = ?", 0).
		Group(groupExpr).
		Order("ts ASC").
		Scan(&buckets).Error
	assert.NoError(t, err)
	assert.Len(t, buckets, 2)
	if len(buckets) == 2 {
		assert.Equal(t, base-(base%bucket), buckets[0].Ts)
		assert.Equal(t, int64(30), buckets[0].DeltaIn)
		assert.Equal(t, int64(10), buckets[0].DeltaOut)
		assert.Equal(t, int64(7), buckets[1].DeltaIn)
	}
}
