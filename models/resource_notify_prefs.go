package models

import (
	"time"
)

// 通知覆盖模式（三段式）
const (
	NotifyModeOff      = "off"      // 不提醒：该资源任何提醒一律不发（最高优先级）
	NotifyModeInherit  = "inherit"  // 全局配置（默认）：跟随所属用户的 user_notify_settings
	NotifyModeOverride = "override" // 独立配置：按本记录的覆盖字段执行
)

// 资源类型标识（ResourceNotifyPrefs.ResourceType 取值）
const (
	NotifyResTypeServer = "server"
	NotifyResTypeClient = "client"
	NotifyResTypeProxy  = "proxy"
)

// ResourceNotifyPrefs 资源级通知覆盖配置（服务端/客户端/隧道各自独立设置）。
// 无记录 = inherit（跟随用户全局），纯增量零迁移、向后兼容。
// 优先级链：off > override > 用户全局。
type ResourceNotifyPrefs struct {
	ID           int    `gorm:"primaryKey"`
	ResourceType string `gorm:"type:varchar(16);uniqueIndex:idx_res_notify"` // server | client | proxy
	ResourceID   string `gorm:"type:varchar(255);uniqueIndex:idx_res_notify"`
	UserID       int    `gorm:"index"` // 冗余：资源属主（admin 代设时也写属主）
	Mode         string `gorm:"type:varchar(16);default:'inherit'"`

	// 以下为 mode=override 时的独立配置（server/client 用前 3 项，proxy 全用）
	EnableOffline        bool `gorm:"default:false"` // 独立：掉线提醒
	EnableOnline         bool `gorm:"default:false"` // 独立：上线提醒
	OfflineDelay         int  `gorm:"default:0"`     // 独立：掉线防抖秒，0=默认 60s
	EnableQuotaExceeded  bool `gorm:"default:false"` // 仅 proxy：流量超额关闭提醒
	EnableScheduleToggle bool `gorm:"default:false"` // 仅 proxy：定时开关提醒

	CreatedAt time.Time
	UpdatedAt time.Time
}

func (*ResourceNotifyPrefs) TableName() string {
	return "resource_notify_prefs"
}
