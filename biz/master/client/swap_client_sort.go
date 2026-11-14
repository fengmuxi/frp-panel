package client

import (
	"github.com/VaalaCat/frp-panel/common"
	"github.com/VaalaCat/frp-panel/models"
	"github.com/VaalaCat/frp-panel/pb"
	"github.com/VaalaCat/frp-panel/services/app"
	"github.com/VaalaCat/frp-panel/services/dao"
	"github.com/VaalaCat/frp-panel/utils/logger"
	"github.com/samber/lo"
)

// SwapClientSortHandler 将客户端与全局显示顺序中的上一个（up=true）或下一个（up=false）交换位置。
// 邻居定位基于全量有序列表，因此可以跨分页交换；交换后按新顺序全量重排 sort_index，
// 保证后续分页查询顺序一致。已在边界时幂等返回成功。
func SwapClientSortHandler(c *app.Context, req *pb.SwapClientSortRequest) (*pb.SwapClientSortResponse, error) {
	logger.Logger(c).Infof("swap client sort, req: [%+v]", req)

	userInfo := common.GetUserInfo(c)
	clientID := req.GetClientId()

	if !userInfo.Valid() {
		return &pb.SwapClientSortResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "invalid user"},
		}, nil
	}

	if len(clientID) == 0 {
		return &pb.SwapClientSortResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "invalid client id"},
		}, nil
	}

	all, err := dao.NewQuery(c).ListAllClientsOrdered(userInfo)
	if err != nil {
		logger.Logger(c).WithError(err).Errorf("cannot list ordered clients")
		return nil, err
	}

	idx := -1
	for i, cli := range all {
		if cli.ClientID == clientID {
			idx = i
			break
		}
	}
	if idx < 0 {
		// 不在可排序列表中（被过滤条件排除或不属于当前用户）
		return &pb.SwapClientSortResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "client not found in sortable list"},
		}, nil
	}

	target := idx + 1
	if req.GetUp() {
		target = idx - 1
	}
	if target < 0 || target >= len(all) {
		// 已在首/尾边界，无需交换（前端通常已禁用按钮，这里幂等成功）
		return &pb.SwapClientSortResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_SUCCESS, Message: "ok"},
		}, nil
	}

	all[idx], all[target] = all[target], all[idx]
	orderedIDs := lo.Map(all, func(cli *models.ClientEntity, _ int) string { return cli.ClientID })

	if err := dao.NewMutation(c).UpdateClientSortOrders(userInfo, orderedIDs); err != nil {
		logger.Logger(c).WithError(err).Errorf("cannot update client sort orders")
		return nil, err
	}

	logger.Logger(c).Infof("swap client sort success, client id: [%s], up: [%v]", clientID, req.GetUp())
	return &pb.SwapClientSortResponse{
		Status: &pb.Status{Code: pb.RespCode_RESP_CODE_SUCCESS, Message: "ok"},
	}, nil
}
