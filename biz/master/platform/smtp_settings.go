package platform

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"github.com/fengmuxi/frp-panel/common"
	"github.com/fengmuxi/frp-panel/defs"
	"github.com/fengmuxi/frp-panel/models"
	"github.com/fengmuxi/frp-panel/services/app"
	"github.com/fengmuxi/frp-panel/services/dao"
	"github.com/fengmuxi/frp-panel/services/mailer"
	"github.com/gin-gonic/gin"
)

// maxOfflineDelaySeconds 下线通知防抖时长的上限（秒），0 表示使用内置默认 60s
const maxOfflineDelaySeconds = 3600

// maxSMTPPasswordLength SMTP 密码明文长度上限（字节）：静态加密后密文长度
// = 64 + 2×len，超过 95 字节会超出 varchar(255) 列宽
const maxSMTPPasswordLength = 95

// smtpSaveMu 串行化 SMTP 设置的读-改-写保存：密码留空保留旧值的语义
// 在并发保存交错下会把新密码回滚为旧值（丢失更新）
var smtpSaveMu sync.Mutex

// smtpSettingsDTO SMTP 服务器连接配置（仅管理员可改）。
// 通知开关已拆分为按用户独立配置，见 notify_prefs.go；
// Password 读取时恒为空串，通过 HasPassword 告知前端是否已保存过密码；
// 更新时 Password 为空表示保留原密码。
type smtpSettingsDTO struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	HasPassword bool   `json:"has_password"`
	From        string `json:"from"`
	FromName    string `json:"from_name"`
	Encryption  string `json:"encryption"`
}

type smtpTestRequest struct {
	smtpSettingsDTO
	To string `json:"to"`
}

func toSMTPDTO(s *models.SMTPSettings) smtpSettingsDTO {
	return smtpSettingsDTO{
		Host:        s.Host,
		Port:        s.Port,
		Username:    s.Username,
		HasPassword: s.Password != "",
		From:        s.FromAddress,
		FromName:    s.FromName,
		Encryption:  s.Encryption,
	}
}

// requireSMTPAdmin 校验当前登录用户为管理员（RBAC 中间件对所有 token 全放行，需在此显式区分角色）
func requireSMTPAdmin(c *gin.Context) bool {
	user := common.GetUserInfo(c)
	if user == nil || !user.Valid() {
		c.JSON(http.StatusOK, common.Err(defs.ErrUserInfoNotValid))
		return false
	}
	if !user.IsAdmin() {
		c.JSON(http.StatusOK, common.Err("permission denied"))
		return false
	}
	return true
}

// validateSMTPInput 校验 SMTP 配置必填项
func validateSMTPInput(req smtpSettingsDTO) string {
	if strings.TrimSpace(req.Host) == "" {
		return "smtp host is required"
	}
	if req.Port < 1 || req.Port > 65535 {
		return "smtp port must be between 1 and 65535"
	}
	if strings.TrimSpace(req.From) == "" {
		return "from address is required"
	}
	switch req.Encryption {
	case mailer.EncryptionNone, mailer.EncryptionStartTLS, mailer.EncryptionSSL:
	default:
		return "invalid encryption type"
	}
	// 密码长度限制：静态加密后密文长度 = 64 + 2×len，超过 varchar(255) 会被
	// MySQL 严格模式拒绝/非严格模式静默截断导致凭证损坏
	if req.Password != "" && len(req.Password) > maxSMTPPasswordLength {
		return "smtp password too long (max 95 bytes)"
	}
	return ""
}

// GetSMTPSettings 读取 SMTP 邮件通知设置（仅管理员）
func GetSMTPSettings(appInstance app.Application) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireSMTPAdmin(c) {
			return
		}
		ctx := app.NewContext(c, appInstance)
		s, err := dao.NewQuery(ctx).GetSMTPSettings()
		if err != nil {
			c.JSON(http.StatusOK, common.Err(err.Error()))
			return
		}
		c.JSON(http.StatusOK, common.OK(defs.ReqSuccess).WithBody(toSMTPDTO(s)))
	}
}

// UpdateSMTPSettings 保存 SMTP 邮件通知设置（仅管理员）
func UpdateSMTPSettings(appInstance app.Application) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireSMTPAdmin(c) {
			return
		}
		var req smtpSettingsDTO
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusOK, common.Err("invalid request: "+err.Error()))
			return
		}
		if msg := validateSMTPInput(req); msg != "" {
			c.JSON(http.StatusOK, common.Err(msg))
			return
		}

		ctx := app.NewContext(c, appInstance)
		old, err := dao.NewQuery(ctx).GetSMTPSettings()
		if err != nil {
			c.JSON(http.StatusOK, common.Err(err.Error()))
			return
		}

		// 先构造新的完整实例再保存，密码留空表示保留原值；
		// 历史全局开关字段原样保留（作为用户未配置通知开关时的默认值回退来源）
		s := &models.SMTPSettings{
			Host:                strings.TrimSpace(req.Host),
			Port:                req.Port,
			Username:            strings.TrimSpace(req.Username),
			Password:            old.Password,
			FromAddress:         strings.TrimSpace(req.From),
			Encryption:          req.Encryption,
			EnableServerOffline: old.EnableServerOffline,
			EnableClientOffline: old.EnableClientOffline,
			EnableProxyOffline:  old.EnableProxyOffline,
			EnableLoginNotify:   old.EnableLoginNotify,
			EnableServerOnline:  old.EnableServerOnline,
			EnableClientOnline:  old.EnableClientOnline,
			EnableProxyOnline:   old.EnableProxyOnline,
			OfflineDelayServer:  old.OfflineDelayServer,
			OfflineDelayClient:  old.OfflineDelayClient,
			OfflineDelayProxy:   old.OfflineDelayProxy,
			CreatedAt:           old.CreatedAt,
			FromName:            strings.TrimSpace(req.FromName),
		}
		if req.Password != "" {
			s.Password = req.Password
		}

		smtpSaveMu.Lock()
		defer smtpSaveMu.Unlock()
		if err := dao.NewMutation(ctx).SaveSMTPSettings(s); err != nil {
			c.JSON(http.StatusOK, common.Err(err.Error()))
			return
		}
		c.JSON(http.StatusOK, common.OK(defs.ReqSuccess).WithBody(toSMTPDTO(s)))
	}
}

// SendTestEmail 使用当前表单（未保存）的配置发送测试邮件（仅管理员，同步返回错误）
func SendTestEmail(appInstance app.Application) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireSMTPAdmin(c) {
			return
		}
		var req smtpTestRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusOK, common.Err("invalid request: "+err.Error()))
			return
		}
		if msg := validateSMTPInput(req.smtpSettingsDTO); msg != "" {
			c.JSON(http.StatusOK, common.Err(msg))
			return
		}

		ctx := app.NewContext(c, appInstance)

		to := strings.TrimSpace(req.To)
		if to == "" {
			// 缺省发给当前登录用户
			to = common.GetUserInfo(c).GetEmail()
		}
		if to == "" {
			c.JSON(http.StatusOK, common.Err("recipient email is empty"))
			return
		}

		// 密码留空表示沿用已保存的密码
		cfg := &models.SMTPSettings{
			Host:        strings.TrimSpace(req.Host),
			Port:        req.Port,
			Username:    strings.TrimSpace(req.Username),
			FromAddress: strings.TrimSpace(req.From),
			FromName:    strings.TrimSpace(req.FromName),
			Encryption:  req.Encryption,
		}
		if req.Password != "" {
			cfg.Password = req.Password
		} else {
			saved, err := dao.NewQuery(ctx).GetSMTPSettings()
			if err != nil {
				c.JSON(http.StatusOK, common.Err(err.Error()))
				return
			}
			cfg.Password = saved.Password
		}

		// 通用连通性测试；通知模板的样例测试见 /notify/prefs/test（所有用户可用）
		if err := mailer.SendSync(context.Background(), cfg, []string{to}, mailer.TestContent()); err != nil {
			c.JSON(http.StatusOK, common.Err("send test email failed: "+err.Error()))
			return
		}
		c.JSON(http.StatusOK, common.OK("test email sent"))
	}
}
