package middleware

import (
	"regexp"
	"sync"

	"github.com/fengmuxi/frp-panel/common"
	"github.com/fengmuxi/frp-panel/pb"
	"github.com/fengmuxi/frp-panel/services/app"
	"github.com/fengmuxi/frp-panel/utils/logger"
	"github.com/gin-gonic/gin"
)

func RBAC(appInstance app.Application) func(*gin.Context) {
	return func(c *gin.Context) {
		perms, err := common.GetTokenPermission(c)
		path := c.Request.URL.Path
		method := c.Request.Method

		if err != nil {
			// 不记录 token 与完整用户实体（安全审计 P2：JWT/凭证入日志 = 会话泄露）
			logger.Logger(c).WithError(err).Errorf("get token permission error")
			common.ErrResp(c, &pb.CommonResponse{Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: err.Error()}}, err.Error())
			c.Abort()
			return
		}

		if len(perms) == 0 {
			logger.Logger(c).WithError(err).Errorf("user has no permission")
			common.ErrResp(c, &pb.CommonResponse{Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "user has no permission"}}, "user has no permission")
			c.Abort()
			return
		}

		for _, perm := range perms {
			if ruleMatched(ruleMatchParam{
				RuleMethod:    perm.Method,
				RulePath:      perm.Path,
				RequestPath:   path,
				RequestMethod: method,
			}) {
				logger.Logger(c).Debugf("user has api permission, continue")
				c.Next()
				return
			}
		}

		// 不记录完整权限 claim（安全审计 P2：其内容由用户可签发的 token 决定）
		logger.Logger(c).Errorf("user has no permission, path: [%s]", path)
		common.ErrResp(c, &pb.CommonResponse{Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "user has no permission"}}, "user has no permission")
		c.Abort()
		return
	}
}

type ruleMatchParam struct {
	RuleMethod    string
	RulePath      string
	RequestPath   string
	RequestMethod string
}

// rulePathCache 缓存编译后的路径规则（安全审计 P2：
// 原实现对用户可控的 RulePath 每次 MustCompile —— 非法正则直接 panic，恶意正则可 ReDoS）
var rulePathCache sync.Map

func ruleMatched(param ruleMatchParam) bool {
	methodMatch := false
	if param.RuleMethod == param.RequestMethod || param.RuleMethod == "*" {
		methodMatch = true
	}

	if !methodMatch {
		return false
	}

	if param.RulePath == "*" {
		return true
	}

	re, ok := rulePathCache.Load(param.RulePath)
	if !ok {
		compiled, err := regexp.Compile(param.RulePath)
		if err != nil {
			// 非法正则降级为字面量匹配，绝不 panic
			compiled = regexp.MustCompile(regexp.QuoteMeta(param.RulePath))
		}
		rulePathCache.Store(param.RulePath, compiled)
		re = compiled
	}
	return re.(*regexp.Regexp).MatchString(param.RequestPath)
}
