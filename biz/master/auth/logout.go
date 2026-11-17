package auth

import (
	"net/http"
	"time"

	"github.com/VaalaCat/frp-panel/common"
	"github.com/VaalaCat/frp-panel/conf"
	"github.com/VaalaCat/frp-panel/middleware"
	"github.com/VaalaCat/frp-panel/pb"
	"github.com/VaalaCat/frp-panel/services/app"
	"github.com/VaalaCat/frp-panel/services/cache"
	"github.com/VaalaCat/frp-panel/utils"
	"github.com/VaalaCat/frp-panel/utils/logger"
	"github.com/gin-gonic/gin"
)

// revokeJWT 把 token 的 jti 写入 freecache 黑名单（TTL = 剩余有效期），
// 使登出后的 token 在服务端立即失效（安全审计 P2：此前登出仅清 cookie）。
func revokeJWT(appInstance app.Application, ctx *gin.Context, cfg conf.Config) {
	tokenStr, err := ctx.Cookie(cfg.App.CookieName)
	if err != nil || tokenStr == "" {
		return
	}
	t, err := utils.ParseToken(conf.JWTSecret(cfg), tokenStr)
	if err != nil {
		return
	}
	jti, _ := t["jti"].(string)
	if jti == "" {
		return // 旧版 token 无 jti，无法精确吊销
	}
	exp, _ := t["exp"].(float64)
	ttl := int(time.Until(time.Unix(int64(exp), 0)).Seconds())
	if ttl <= 0 {
		return
	}
	if err := cache.Get().Set([]byte(middleware.JWTDenyPrefix+jti), []byte("1"), ttl); err != nil {
		logger.Logger(ctx).WithError(err).Warnf("write jwt denylist failed")
		return
	}
	logger.Logger(ctx).Infof("jwt revoked on logout")
}

func RemoveJWTHandler(appInstance app.Application) func(c *gin.Context) {
	return func(ctx *gin.Context) {
		cfg := appInstance.GetConfig()
		revokeJWT(appInstance, ctx, cfg)
		ctx.SetSameSite(http.SameSiteLaxMode)
		ctx.SetCookie(cfg.App.CookieName,
			"", -1,
			cfg.App.CookiePath,
			cfg.App.CookieDomain,
			cfg.App.CookieSecure,
			cfg.App.CookieHTTPOnly)
		common.OKResp(ctx, &pb.CommonResponse{Status: &pb.Status{Code: pb.RespCode_RESP_CODE_SUCCESS,
			Message: "ok"}})
	}
}
