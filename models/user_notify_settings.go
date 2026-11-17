package models

import (
	"time"
)

// UserNotifySettings 每个用户自己的邮件通知开关与下线防抖配置（按用户独立，user_id 为主键）。
// 默认全部关闭（Go 零值），用户在「邮件通知」设置页自行开启。
type UserNotifySettings struct {
	UserID              int  `gorm:"primaryKey"`
	EnableServerOffline bool `gorm:"default:false"` // 服务端掉线提醒
	EnableClientOffline bool `gorm:"default:false"` // 客户端掉线提醒
	EnableProxyOffline  bool `gorm:"default:false"` // 隧道掉线提醒
	EnableLoginNotify   bool `gorm:"default:false"` // 用户登录通知
	EnableServerOnline  bool `gorm:"default:false"` // 服务端上线提醒
	EnableClientOnline  bool `gorm:"default:false"` // 客户端上线提醒
	EnableProxyOnline   bool `gorm:"default:false"` // 隧道上线提醒
	// 自动规则触发的提醒（按用户独立开关，默认关闭）
	EnableQuotaExceeded  bool `gorm:"default:false"` // 隧道流量超额关闭提醒
	EnableScheduleToggle bool `gorm:"default:false"` // 隧道定时开启/关闭提醒
	// 下线通知防抖时长（秒）：离线持续超过该时长才发送下线提醒；
	// 0 表示使用内置默认（defs.OfflineCheckDelay，60 秒）
	OfflineDelayServer int `gorm:"default:0"`
	OfflineDelayClient int `gorm:"default:0"`
	OfflineDelayProxy  int `gorm:"default:0"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func (*UserNotifySettings) TableName() string {
	return "user_notify_settings"
}
