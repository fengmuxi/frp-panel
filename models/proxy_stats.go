package models

import (
	"time"

	"gorm.io/gorm"
)

type ProxyStats struct {
	*ProxyStatsEntity
}

type ProxyStatsEntity struct {
	ProxyID           int    `json:"proxy_id" gorm:"primary_key;auto_increment"`
	ServerID          string `json:"server_id" gorm:"index"`
	ClientID          string `json:"client_id" gorm:"index"`
	OriginClientID    string `json:"origin_client_id" gorm:"index"`
	Name              string `json:"name"`
	Type              string `json:"type"`
	UserID            int    `json:"user_id" gorm:"index"`
	TenantID          int    `json:"tenant_id" gorm:"index"`
	TodayTrafficIn    int64  `json:"today_traffic_in"`
	TodayTrafficOut   int64  `json:"today_traffic_out"`
	HistoryTrafficIn  int64  `json:"history_traffic_in"`
	HistoryTrafficOut int64  `json:"history_traffic_out"`
	// ReportedTodayIn/Out 记录最近一次收到的 frps 内存当日计数（frps 重启会清零）。
	// 用于检测上报值回退（frps 重启/计数轮转），使 master 侧的今日累计跨 frps 重启连续；
	// nil 表示该补偿字段上线前没有记录（首次上报按旧口径衔接）。
	ReportedTodayIn  *int64 `json:"reported_today_in,omitempty"`
	ReportedTodayOut *int64 `json:"reported_today_out,omitempty"`
	// StatsDate 是 today_* 计数的归属自然日（frps 节点本地日，YYYY-MM-DD）。
	// 跨天翻转按该日期变化判断，而非 master 本地日——节点与 master 时区
	// 不一致、或推送延迟跨零点时仍能正确翻转；空表示旧节点未上报（回退
	// 到按 UpdatedAt 的本地日判断）。
	StatsDate string `json:"stats_date" gorm:"default:''"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (*ProxyStats) TableName() string {
	return "proxy_stats"
}

// HistoryProxyStats 历史流量统计，不保证精准，只是为了展示。精准请使用 proxies 表中的 history_traffic_in/history_traffic_out
// 后续看看是否要改成时序类数据 https://github.com/nakabonne/tstorage
type HistoryProxyStats struct {
	gorm.Model
	ProxyID        int    `json:"proxy_id" gorm:"index"`
	ServerID       string `json:"server_id" gorm:"index"`
	ClientID       string `json:"client_id" gorm:"index"`
	OriginClientID string `json:"origin_client_id" gorm:"index"`
	Name           string `json:"name"`
	Type           string `json:"type"`
	UserID         int    `json:"user_id" gorm:"index"`
	TenantID       int    `json:"tenant_id" gorm:"index"`
	TrafficIn      int64  `json:"traffic_in"`
	TrafficOut     int64  `json:"traffic_out"`
}

func (*HistoryProxyStats) TableName() string {
	return "history_proxy_stats"
}
