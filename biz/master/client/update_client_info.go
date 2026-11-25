package client

import (
	"strings"

	"github.com/fengmuxi/frp-panel/common"
	"github.com/fengmuxi/frp-panel/pb"
	"github.com/fengmuxi/frp-panel/services/app"
	"github.com/fengmuxi/frp-panel/services/dao"
	"github.com/fengmuxi/frp-panel/utils/logger"
)

// UpdateClientInfoHandler 轻量更新客户端名称（备注）与排序序号。
// 与 /frpc/update 的完整配置更新链路不同，本接口只改元数据：
// 不校验/不触碰 frpc 配置、不要求 Server 已就绪，也不会向客户端下发任何事件。
func UpdateClientInfoHandler(c *app.Context, req *pb.UpdateClientInfoRequest) (*pb.UpdateClientInfoResponse, error) {
	logger.Logger(c).Infof("update client info, req: [%+v]", req)

	userInfo := common.GetUserInfo(c)
	clientID := req.GetClientId()

	if !userInfo.Valid() {
		return &pb.UpdateClientInfoResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "invalid user"},
		}, nil
	}

	if len(clientID) == 0 {
		return &pb.UpdateClientInfoResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "invalid client id"},
		}, nil
	}

	cli, err := dao.NewQuery(c).GetClientByClientID(userInfo, clientID)
	if err != nil {
		logger.Logger(c).WithError(err).Errorf("cannot get client, id: [%s]", clientID)
		return &pb.UpdateClientInfoResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "cannot get client"},
		}, nil
	}

	// optional 字段：未传则保持原值
	if req.Comment != nil {
		newComment := strings.TrimSpace(req.GetComment())
		// 名称不允许重复（排除自身）
		dupCount, err := dao.NewQuery(c).CountClientsByComment(userInfo, newComment, clientID)
		if err != nil {
			logger.Logger(c).WithError(err).Errorf("count clients by comment failed, comment: [%s]", newComment)
			return nil, err
		}
		if dupCount > 0 {
			return &pb.UpdateClientInfoResponse{
				Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "client name already exists"},
			}, nil
		}
		cli.Comment = newComment
	}
	if req.SortIndex != nil {
		cli.SortIndex = int(req.GetSortIndex())
	}

	if err := dao.NewMutation(c).UpdateClient(userInfo, cli.ClientEntity); err != nil {
		logger.Logger(c).WithError(err).Errorf("cannot update client info, id: [%s]", clientID)
		return nil, err
	}

	logger.Logger(c).Infof("update client info success, client id: [%s]", clientID)
	return &pb.UpdateClientInfoResponse{
		Status: &pb.Status{Code: pb.RespCode_RESP_CODE_SUCCESS, Message: "ok"},
	}, nil
}
