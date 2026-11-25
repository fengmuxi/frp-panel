package dao

import (
	"fmt"

	"github.com/fengmuxi/frp-panel/models"
	"gorm.io/gorm/clause"
)

// SaveHourlySnapshots 以 (proxy_id, hour_ts) 为幂等键写入一批小时快照，
// 同一小时重复执行（如 master 重启后的补写）会覆盖而非重复插入。
func (m *proxyMutation) SaveHourlySnapshots(snaps []*models.ProxyStatsHourly) error {
	if len(snaps) == 0 {
		return nil
	}
	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()
	// 分批 upsert，避免单语句过大
	const batchSize = 100
	for i := 0; i < len(snaps); i += batchSize {
		end := i + batchSize
		if end > len(snaps) {
			end = len(snaps)
		}
		if err := db.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "proxy_id"}, {Name: "hour_ts"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"today_in", "today_out", "history_in", "history_out", "name", "type", "server_id", "client_id", "user_id", "tenant_id", "updated_at",
			}),
		}).Create(snaps[i:end]).Error; err != nil {
			return fmt.Errorf("save hourly snapshots batch [%d,%d): %w", i, end, err)
		}
	}
	return nil
}

// DeleteHourlySnapshotsBefore 清理指定时刻之前的小时快照（只服务"今天"的序列，无需长存）。
func (m *proxyMutation) DeleteHourlySnapshotsBefore(beforeTs int64) error {
	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()
	return db.Unscoped().
		Where("hour_ts < ?", beforeTs).
		Delete(&models.ProxyStatsHourly{}).Error
}

// GetHourlySnapshotsByUser 返回 [fromTs, toTs] 内当前用户可见的小时快照
// （与 GetProxyStatsByUser 一致：user_id = 当前用户 或 0 全局记录）。
func (q *proxyQuery) GetHourlySnapshotsByUser(userInfo models.UserInfo, fromTs, toTs int64) ([]*models.ProxyStatsHourly, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	list := []*models.ProxyStatsHourly{}
	err := db.
		Where("hour_ts >= ? AND hour_ts <= ?", fromTs, toTs).
		Where("user_id = ? OR user_id = 0", userInfo.GetUserID()).
		Where("tenant_id = ?", userInfo.GetTenantID()).
		Order("hour_ts asc").
		Find(&list).Error
	if err != nil {
		return nil, err
	}
	return list, nil
}
