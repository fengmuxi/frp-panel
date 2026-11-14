package server

import (
	"strings"

	"github.com/VaalaCat/frp-panel/common"
	"github.com/VaalaCat/frp-panel/pb"
	"github.com/VaalaCat/frp-panel/services/app"
	"github.com/VaalaCat/frp-panel/services/dao"
	"github.com/VaalaCat/frp-panel/utils/logger"
)

// UpdateServerInfoHandler 轻量更新服务端名称（备注）与排序序号。
// 与 /frps/update 的完整配置更新链路不同，本接口只改元数据，不触碰 frps 配置。
func UpdateServerInfoHandler(c *app.Context, req *pb.UpdateServerInfoRequest) (*pb.UpdateServerInfoResponse, error) {
	logger.Logger(c).Infof("update server info, req: [%+v]", req)

	userInfo := common.GetUserInfo(c)
	serverID := req.GetServerId()

	if !userInfo.Valid() {
		return &pb.UpdateServerInfoResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "invalid user"},
		}, nil
	}

	if len(serverID) == 0 {
		return &pb.UpdateServerInfoResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "invalid server id"},
		}, nil
	}

	srv, err := dao.NewQuery(c).GetServerByServerID(userInfo, serverID)
	if err != nil || srv == nil {
		logger.Logger(c).WithError(err).Errorf("cannot get server, id: [%s]", serverID)
		return &pb.UpdateServerInfoResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "cannot get server"},
		}, nil
	}

	// optional 字段：未传则保持原值
	if req.Comment != nil {
		newComment := strings.TrimSpace(req.GetComment())
		// 名称不允许重复（排除自身；默认服务端归属 user_id=0，查重时单独纳入）
		dupCount, err := dao.NewQuery(c).CountServersByComment(userInfo, newComment, serverID)
		if err != nil {
			logger.Logger(c).WithError(err).Errorf("count servers by comment failed, comment: [%s]", newComment)
			return nil, err
		}
		if dupCount > 0 {
			return &pb.UpdateServerInfoResponse{
				Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "server name already exists"},
			}, nil
		}
		srv.Comment = newComment
	}
	if req.SortIndex != nil {
		srv.SortIndex = int(req.GetSortIndex())
	}

	if err := dao.NewMutation(c).UpdateServer(userInfo, srv); err != nil {
		logger.Logger(c).WithError(err).Errorf("cannot update server info, id: [%s]", serverID)
		return nil, err
	}

	logger.Logger(c).Infof("update server info success, server id: [%s]", serverID)
	return &pb.UpdateServerInfoResponse{
		Status: &pb.Status{Code: pb.RespCode_RESP_CODE_SUCCESS, Message: "ok"},
	}, nil
}
