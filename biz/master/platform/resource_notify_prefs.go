package platform

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/VaalaCat/frp-panel/common"
	"github.com/VaalaCat/frp-panel/defs"
	"github.com/VaalaCat/frp-panel/models"
	"github.com/VaalaCat/frp-panel/services/app"
	"github.com/VaalaCat/frp-panel/services/dao"
	"github.com/gin-gonic/gin"
)

// resourceNotifyPrefsDTO 资源级通知覆盖配置（三段式：off/inherit/override）。
// 无记录时后端返回 Mode=inherit（跟随用户全局），这是默认语义。
type resourceNotifyPrefsDTO struct {
	ResourceType string `json:"type"`        // server | client | proxy
	ResourceID   string `json:"resource_id"` // server/client 业务主键；proxy 为 proxy_config 行主键
	Mode         string `json:"mode"`        // off | inherit | override

	// mode=override 时的独立配置（server/client 用前 3 项，proxy 全用）
	EnableOffline        bool `json:"enable_offline"`
	EnableOnline         bool `json:"enable_online"`
	OfflineDelay         int  `json:"offline_delay"` // 秒，0=默认 60s
	EnableQuotaExceeded  bool `json:"enable_quota_exceeded"`
	EnableScheduleToggle bool `json:"enable_schedule_toggle"`
}

func toResourceNotifyDTO(p *models.ResourceNotifyPrefs) resourceNotifyPrefsDTO {
	return resourceNotifyPrefsDTO{
		ResourceType:         p.ResourceType,
		ResourceID:           p.ResourceID,
		Mode:                 p.Mode,
		EnableOffline:        p.EnableOffline,
		EnableOnline:         p.EnableOnline,
		OfflineDelay:         p.OfflineDelay,
		EnableQuotaExceeded:  p.EnableQuotaExceeded,
		EnableScheduleToggle: p.EnableScheduleToggle,
	}
}

// validateResourceIdentity 校验资源类型与标识，返回解析后的 proxy 行主键
func validateResourceIdentity(resType, resID string) (proxyID uint, errMsg string) {
	switch resType {
	case models.NotifyResTypeServer, models.NotifyResTypeClient:
		if resID == "" {
			return 0, "resource_id is required"
		}
	case models.NotifyResTypeProxy:
		id, err := strconv.ParseUint(resID, 10, 64)
		if err != nil || id == 0 {
			return 0, "invalid proxy resource_id"
		}
		return uint(id), ""
	default:
		return 0, "invalid resource type"
	}
	return 0, ""
}

// assertResourceOwnership 按资源类型分派归属断言（get 与 update 都必须调用，
// 防越权读写他人资源；RBAC 中间件不区分角色，归属在此显式校验）
func assertResourceOwnership(ctx *app.Context, userInfo models.UserInfo, resType, resID string, proxyID uint) error {
	switch resType {
	case models.NotifyResTypeServer:
		return dao.AssertServerOwnership(ctx, userInfo, resID)
	case models.NotifyResTypeClient:
		return dao.AssertClientOwnership(ctx, userInfo, resID)
	case models.NotifyResTypeProxy:
		return dao.AssertProxyOwnership(ctx, userInfo, proxyID)
	default:
		return fmt.Errorf("invalid resource type: %s", resType)
	}
}

// resolveResourceOwnerID 解析资源属主用户 ID（归属断言通过后调用）。
// 记录的 UserID 始终写资源属主：admin 代普通用户设置时也归到属主名下。
func resolveResourceOwnerID(ctx *app.Context, resType, resID string, proxyID uint) (int, error) {
	query := dao.NewQuery(ctx)
	switch resType {
	case models.NotifyResTypeServer:
		srv, err := query.AdminGetServerByServerID(resID)
		if err != nil || srv == nil {
			return 0, fmt.Errorf("server not found: %s", resID)
		}
		return srv.UserID, nil
	case models.NotifyResTypeClient:
		cli, err := query.AdminGetClientByClientID(resID)
		if err != nil || cli == nil {
			return 0, fmt.Errorf("client not found: %s", resID)
		}
		return cli.UserID, nil
	case models.NotifyResTypeProxy:
		cfg, err := query.AdminGetProxyConfigByID(proxyID)
		if err != nil || cfg == nil {
			return 0, fmt.Errorf("proxy config not found: %s", resID)
		}
		return cfg.UserID, nil
	default:
		return 0, fmt.Errorf("invalid resource type: %s", resType)
	}
}

// GetResourceNotifyPrefsHandler 读取指定资源的通知覆盖配置（所有登录用户，限本人资源或 admin）
func GetResourceNotifyPrefsHandler(appInstance app.Application) gin.HandlerFunc {
	return func(c *gin.Context) {
		user := common.GetUserInfo(c)
		if user == nil || !user.Valid() {
			c.JSON(http.StatusOK, common.Err(defs.ErrUserInfoNotValid))
			return
		}
		var req resourceNotifyPrefsDTO
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusOK, common.Err("invalid request: "+err.Error()))
			return
		}
		proxyID, msg := validateResourceIdentity(req.ResourceType, req.ResourceID)
		if msg != "" {
			c.JSON(http.StatusOK, common.Err(msg))
			return
		}

		ctx := app.NewContext(c, appInstance)
		if err := assertResourceOwnership(ctx, user, req.ResourceType, req.ResourceID, proxyID); err != nil {
			c.JSON(http.StatusOK, common.Err("permission denied"))
			return
		}

		prefs, err := dao.NewQuery(ctx).GetResourceNotifyPrefs(req.ResourceType, req.ResourceID)
		if err != nil {
			c.JSON(http.StatusOK, common.Err(err.Error()))
			return
		}
		c.JSON(http.StatusOK, common.OK(defs.ReqSuccess).WithBody(toResourceNotifyDTO(prefs)))
	}
}

// UpdateResourceNotifyPrefsHandler 保存指定资源的通知覆盖配置（所有登录用户，限本人资源或 admin）
func UpdateResourceNotifyPrefsHandler(appInstance app.Application) gin.HandlerFunc {
	return func(c *gin.Context) {
		user := common.GetUserInfo(c)
		if user == nil || !user.Valid() {
			c.JSON(http.StatusOK, common.Err(defs.ErrUserInfoNotValid))
			return
		}
		var req resourceNotifyPrefsDTO
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusOK, common.Err("invalid request: "+err.Error()))
			return
		}
		proxyID, msg := validateResourceIdentity(req.ResourceType, req.ResourceID)
		if msg != "" {
			c.JSON(http.StatusOK, common.Err(msg))
			return
		}
		switch req.Mode {
		case models.NotifyModeOff, models.NotifyModeInherit, models.NotifyModeOverride:
		default:
			c.JSON(http.StatusOK, common.Err("invalid mode"))
			return
		}
		if req.OfflineDelay < 0 || req.OfflineDelay > maxOfflineDelaySeconds {
			c.JSON(http.StatusOK, common.Err("offline delay must be between 0 and 3600 seconds"))
			return
		}

		ctx := app.NewContext(c, appInstance)
		if err := assertResourceOwnership(ctx, user, req.ResourceType, req.ResourceID, proxyID); err != nil {
			c.JSON(http.StatusOK, common.Err("permission denied"))
			return
		}
		ownerID, err := resolveResourceOwnerID(ctx, req.ResourceType, req.ResourceID, proxyID)
		if err != nil {
			c.JSON(http.StatusOK, common.Err(err.Error()))
			return
		}

		// 先构造完整记录再 upsert，UserID 写资源属主
		prefs := &models.ResourceNotifyPrefs{
			ResourceType:         req.ResourceType,
			ResourceID:           req.ResourceID,
			UserID:               ownerID,
			Mode:                 req.Mode,
			EnableOffline:        req.EnableOffline,
			EnableOnline:         req.EnableOnline,
			OfflineDelay:         req.OfflineDelay,
			EnableQuotaExceeded:  req.EnableQuotaExceeded,
			EnableScheduleToggle: req.EnableScheduleToggle,
		}
		if err := dao.NewMutation(ctx).SaveResourceNotifyPrefs(prefs); err != nil {
			c.JSON(http.StatusOK, common.Err(err.Error()))
			return
		}
		c.JSON(http.StatusOK, common.OK(defs.ReqSuccess).WithBody(toResourceNotifyDTO(prefs)))
	}
}
