package auth

import (
	"context"

	"github.com/VaalaCat/frp-panel/conf"
	"github.com/VaalaCat/frp-panel/defs"
	"github.com/VaalaCat/frp-panel/middleware"
	"github.com/VaalaCat/frp-panel/models"
	"github.com/VaalaCat/frp-panel/pb"
	"github.com/VaalaCat/frp-panel/services/app"
	"github.com/VaalaCat/frp-panel/services/dao"
	"github.com/VaalaCat/frp-panel/services/mailer"
	"github.com/VaalaCat/frp-panel/utils/logger"
)

func LoginHandler(ctx *app.Context, req *pb.LoginRequest) (*pb.LoginResponse, error) {
	username := req.GetUsername()
	password := req.GetPassword()
	ok, user, err := dao.NewQuery(ctx).CheckUserPassword(username, password)
	if err != nil {
		// 统一失败响应（安全审计 P3 反枚举）：不把内部错误原文泄露给客户端
		logger.Logger(ctx).WithError(err).Warn("login failed, uniform response returned")
		return &pb.LoginResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "invalid username or password"},
		}, nil
	}

	if !ok {
		return &pb.LoginResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "invalid username or password"},
		}, nil
	}

	userCount, err := dao.NewQuery(ctx).AdminCountUsers()
	if err != nil {
		logger.Logger(ctx).WithError(err).Error("get user count failed")
	}

	if userCount == 1 && user.GetSafeUserInfo().Role != defs.UserRole_Admin {
		userEntity, ok := user.(models.User)
		if !ok {
			logger.Logger(ctx).Errorf("trans user entity failed, invalid user entity")
			return &pb.LoginResponse{
				Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "invalid user"},
			}, nil
		}

		userEntity.Role = defs.UserRole_Admin

		dao.NewMutation(ctx).AdminUpdateUser(&models.UserEntity{
			UserID: user.GetUserID(),
		}, userEntity.UserEntity)
	}

	tokenStr := conf.GetJWTWithAllPermission(ctx.GetApp().GetConfig(), user.GetUserID())

	ginCtx := ctx.GetGinCtx()
	middleware.PushTokenStr(ginCtx, ctx.GetApp(), tokenStr)

	// 登录成功后异步发送登录通知邮件（内部判断开关与邮箱，使用脱离 gin 上下文的 app context 避免竞态）
	go mailer.NotifyLogin(
		app.NewContext(context.Background(), ctx.GetApp()),
		user.GetUserID(),
		user.GetUserName(),
		ginCtx.ClientIP(),
	)

	return &pb.LoginResponse{
		Status: &pb.Status{Code: pb.RespCode_RESP_CODE_SUCCESS, Message: "ok"},
		Token:  &tokenStr,
	}, nil
}
