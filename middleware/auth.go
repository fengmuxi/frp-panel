package middleware

import (
	"github.com/fengmuxi/frp-panel/common"
	"github.com/fengmuxi/frp-panel/defs"
	"github.com/fengmuxi/frp-panel/models"
	"github.com/fengmuxi/frp-panel/services/app"
	"github.com/fengmuxi/frp-panel/services/dao"
	"github.com/fengmuxi/frp-panel/utils/logger"
	"github.com/gin-gonic/gin"
	"github.com/spf13/cast"
)

func AuthCtx(appInstance app.Application) func(*gin.Context) {
	return func(c *gin.Context) {
		var err error
		var u *models.UserEntity
		appCtx := app.NewContext(c, appInstance)

		defer func() {
			logger.Logger(c).Debugf("finish auth user middleware")
		}()

		userID, err := cast.ToIntE(c.Value(defs.UserIDKey))
		if err != nil {
			logger.Logger(c).WithError(err).Errorf("invalid user id: %v", c.Value(defs.UserIDKey))
			common.ErrUnAuthorized(c, "token invalid")
			c.Abort()
			return
		}

		u, err = dao.NewQuery(appCtx).GetUserByUserID(userID)
		if err != nil {
			logger.Logger(c).WithError(err).Errorf("get user by user id failed, userID: [%d]", userID)
			common.ErrUnAuthorized(c, "token invalid")
			c.Abort()
			return
		}

		// 不打印完整用户实体（安全审计 P2：实体含 bcrypt 口径与 frp Token）
		logger.Logger(c).Debugf("auth middleware authed user id: [%d]", userID)

		if u.Valid() {
			logger.Logger(c).Debugf("set auth user to context, login success")
			c.Set(defs.UserInfoKey, u)
			c.Next()
			return
		}

		// 原 else 分支中的 "uid == 1 admin assign token" 依赖一个从未赋值的变量，
		// 属于死代码；若被"修复"将绕过被封禁用户的校验（安全审计已移除）
		logger.Logger(c).Errorf("invalid authorization, auth ctx middleware login failed")
		common.ErrUnAuthorized(c, "token invalid")
		c.Abort()
	}
}

// AuthAdmin 原实现逻辑反向（admin 反而 401）且无任何调用点，已删除；
// 管理员门禁统一改用资源归属断言（services/dao.AssertClientOwnership 等）
// 与 handler 内的角色判断（如 requireSMTPAdmin）。见 docs/security-remediation-plan.md 阶段 3。
