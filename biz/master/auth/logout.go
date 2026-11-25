package auth

import (
	"net/http"
	"strings"
	"time"

	"github.com/fengmuxi/frp-panel/common"
	"github.com/fengmuxi/frp-panel/conf"
	"github.com/fengmuxi/frp-panel/defs"
	"github.com/fengmuxi/frp-panel/middleware"
	"github.com/fengmuxi/frp-panel/pb"
	"github.com/fengmuxi/frp-panel/services/app"
	"github.com/fengmuxi/frp-panel/services/cache"
	"github.com/fengmuxi/frp-panel/utils"
	"github.com/fengmuxi/frp-panel/utils/logger"
	"github.com/gin-gonic/gin"
)

// revokeJWT 把 token 的 jti 写入 freecache 黑名单（TTL = 剩余有效期），
// 使登出后的 token 在服务端立即失效（安全审计 P2：此前登出仅清 cookie）。
// 吊销目标覆盖三个认证源：cookie、Authorization header、?token= query——
// sign_token 签发的 header token 泄露后用户可通过登出吊销。
func revokeJWT(appInstance app.Application, ctx *gin.Context, cfg conf.Config) {
	candidates := []string{}
	if tk, err := ctx.Cookie(cfg.App.CookieName); err == nil && tk != "" {
		candidates = append(candidates, tk)
	}
	if tk := ctx.GetHeader(defs.AuthorizationKey); tk != "" {
		candidates = append(candidates, strings.TrimPrefix(tk, "Bearer "))
	}
	if tk := ctx.Query(defs.TokenKey); tk != "" {
		candidates = append(candidates, tk)
	}
	for _, tokenStr := range candidates {
		t, err := utils.ParseToken(conf.JWTSecret(cfg), tokenStr)
		if err != nil {
			continue
		}
		jti, _ := t["jti"].(string)
		if jti == "" {
			continue // 旧版 token 无 jti，无法精确吊销
		}
		exp, _ := t["exp"].(float64)
		ttl := int(time.Until(time.Unix(int64(exp), 0)).Seconds())
		if ttl <= 0 {
			continue
		}
		if err := cache.Get().Set([]byte(middleware.JWTDenyPrefix+jti), []byte("1"), ttl); err != nil {
			logger.Logger(ctx).WithError(err).Warnf("write jwt denylist failed")
			continue
		}
		logger.Logger(ctx).Infof("jwt revoked on logout")
	}
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
