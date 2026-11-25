package dao

import (
	"errors"
	"fmt"
	"time"

	"github.com/fengmuxi/frp-panel/models"
	"gorm.io/gorm"
)

const (
	MSetBatchSize = 100
)

type StatsQuery interface {
	GetHistoryStatsByProxyID(userInfo models.UserInfo, proxyID int) ([]*models.HistoryProxyStats, error)
	GetHistoryStatsByClientID(userInfo models.UserInfo, clientID string) ([]*models.HistoryProxyStats, error)
	GetHistoryStatsByServerID(userInfo models.UserInfo, serverID string) ([]*models.HistoryProxyStats, error)
	GetHistoryStatsInRange(userInfo models.UserInfo, from, to time.Time) ([]*models.HistoryProxyStats, error)
	AdminGetQuotaHistoryBaseline(proxyID int, windowStart time.Time) (baselineIn, baselineOut int64, found bool, err error)
}

type StatsMutation interface {
	AdminSaveTodyStats(s *models.HistoryProxyStats) error
	AdminMSaveTodyStats(tx *gorm.DB, s []*models.HistoryProxyStats) error
}

type statsQuery struct{ *queryImpl }
type statsMutation struct{ *mutationImpl }

func newStatsQuery(base *queryImpl) StatsQuery          { return &statsQuery{base} }
func newStatsMutation(base *mutationImpl) StatsMutation { return &statsMutation{base} }

func (m *statsMutation) AdminSaveTodyStats(s *models.HistoryProxyStats) error {
	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()
	return db.Save(s).Error
}

func (m *statsMutation) AdminMSaveTodyStats(tx *gorm.DB, s []*models.HistoryProxyStats) error {
	if len(s) == 0 {
		return nil
	}

	if err := tx.CreateInBatches(s, MSetBatchSize).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("AdminMSaveTodyStats failed to save history proxy stats: %v", err)
	}
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("AdminMSaveTodyStats failed to commit transaction: %v", err)
	}
	return nil
}

func (q *statsQuery) GetHistoryStatsByProxyID(userInfo models.UserInfo, proxyID int) ([]*models.HistoryProxyStats, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	var stats []*models.HistoryProxyStats
	err := db.Where(&models.HistoryProxyStats{
		ProxyID:  proxyID,
		UserID:   userInfo.GetUserID(),
		TenantID: userInfo.GetTenantID(),
	}).Find(&stats).Error
	if err != nil {
		return nil, err
	}
	return stats, nil
}

func (q *statsQuery) GetHistoryStatsByClientID(userInfo models.UserInfo, clientID string) ([]*models.HistoryProxyStats, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	var stats []*models.HistoryProxyStats
	err := db.Where(&models.HistoryProxyStats{
		ClientID: clientID,
		UserID:   userInfo.GetUserID(),
		TenantID: userInfo.GetTenantID(),
	}).Find(&stats).Error
	if err != nil {
		return nil, err
	}
	return stats, nil
}

func (q *statsQuery) GetHistoryStatsByServerID(userInfo models.UserInfo, serverID string) ([]*models.HistoryProxyStats, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	var stats []*models.HistoryProxyStats
	err := db.Where(&models.HistoryProxyStats{
		ServerID: serverID,
		UserID:   userInfo.GetUserID(),
		TenantID: userInfo.GetTenantID(),
	}).Find(&stats).Error
	if err != nil {
		return nil, err
	}
	return stats, nil
}

// GetHistoryStatsInRange 查询 [from, to] 时间范围内当前用户（含 UserID=0 全局记录）的全部每日快照，按 proxy_id、created_at 升序
func (q *statsQuery) GetHistoryStatsInRange(userInfo models.UserInfo, from, to time.Time) ([]*models.HistoryProxyStats, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	var stats []*models.HistoryProxyStats
	err := db.
		Where("created_at >= ? AND created_at <= ?", from, to).
		Where("user_id IN ?", []int{userInfo.GetUserID(), 0}).
		Where("tenant_id = ?", userInfo.GetTenantID()).
		Order("proxy_id, created_at").
		Find(&stats).Error
	if err != nil {
		return nil, err
	}
	return stats, nil
}

// AdminGetQuotaHistoryBaseline 返回限额统计窗口内最早一次每日快照的累计流量，
// 入站/出站分开返回（限额支持按方向统计后，基线也需要分方向做差分）。
// found=false 表示窗口开始后尚无快照（调用方应回退为当前 History 作为基线）。
// 快照记录的是“截至快照时刻的历史累计值”，因此取窗口内最早的快照作为窗口起点基线最接近真实值。
func (q *statsQuery) AdminGetQuotaHistoryBaseline(proxyID int, windowStart time.Time) (baselineIn, baselineOut int64, found bool, err error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()

	var snap models.HistoryProxyStats
	e := db.Where("proxy_id = ?", proxyID).
		Where("created_at >= ?", windowStart).
		Order("created_at ASC").
		First(&snap).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return 0, 0, false, nil
	}
	if e != nil {
		return 0, 0, false, e
	}
	return snap.TrafficIn, snap.TrafficOut, true, nil
}
