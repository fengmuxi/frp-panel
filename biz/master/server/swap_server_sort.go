package server

import (
	"github.com/fengmuxi/frp-panel/common"
	"github.com/fengmuxi/frp-panel/models"
	"github.com/fengmuxi/frp-panel/pb"
	"github.com/fengmuxi/frp-panel/services/app"
	"github.com/fengmuxi/frp-panel/services/dao"
	"github.com/fengmuxi/frp-panel/utils/logger"
	"github.com/samber/lo"
)

// SwapServerSortHandler 将服务端与全局显示顺序中的上一个（up=true）或下一个（up=false）交换位置。
// 邻居定位基于全量有序列表，因此可以跨分页交换；交换后按新顺序全量重排 sort_index，
// 保证后续分页查询顺序一致。已在边界时幂等返回成功。
func SwapServerSortHandler(c *app.Context, req *pb.SwapServerSortRequest) (*pb.SwapServerSortResponse, error) {
	logger.Logger(c).Infof("swap server sort, req: [%+v]", req)

	userInfo := common.GetUserInfo(c)
	serverID := req.GetServerId()

	if !userInfo.Valid() {
		return &pb.SwapServerSortResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "invalid user"},
		}, nil
	}

	if len(serverID) == 0 {
		return &pb.SwapServerSortResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "invalid server id"},
		}, nil
	}

	all, err := dao.NewQuery(c).ListAllServersOrdered(userInfo)
	if err != nil {
		logger.Logger(c).WithError(err).Errorf("cannot list ordered servers")
		return nil, err
	}

	idx := -1
	for i, srv := range all {
		if srv.ServerID == serverID {
			idx = i
			break
		}
	}
	if idx < 0 {
		// 不在可排序列表中（被过滤条件排除或不属于当前用户）
		return &pb.SwapServerSortResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "server not found in sortable list"},
		}, nil
	}

	target := idx + 1
	if req.GetUp() {
		target = idx - 1
	}
	if target < 0 || target >= len(all) {
		// 已在首/尾边界，无需交换（前端通常已禁用按钮，这里幂等成功）
		return &pb.SwapServerSortResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_SUCCESS, Message: "ok"},
		}, nil
	}

	all[idx], all[target] = all[target], all[idx]
	orderedIDs := lo.Map(all, func(srv *models.ServerEntity, _ int) string { return srv.ServerID })

	if err := dao.NewMutation(c).UpdateServerSortOrders(userInfo, orderedIDs); err != nil {
		logger.Logger(c).WithError(err).Errorf("cannot update server sort orders")
		return nil, err
	}

	logger.Logger(c).Infof("swap server sort success, server id: [%s], up: [%v]", serverID, req.GetUp())
	return &pb.SwapServerSortResponse{
		Status: &pb.Status{Code: pb.RespCode_RESP_CODE_SUCCESS, Message: "ok"},
	}, nil
}
