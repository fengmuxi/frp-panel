package models

import (
	"time"

	"gorm.io/gorm"
)

// ProxyStatsHourly 记录每小时整点各隧道的"当日累计流量"快照（today_* 口径）。
//
// 用途：首页"今日"时段的小时粒度趋势。frps 内存统计没有小时维度、
// proxy_stats 又是覆盖写（无历史轨迹），因此由 master 每小时整点
// 把当前 today 值快照落库；相邻整点快照差分即该小时增量。
//
// 生命周期：仅服务"今天"的序列，超过 48 小时的旧快照由定时任务清理。
type ProxyStatsHourly struct {
	gorm.Model
	ProxyID int `gorm:"index:idx_proxy_hourly_unique,unique"`
	// HourTs 整点 unix 秒（master 本地时区），与 today_* 的自然日口径一致
	HourTs int64 `gorm:"index:idx_proxy_hourly_unique,unique;index"`

	// TodayIn/Out 是快照时刻的当日累计（不是小时增量，增量由查询侧差分得出）
	TodayIn  int64 `json:"today_in"`
	TodayOut int64 `json:"today_out"`
	// HistoryIn/Out 是快照时刻的"今日 00:00 全期累计"基线（跨零点翻转后的 history）。
	// 用于昨日等跨天序列的差分：昨天 24:00 的全期累计 = 次日 history；
	// nil 表示该补偿字段上线前的旧快照（旧快照无法参与跨天差分）。
	HistoryIn  *int64 `json:"history_in,omitempty"`
	HistoryOut *int64 `json:"history_out,omitempty"`

	Name     string `json:"name" gorm:"index"`
	Type     string `json:"type"`
	ServerID string `json:"server_id" gorm:"index"`
	ClientID string `json:"client_id" gorm:"index"`

	UserID   int `json:"user_id" gorm:"index"`
	TenantID int `json:"tenant_id" gorm:"index"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

func (*ProxyStatsHourly) TableName() string {
	return "proxy_stats_hourly"
}
