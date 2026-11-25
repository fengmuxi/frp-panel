package dao

import (
	"fmt"

	"github.com/fengmuxi/frp-panel/models"
	"gorm.io/gorm"
)

// ProxyStatsTickBucket 是按 tickSeriesBucketSeconds 聚合后的时段流量增量。
type ProxyStatsTickBucket struct {
	Ts       int64 `json:"ts"`       // 桶起点整点秒
	DeltaIn  int64 `json:"delta_in"` // 桶内入站增量合计
	DeltaOut int64 `json:"delta_out"`
}

// AdminSaveProxyStatTicks 在统计入库事务内批量写入本推送周期的增量流水。
func (m *proxyMutation) AdminSaveProxyStatTicks(tx *gorm.DB, ticks []*models.ProxyStatsTick) error {
	if len(ticks) == 0 {
		return nil
	}
	return tx.CreateInBatches(ticks, MSetBatchSize).Error
}

// AdminDeleteProxyStatTicksBefore 清理指定时刻之前的增量流水（只服务最近两天的序列）。
func (m *proxyMutation) AdminDeleteProxyStatTicksBefore(beforeTs int64) error {
	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()
	return db.Where("ts < ?", beforeTs).Delete(&models.ProxyStatsTick{}).Error
}

// AdminGetProxyStatTickBucketsInRange 返回 [from, to] 内当前用户可见的增量流水，
// 已按 tickSeriesBucketSeconds 秒桶聚合（ts - (ts % bucket) 分组，sqlite/mysql
// 方言通用的整数取模表达）。仅聚合并返回桶和，不在此处做符号钳制。
func (q *proxyQuery) AdminGetProxyStatTickBucketsInRange(userInfo models.UserInfo, from, to int64, bucket int64) ([]*ProxyStatsTickBucket, error) {
	if bucket <= 0 {
		return nil, fmt.Errorf("invalid bucket seconds")
	}
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	buckets := []*ProxyStatsTickBucket{}
	groupExpr := fmt.Sprintf("(ts - (ts %% %d))", bucket)
	err := db.Model(&models.ProxyStatsTick{}).
		Select(fmt.Sprintf("ts - (ts %% %d) AS ts, SUM(delta_in) AS delta_in, SUM(delta_out) AS delta_out", bucket)).
		Where("ts >= ? AND ts <= ?", from, to).
		Where("user_id IN ?", []int{userInfo.GetUserID(), 0}).
		Where("tenant_id = ?", userInfo.GetTenantID()).
		Group(groupExpr).
		Order("ts ASC").
		Scan(&buckets).Error
	if err != nil {
		return nil, err
	}
	return buckets, nil
}
