package server

import (
	"context"
	"crypto/subtle"
	"fmt"
	"time"

	"github.com/VaalaCat/frp-panel/middleware"
	"github.com/VaalaCat/frp-panel/pb"
	"github.com/VaalaCat/frp-panel/services/app"
	"github.com/VaalaCat/frp-panel/services/cache"
	"github.com/VaalaCat/frp-panel/services/dao"
	"github.com/VaalaCat/frp-panel/utils/logger"
)

// FRPAuth frpc 接入认证（frps multiuser 插件回源）。
// 安全口径：token 比较用常量时间；日志不打印 token 明文（此前 %+v 会把
// 用户 frp token 写入访问日志）。
// frpAuthLimiter 按用户名限流：/auth 插件回源可在线爆破用户 frp token
// （token 比较虽为常量时间，逐用户维度仍需限流防爆破）
var frpAuthLimiter = middleware.NewRateLimiter(15*time.Minute, 20, 15*time.Minute)

func FRPAuth(ctx *app.Context, req *pb.FRPAuthRequest) (*pb.FRPAuthResponse, error) {
	if !frpAuthLimiter.Allow("frpauth:" + req.User) {
		logger.Logger(ctx).Warnf("frp auth rate limited, user: [%s]", req.User)
		return &pb.FRPAuthResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "too many requests"},
			Ok:     false,
		}, fmt.Errorf("too many requests")
	}
	logger.Logger(ctx).Infof("frpc auth, user: [%s]", req.User)
	var (
		err error
	)

	userToken, err := cache.Get().Get([]byte(req.User))
	if err != nil {
		u, err := dao.NewQuery(ctx).GetUserByUserName(req.User)
		if err != nil || u == nil {
			logger.Logger(context.Background()).WithError(err).Errorf("invalid user: %s", req.User)
			return &pb.FRPAuthResponse{
				Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: err.Error()},
				Ok:     false,
			}, fmt.Errorf("invalid user: %s", req.User)
		}
		cache.Get().Set([]byte(u.GetUserName()), []byte(u.GetToken()), 0)
		userToken = []byte(u.GetToken())
	}

	// 常量时间比较（与 dao 层 ValidateClientSecret/ValidateServerSecret 口径一致）
	if subtle.ConstantTimeCompare(userToken, []byte(req.GetToken())) != 1 {
		logger.Logger(ctx).Error("invalid token")
		return &pb.FRPAuthResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "invalid token"},
			Ok:     false,
		}, fmt.Errorf("invalid token")
	}

	return &pb.FRPAuthResponse{
		Status: &pb.Status{Code: pb.RespCode_RESP_CODE_SUCCESS, Message: "ok"},
		Ok:     true,
	}, nil
}
