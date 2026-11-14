package models

import (
	"time"
)

// SMTPSettings SMTP 邮件通知配置，单行表（ID 恒为 1）。
// 4 个通知开关默认关闭（Go 零值），需要显式在界面开启。
type SMTPSettings struct {
	ID                  int    `gorm:"primaryKey"`
	Host                string `gorm:"type:varchar(255)"`
	Port                int    `gorm:"default:465"`
	Username            string `gorm:"type:varchar(255)"`
	Password            string `gorm:"type:varchar(255)"`
	FromAddress         string `gorm:"column:from_address;type:varchar(255)"` // from 是 SQL 关键字，故用 from_address 列名
	FromName            string `gorm:"column:from_name;type:varchar(255)"`    // 可选的发件人显示名称（RFC 5322 display-name），留空则仅显示地址
	Encryption          string `gorm:"type:varchar(16);default:none"`         // none | starttls | ssl
	EnableServerOffline bool   `gorm:"default:false"`                         // 服务端掉线提醒
	EnableClientOffline bool   `gorm:"default:false"`                         // 客户端掉线提醒
	EnableProxyOffline  bool   `gorm:"default:false"`                         // 隧道掉线提醒
	EnableLoginNotify   bool   `gorm:"default:false"`                         // 用户登录通知
	EnableServerOnline  bool   `gorm:"default:false"`                         // 服务端上线提醒
	EnableClientOnline  bool   `gorm:"default:false"`                         // 客户端上线提醒
	EnableProxyOnline   bool   `gorm:"default:false"`                         // 隧道上线提醒
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

func (*SMTPSettings) TableName() string {
	return "smtp_settings"
}
