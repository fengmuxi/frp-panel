package client

import (
	"reflect"

	"github.com/fengmuxi/frp-panel/pb"
	"github.com/fengmuxi/frp-panel/services/app"
	"github.com/fengmuxi/frp-panel/services/client"
	"github.com/fengmuxi/frp-panel/utils"
	"github.com/fengmuxi/frp-panel/utils/logger"
)

func UpdateFrpcHander(ctx *app.Context, req *pb.UpdateFRPCRequest) (*pb.UpdateFRPCResponse, error) {
	logger.Logger(ctx).Infof("update frpc, req: [%+v]", req)
	content := req.GetConfig()
	c, p, v, err := utils.LoadClientConfig(content, false)
	if err != nil {
		logger.Logger(ctx).WithError(err).Errorf("cannot load config")
		return &pb.UpdateFRPCResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: err.Error()},
		}, err
	}

	cli := ctx.GetApp().GetClientController().Get(req.GetClientId(), req.GetServerId())
	if cli != nil {
		if reflect.DeepEqual(c, cli.GetCommonCfg()) {
			logger.Logger(ctx).Warnf("client common config not changed")
			cli.Update(p, v)
			logger.Logger(ctx).Infof("update client, id: [%s] success, running", req.GetClientId())
			return &pb.UpdateFRPCResponse{
				Status: &pb.Status{Code: pb.RespCode_RESP_CODE_SUCCESS, Message: "ok"},
			}, nil
		}
	}

	// 先构造新实例，成功后再替换旧实例，坏配置不会中断当前运行的客户端
	newHandler, err := client.NewClientHandler(c, p, v)
	if err != nil {
		logger.Logger(ctx).WithError(err).Errorf("cannot create client handler, id: [%s]", req.GetClientId())
		return &pb.UpdateFRPCResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: err.Error()},
		}, err
	}

	if cli != nil {
		cli.Stop()
		ctx.GetApp().GetClientController().Delete(req.GetClientId(), req.GetServerId())
		ctx.GetApp().GetClientController().Add(req.GetClientId(), req.GetServerId(), newHandler)
		ctx.GetApp().GetClientController().Run(req.GetClientId(), req.GetServerId())
		logger.Logger(ctx).Infof("update client, id: [%s] success, running", req.GetClientId())
	} else {
		ctx.GetApp().GetClientController().Add(req.GetClientId(), req.GetServerId(), newHandler)
		ctx.GetApp().GetClientController().Run(req.GetClientId(), req.GetServerId())
		logger.Logger(ctx).Infof("add new client, id: [%s], running", req.GetClientId())
	}

	return &pb.UpdateFRPCResponse{
		Status: &pb.Status{Code: pb.RespCode_RESP_CODE_SUCCESS, Message: "ok"},
	}, nil
}
