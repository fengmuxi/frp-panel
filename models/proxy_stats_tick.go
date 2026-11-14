package models

import (
	"time"

	"gorm.io/gorm"
)

// ProxyStatsTick 记录每次节点推送（默认 30s 一次）计算出的隧道流量增量。
//
// 用途：首页"今日/昨日"时段的分钟粒度实时趋势——查询侧按 5 分钟桶聚合
// （SQL 端 ts - (ts % bucket) 分组，方言无关），无需等整点快照。
// 与 proxy_stats_hourly 互补：无 tick 数据（部署初期）时查询侧回退
// 小时快照差分。
//
// 生命周期：仅服务最近两天的序列，超过 48 小时的旧行由每小时任务清理；
// 零增量的推送不落行，控制表体积。
type ProxyStatsTick struct {
	gorm.Model
	ProxyID int   `gorm:"index"`
	Ts      int64 `gorm:"index"` // 推送落库时刻（unix 秒，master 本地）

	// DeltaIn/Out 是本推送周期（约 30s）内的流量增量，非累计值
	DeltaIn  int64 `json:"delta_in"`
	DeltaOut int64 `json:"delta_out"`

	Name     string `json:"name" gorm:"index"`
	Type     string `json:"type"`
	ServerID string `json:"server_id" gorm:"index"`
	ClientID string `json:"client_id" gorm:"index"`

	UserID   int `json:"user_id" gorm:"index"`
	TenantID int `json:"tenant_id" gorm:"index"`

	CreatedAt time.Time
}

func (*ProxyStatsTick) TableName() string {
	return "proxy_stats_tick"
}
