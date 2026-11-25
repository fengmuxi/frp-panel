package platform

import (
	"context"
	"net/http"
	"strings"

	"github.com/fengmuxi/frp-panel/common"
	"github.com/fengmuxi/frp-panel/defs"
	"github.com/fengmuxi/frp-panel/models"
	"github.com/fengmuxi/frp-panel/services/app"
	"github.com/fengmuxi/frp-panel/services/dao"
	"github.com/fengmuxi/frp-panel/services/mailer"
	"github.com/gin-gonic/gin"
)

// notifyPrefsDTO 每个用户自己的邮件通知开关与防抖配置。
// 与 SMTP 服务器连接配置（仅管理员）不同，通知开关对所有登录用户开放，且只能操作自己。
type notifyPrefsDTO struct {
	EnableServerOffline bool `json:"enable_server_offline"`
	EnableClientOffline bool `json:"enable_client_offline"`
	EnableProxyOffline  bool `json:"enable_proxy_offline"`
	EnableLoginNotify   bool `json:"enable_login_notify"`
	EnableServerOnline  bool `json:"enable_server_online"`
	EnableClientOnline  bool `json:"enable_client_online"`
	EnableProxyOnline   bool `json:"enable_proxy_online"`
	// 自动规则触发的提醒
	EnableQuotaExceeded  bool `json:"enable_quota_exceeded"`
	EnableScheduleToggle bool `json:"enable_schedule_toggle"`
	// 下线通知防抖时长（秒），0=默认 60s
	OfflineDelayServer int `json:"offline_delay_server"`
	OfflineDelayClient int `json:"offline_delay_client"`
	OfflineDelayProxy  int `json:"offline_delay_proxy"`
}

type notifyTestRequest struct {
	// Template 为空（或 "test"）时发送通用连通性测试邮件；也可指定业务模板枚举
	// （mailer.MailTemplateServerOffline 等），用样例数据渲染对应通知内容
	Template string `json:"template"`
}

func toNotifyPrefsDTO(p *models.UserNotifySettings) notifyPrefsDTO {
	return notifyPrefsDTO{
		EnableServerOffline:  p.EnableServerOffline,
		EnableClientOffline:  p.EnableClientOffline,
		EnableProxyOffline:   p.EnableProxyOffline,
		EnableLoginNotify:    p.EnableLoginNotify,
		EnableServerOnline:   p.EnableServerOnline,
		EnableClientOnline:   p.EnableClientOnline,
		EnableProxyOnline:    p.EnableProxyOnline,
		EnableQuotaExceeded:  p.EnableQuotaExceeded,
		EnableScheduleToggle: p.EnableScheduleToggle,
		OfflineDelayServer:   p.OfflineDelayServer,
		OfflineDelayClient:   p.OfflineDelayClient,
		OfflineDelayProxy:    p.OfflineDelayProxy,
	}
}

// requireValidUser 校验当前登录用户有效（通知设置对所有用户开放，此处不校验管理员）
func requireValidUser(c *gin.Context) bool {
	user := common.GetUserInfo(c)
	if user == nil || !user.Valid() {
		c.JSON(http.StatusOK, common.Err(defs.ErrUserInfoNotValid))
		return false
	}
	return true
}

// GetNotifyPrefs 读取当前登录用户自己的通知开关配置
func GetNotifyPrefs(appInstance app.Application) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireValidUser(c) {
			return
		}
		ctx := app.NewContext(c, appInstance)
		prefs, err := dao.NewQuery(ctx).GetNotifyPrefs(common.GetUserInfo(c).GetUserID())
		if err != nil {
			c.JSON(http.StatusOK, common.Err(err.Error()))
			return
		}
		c.JSON(http.StatusOK, common.OK(defs.ReqSuccess).WithBody(toNotifyPrefsDTO(prefs)))
	}
}

// UpdateNotifyPrefs 保存当前登录用户自己的通知开关配置（只作用于自己）
func UpdateNotifyPrefs(appInstance app.Application) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireValidUser(c) {
			return
		}
		var req notifyPrefsDTO
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusOK, common.Err("invalid request: "+err.Error()))
			return
		}
		for _, d := range []int{req.OfflineDelayServer, req.OfflineDelayClient, req.OfflineDelayProxy} {
			if d < 0 || d > maxOfflineDelaySeconds {
				c.JSON(http.StatusOK, common.Err("offline delay must be between 0 and 3600 seconds"))
				return
			}
		}

		ctx := app.NewContext(c, appInstance)
		// 构造自己的完整配置再保存，不触碰他人记录
		prefs := &models.UserNotifySettings{
			UserID:               common.GetUserInfo(c).GetUserID(),
			EnableServerOffline:  req.EnableServerOffline,
			EnableClientOffline:  req.EnableClientOffline,
			EnableProxyOffline:   req.EnableProxyOffline,
			EnableLoginNotify:    req.EnableLoginNotify,
			EnableServerOnline:   req.EnableServerOnline,
			EnableClientOnline:   req.EnableClientOnline,
			EnableProxyOnline:    req.EnableProxyOnline,
			EnableQuotaExceeded:  req.EnableQuotaExceeded,
			EnableScheduleToggle: req.EnableScheduleToggle,
			OfflineDelayServer:   req.OfflineDelayServer,
			OfflineDelayClient:   req.OfflineDelayClient,
			OfflineDelayProxy:    req.OfflineDelayProxy,
		}
		if err := dao.NewMutation(ctx).SaveNotifyPrefs(prefs); err != nil {
			c.JSON(http.StatusOK, common.Err(err.Error()))
			return
		}
		c.JSON(http.StatusOK, common.OK(defs.ReqSuccess).WithBody(toNotifyPrefsDTO(prefs)))
	}
}

// SendNotifyTest 使用管理员已保存的 SMTP 配置给当前登录用户自己发送一封样例通知，
// 供任意用户验证自己的通知模板（收件人固定为本人，不指定他人邮箱）。
func SendNotifyTest(appInstance app.Application) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireValidUser(c) {
			return
		}
		var req notifyTestRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusOK, common.Err("invalid request: "+err.Error()))
			return
		}

		user := common.GetUserInfo(c)
		to := strings.TrimSpace(user.GetEmail())
		if to == "" {
			c.JSON(http.StatusOK, common.Err("your account has no email address"))
			return
		}

		ctx := app.NewContext(c, appInstance)
		cfg, err := dao.NewQuery(ctx).GetSMTPSettings()
		if err != nil {
			c.JSON(http.StatusOK, common.Err(err.Error()))
			return
		}
		if cfg.Host == "" {
			c.JSON(http.StatusOK, common.Err("smtp not configured, ask administrator to set it first"))
			return
		}

		var content mailer.MailContent
		if req.Template == "" || req.Template == mailer.MailTemplateTest {
			content = mailer.TestContent()
		} else {
			var ok bool
			if content, ok = mailer.BuildSampleContent(req.Template); !ok {
				c.JSON(http.StatusOK, common.Err("unknown template: "+req.Template))
				return
			}
		}

		if err := mailer.SendSync(context.Background(), cfg, []string{to}, content); err != nil {
			c.JSON(http.StatusOK, common.Err("send test email failed: "+err.Error()))
			return
		}
		c.JSON(http.StatusOK, common.OK("test email sent"))
	}
}
