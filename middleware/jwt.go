package middleware

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/VaalaCat/frp-panel/common"
	"github.com/VaalaCat/frp-panel/conf"
	"github.com/VaalaCat/frp-panel/defs"
	"github.com/VaalaCat/frp-panel/services/app"
	"github.com/VaalaCat/frp-panel/services/cache"
	"github.com/VaalaCat/frp-panel/utils"
	"github.com/VaalaCat/frp-panel/utils/logger"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/spf13/cast"
)

// jwtDenyPrefix 是吊销黑名单的 key 前缀（freecache），biz/master/auth/logout.go 写入。
const JWTDenyPrefix = "jwt-deny:"

// jwtRevoked 检查 token 的 jti 是否已被服务端吊销（安全审计 P2：
// 此前登出仅清 cookie，token 在服务端仍然有效）。
// 旧版 token 没有 jti claim，无法精确吊销，跳过检查（过渡兼容）。
func jwtRevoked(appInstance app.Application, t jwt.MapClaims) bool {
	jti, _ := t["jti"].(string)
	if jti == "" {
		return false
	}
	if _, err := cache.Get().Get([]byte(JWTDenyPrefix + jti)); err == nil {
		return true
	}
	return false
}

// JWTMAuth check if authed and set uid to context
func JWTAuth(appInstance app.Application) func(c *gin.Context) {
	return func(c *gin.Context) {
		defer func() {
			logger.Logger(c).Debugf("finish jwt middleware")
		}()

		// authed 响应禁止缓存（安全审计 P2：token/租户数据不得落入共享缓存）
		c.Header("Cache-Control", "no-store")

		var tokenStr string

		if tokenStr = c.Copy().Query(defs.TokenKey); len(tokenStr) != 0 {
			if t, err := utils.ParseToken(conf.JWTSecret(appInstance.GetConfig()), tokenStr); err == nil {
				if jwtRevoked(appInstance, t) {
					common.ErrUnAuthorized(c, "token revoked")
					c.Abort()
					return
				}
				for k, v := range t {
					c.Set(k, v)
				}
				logger.Logger(c).Debugf("query auth success")
				if err = resignAndPatchCtxJWT(c, appInstance, cast.ToInt(t[defs.UserIDKey]), t, tokenStr); err != nil {
					logger.Logger(c).WithError(err).Errorf("resign jwt error")
					common.ErrUnAuthorized(c, "resign jwt error")
					c.Abort()
					return
				}
				c.Next()
				SetToken(c, appInstance, cast.ToInt(t[defs.UserIDKey]), t)
				return
			}
			logger.Logger(c).Debugf("query auth failed")
		}

		cookieToken, err := c.Cookie(appInstance.GetConfig().App.CookieName)
		if err == nil {
			if t, err := utils.ParseToken(conf.JWTSecret(appInstance.GetConfig()), cookieToken); err == nil {
				if jwtRevoked(appInstance, t) {
					common.ErrUnAuthorized(c, "token revoked")
					c.Abort()
					return
				}
				for k, v := range t {
					c.Set(k, v)
				}
				logger.Logger(c).Debugf("cookie auth success")
				if err = resignAndPatchCtxJWT(c, appInstance, cast.ToInt(t[defs.UserIDKey]), t, cookieToken); err != nil {
					logger.Logger(c).WithError(err).Errorf("resign jwt error")
					common.ErrUnAuthorized(c, "resign jwt error")
					c.Abort()
					return
				}
				c.Next()
				return
			} else {
				logger.Logger(c).WithError(err).Errorf("jwt middleware parse token error")
				common.ErrUnAuthorized(c, "invalid authorization")
				c.Abort()
				return
			}
		}

		tokenStr = c.Request.Header.Get(defs.AuthorizationKey)
		tokenStr = strings.TrimPrefix(tokenStr, "Bearer ")
		if tokenStr == "" || tokenStr == "null" {
			logger.Logger(c).WithError(errors.New("authorization is empty")).Infof("authorization is empty")
			common.ErrUnAuthorized(c, "invalid authorization")
			c.Abort()
			return
		}

		if t, err := utils.ParseToken(conf.JWTSecret(appInstance.GetConfig()), tokenStr); err == nil {
			if jwtRevoked(appInstance, t) {
				common.ErrUnAuthorized(c, "token revoked")
				c.Abort()
				return
			}
			for k, v := range t {
				c.Set(k, v)
			}
			logger.Logger(c).Debugf("header auth success")
			if err = resignAndPatchCtxJWT(c, appInstance, cast.ToInt(t[defs.UserIDKey]), t, tokenStr); err != nil {
				logger.Logger(c).WithError(err).Errorf("resign jwt error")
				common.ErrUnAuthorized(c, "resign jwt error")
				c.Abort()
				return
			}
			c.Next()
			return
		} else {
			// 不记录 token 本身（安全审计 P2：JWT 入日志 = 会话被日志持有者窃取）
			logger.Logger(c).WithError(err).Errorf("jwt middleware parse token error")
		}
	}
}

func resignAndPatchCtxJWT(c *gin.Context, appInstance app.Application, userID int, t jwt.MapClaims, tokenStr string) error {
	// 无 exp 的合法签名 token 原会让下面解引用 panic（安全审计 P3）
	tokenExpire, err := t.GetExpirationTime()
	if err != nil || tokenExpire == nil {
		return fmt.Errorf("token has no expiration claim")
	}
	now := time.Now().Add(time.Duration(appInstance.GetConfig().App.CookieAge/2) * time.Second)
	if now.Before(tokenExpire.Time) {
		logger.Logger(c).Debugf("jwt not going to expire, continue to use old one")
		c.Set(defs.TokenKey, tokenStr)
		return nil
	}

	// 安全审计 P2：续签沿用原 token 的剩余有效期（不滑动延长）；
	// jti 保持不变，保证已吊销的会话续签后仍然无效
	remaining := int64(time.Until(tokenExpire.Time).Seconds())
	if remaining < 60 {
		remaining = 60
	}
	newToken, err := conf.GetJWTWithPayloadAndExpiry(appInstance.GetConfig(), userID, t, remaining)
	if err != nil {
		logger.Logger(c).WithError(err).Errorf("resign jwt error")
		return err
	}
	c.Set(defs.TokenKey, newToken)
	PushTokenStr(c, appInstance, newToken)

	logger.Logger(c).Debugf("jwt going to expire, resigning token")
	return nil
}

func SetToken(c *gin.Context, appInstance app.Application, userID int, payload jwt.MapClaims) (string, error) {
	logger.Logger(c).Debugf("set token for userID:[%d]", userID)
	if payload == nil {
		payload = make(map[string]interface{})
	}
	payload[defs.UserIDKey] = userID
	token, err := conf.GetJWTWithPayload(appInstance.GetConfig(), userID, payload)
	if err != nil {
		return "", err
	}
	c.Set(defs.TokenKey, token)
	return token, nil
}

// PushTokenStr 推送token到客户端
func PushTokenStr(c *gin.Context, appInstance app.Application, tokenStr string) {
	logger.Logger(c).Debugf("push new token to client")
	// 显式 SameSite=Lax（安全审计：CSRF 纵深防御）
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(appInstance.GetConfig().App.CookieName,
		tokenStr,
		appInstance.GetConfig().App.CookieAge,
		appInstance.GetConfig().App.CookiePath,
		appInstance.GetConfig().App.CookieDomain,
		appInstance.GetConfig().App.CookieSecure,
		appInstance.GetConfig().App.CookieHTTPOnly)
	c.Header(defs.SetAuthorizationKey, tokenStr)
}
