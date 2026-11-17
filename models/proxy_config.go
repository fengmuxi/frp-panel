package models

import (
	"fmt"

	"github.com/VaalaCat/frp-panel/defs"
	"github.com/VaalaCat/frp-panel/pb"
	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/samber/lo"
	"gorm.io/gorm"
)

type ProxyConfig struct {
	*gorm.Model
	*ProxyConfigEntity

	WorkerID string `gorm:"type:varchar(255);index"` // 引用的worker
	Worker   Worker
}

type ProxyConfigEntity struct {
	ServerID       string `json:"server_id" gorm:"index"`
	ClientID       string `json:"client_id" gorm:"index"`
	Name           string `json:"name" gorm:"index"`
	Type           string `json:"type" gorm:"index"`
	UserID         int    `json:"user_id" gorm:"index"`
	TenantID       int    `json:"tenant_id" gorm:"index"`
	OriginClientID string `json:"origin_client_id" gorm:"index"`
	Content        []byte `json:"content"`
	Stopped        bool   `json:"stopped" gorm:"index"`

	// 自动规则：定时开关（ScheduleEnabled 时按时间窗口与周几自动启停）
	ScheduleEnabled bool           `json:"schedule_enabled" gorm:"index"`
	ScheduleStart   string         `json:"schedule_start"` // HH:MM 开启时间（分钟精度）
	ScheduleEnd     string         `json:"schedule_end"`   // HH:MM 关闭时间，start > end 表示跨天（如 22:00-06:00）
	ScheduleDays    GormArray[int] `json:"schedule_days"`  // 生效周几，0=周日 ~ 6=周六，多选；空=不生效
	// 自动规则：流量限额（QuotaType 非空且 QuotaLimit > 0 时生效），达到限额自动停用
	QuotaType  string `json:"quota_type" gorm:"index"` // defs.QuotaType* 取值，空=未启用
	QuotaLimit int64  `json:"quota_limit"`             // 限额字节数
	// QuotaDirection 是限额的统计方向（defs.QuotaDirection* 取值）；
	// 空=双向合计（历史数据兼容口径）
	QuotaDirection string `json:"quota_direction" gorm:"default:''"`
	// 是否被自动规则（定时窗口/流量限额）停用，用于区分手动暂停：
	// 手动停用的隧道不会被自动规则拉起，自动停用的隧道在规则允许时会被自动恢复
	AutoStopped bool `json:"auto_stopped" gorm:"index"`
}

func (*ProxyConfig) TableName() string {
	return "proxy_config"
}

func (p *ProxyConfigEntity) FillTypedProxyConfig(cfg v1.TypedProxyConfig) error {
	var err error
	p.Name = cfg.GetBaseConfig().Name
	p.Type = cfg.GetBaseConfig().Type
	p.Content, err = cfg.MarshalJSON()
	return err
}

func (p *ProxyConfigEntity) FillClientConfig(cli *ClientEntity) error {
	if cli == nil {
		return fmt.Errorf("invalid client, client is nil")
	}
	p.ServerID = cli.ServerID
	p.ClientID = cli.ClientID
	p.UserID = cli.UserID
	p.TenantID = cli.TenantID
	p.OriginClientID = cli.OriginClientID
	return nil
}

func (p *ProxyConfigEntity) GetTypedProxyConfig() (v1.TypedProxyConfig, error) {
	var cfg v1.TypedProxyConfig
	err := cfg.UnmarshalJSON(p.Content)
	return cfg, err
}

func (p *ProxyConfig) GetTypedProxyConfig() (v1.TypedProxyConfig, error) {
	return p.ProxyConfigEntity.GetTypedProxyConfig()
}

func (p *ProxyConfig) FillClientConfig(cli *ClientEntity) error {
	return p.ProxyConfigEntity.FillClientConfig(cli)
}

func (p *ProxyConfig) FillTypedProxyConfig(cfg v1.TypedProxyConfig) error {
	annotations := cfg.GetBaseConfig().Annotations
	if len(annotations) > 0 {
		if annotations[defs.FrpProxyAnnotationsKey_Ingress] != "" && len(annotations[defs.FrpProxyAnnotationsKey_WorkerId]) > 0 {
			workerId := annotations[defs.FrpProxyAnnotationsKey_WorkerId]
			p.WorkerID = workerId
		}
	}

	return p.ProxyConfigEntity.FillTypedProxyConfig(cfg)
}

func (p *ProxyConfig) ToPB() *pb.ProxyConfig {
	return &pb.ProxyConfig{
		Id:             lo.ToPtr(uint32(p.ID)),
		Name:           lo.ToPtr(p.Name),
		Type:           lo.ToPtr(p.Type),
		Config:         lo.ToPtr(string(p.Content)),
		Stopped:        lo.ToPtr(p.Stopped),
		ServerId:       lo.ToPtr(p.ServerID),
		ClientId:       lo.ToPtr(p.ClientID),
		OriginClientId: lo.ToPtr(p.OriginClientID),
	}
}
