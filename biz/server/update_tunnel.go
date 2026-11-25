package server

import (
	"context"
	"reflect"

	"github.com/fengmuxi/frp-panel/pb"
	"github.com/fengmuxi/frp-panel/services/app"
	"github.com/fengmuxi/frp-panel/services/server"
	"github.com/fengmuxi/frp-panel/utils"
	"github.com/fengmuxi/frp-panel/utils/logger"
)

func UpdateFrpsHander(ctx *app.Context, req *pb.UpdateFRPSRequest) (*pb.UpdateFRPSResponse, error) {
	logger.Logger(ctx).Infof("update frps, req: [%+v]", req)

	content := req.GetConfig()

	s, err := utils.LoadServerConfig(content, true)
	if err != nil {
		logger.Logger(context.Background()).WithError(err).Errorf("cannot load config")
		return &pb.UpdateFRPSResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: err.Error()},
		}, err
	}

	InjectAuthPlugin(ctx, s)

	serverID := req.GetServerId()
	if cli := ctx.GetApp().GetServerController().Get(serverID); cli != nil {
		if reflect.DeepEqual(cli.GetCommonCfg(), s) {
			logger.Logger(ctx).Infof("server %s config not changed", serverID)
			return &pb.UpdateFRPSResponse{
				Status: &pb.Status{Code: pb.RespCode_RESP_CODE_SUCCESS, Message: "ok"},
			}, nil
		}
	}

	// 先构造新实例，成功后再替换旧实例，坏配置不影响当前运行的服务
	newHandler, err := server.NewServerHandler(s)
	if err != nil {
		logger.Logger(context.Background()).WithError(err).Errorf("cannot create server handler, server id: [%s]", serverID)
		return &pb.UpdateFRPSResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: err.Error()},
		}, err
	}

	if cli := ctx.GetApp().GetServerController().Get(serverID); cli != nil {
		cli.Stop()
		ctx.GetApp().GetServerController().Delete(serverID)
		logger.Logger(ctx).Infof("server %s config changed, will recreate it", serverID)
	}

	ctx.GetApp().GetServerController().Add(serverID, newHandler)
	ctx.GetApp().GetServerController().Run(serverID)

	return &pb.UpdateFRPSResponse{
		Status: &pb.Status{Code: pb.RespCode_RESP_CODE_SUCCESS, Message: "ok"},
	}, nil
}
