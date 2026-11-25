package client

import (
	"context"

	"github.com/VaalaCat/frp-panel/common"
	"github.com/VaalaCat/frp-panel/pb"
	"github.com/VaalaCat/frp-panel/services/app"
	"github.com/VaalaCat/frp-panel/services/dao"
	"github.com/VaalaCat/frp-panel/services/rpc"
	"github.com/VaalaCat/frp-panel/utils/logger"
)

func DeleteClientHandler(ctx *app.Context, req *pb.DeleteClientRequest) (*pb.DeleteClientResponse, error) {
	logger.Logger(ctx).Infof("delete client, req: [%+v]", req)

	userInfo := common.GetUserInfo(ctx)
	clientID := req.GetClientId()

	if !userInfo.Valid() {
		return &pb.DeleteClientResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "invalid user"},
		}, nil
	}

	if len(clientID) == 0 {
		return &pb.DeleteClientResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "invalid client id"},
		}, nil
	}

	// 统一级联入口：clients 行（origin+child）、隧道 proxy_config 行与通知偏好、
	// 流量统计/快照/流水、WG 接口与 endpoints/links、worker 关联，
	// 以及节点侧清理事件（WG 删除/worker 进程删除/frpc 实例停止，尽力而为）
	if err := dao.CascadeDeleteClient(ctx, userInfo, clientID); err != nil {
		return nil, err
	}

	go func() {
		resp, err := rpc.CallClient(app.NewContext(context.Background(), ctx.GetApp()), req.GetClientId(), pb.Event_EVENT_REMOVE_FRPC, req)
		if err != nil {
			logger.Logger(context.Background()).WithError(err).Errorf("remove event send to client error, client id: [%s]", req.GetClientId())
		}

		if resp == nil {
			logger.Logger(context.Background()).Errorf("cannot get response, client id: [%s]", req.GetClientId())
		}
	}()

	return &pb.DeleteClientResponse{
		Status: &pb.Status{Code: pb.RespCode_RESP_CODE_SUCCESS, Message: "ok"},
	}, nil
}
