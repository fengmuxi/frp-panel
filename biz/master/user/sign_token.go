package user

import (
	"time"

	"github.com/fengmuxi/frp-panel/common"
	"github.com/fengmuxi/frp-panel/conf"
	"github.com/fengmuxi/frp-panel/defs"
	"github.com/fengmuxi/frp-panel/pb"
	"github.com/fengmuxi/frp-panel/services/app"
	"github.com/fengmuxi/frp-panel/utils"
	"github.com/fengmuxi/frp-panel/utils/logger"
	"github.com/samber/lo"
)

func SignTokenHandler(ctx *app.Context, req *pb.SignTokenRequest) (*pb.SignTokenResponse, error) {
	var (
		userInfo = common.GetUserInfo(ctx)
		cfg      = ctx.GetApp().GetConfig()
	)

	// 安全审计 P2（决策点 E）：sign-token 曾允许客户端自签任意 permissions claim
	// 与无上限 expiresIn（配合滑动续签等价于永久 token）。现在：
	//   - permissions 由服务端按角色推导，忽略请求值；
	//   - 有效期上限 24h；
	//   - 附带 jti 以支持服务端吊销。
	expiresIn := req.GetExpiresIn()
	if expiresIn <= 0 || expiresIn > int64((24*time.Hour).Seconds()) {
		expiresIn = int64((24 * time.Hour).Seconds())
	}
	permissions := conf.AllPermission() // 资源级隔离由归属校验（dao.Assert*Ownership）承担

	token, err := utils.GetJwtTokenFromMap(conf.JWTSecret(cfg),
		time.Now().Unix(),
		expiresIn,
		map[string]interface{}{
			defs.UserIDKey:                   userInfo.GetUserID(),
			defs.TokenPayloadKey_Permissions: permissions,
			"jti":                            utils.GenerateUUID(),
		})
	if err != nil {
		logger.Logger(ctx).WithError(err).Errorf("get jwt token failed")
		return nil, err
	}

	logger.Logger(ctx).Infof("get jwt token success, user: [%s], expiresIn: %ds", userInfo.GetUserName(), expiresIn)

	return &pb.SignTokenResponse{
		Token: lo.ToPtr(token),
		Status: &pb.Status{
			Code:    pb.RespCode_RESP_CODE_SUCCESS,
			Message: "ok",
		},
	}, nil
}
