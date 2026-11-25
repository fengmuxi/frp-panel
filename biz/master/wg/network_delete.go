package wg

import (
	"context"
	"errors"

	"github.com/VaalaCat/frp-panel/common"
	"github.com/VaalaCat/frp-panel/pb"
	"github.com/VaalaCat/frp-panel/services/app"
	"github.com/VaalaCat/frp-panel/services/dao"
	"github.com/VaalaCat/frp-panel/services/rpc"
)

func DeleteNetwork(ctx *app.Context, req *pb.DeleteNetworkRequest) (*pb.DeleteNetworkResponse, error) {
	userInfo := common.GetUserInfo(ctx)
	if !userInfo.Valid() {
		return nil, errors.New("invalid user")
	}
	id := uint(req.GetId())
	if id == 0 {
		return nil, errors.New("invalid id")
	}

	// 级联清理：先删网络下的全部 WireGuard 并尽力通知客户端停掉其 wg 接口。
	// 否则残留 WG（network_id 悬挂、Network 预加载为 nil）会在 30s 内被各节点的
	// PullWireGuards 触发 ListClientWireGuards/get_network_typology 的 nil 解引用，
	// 直接击穿 master 的 gRPC 服务（无 recovery 拦截器，panic 即进程崩溃）
	q := dao.NewQuery(ctx)
	wgs, err := q.GetWireGuardsByNetworkID(userInfo, id)
	if err != nil {
		return nil, err
	}
	m := dao.NewMutation(ctx)
	for _, wg := range wgs {
		if wg == nil || wg.WireGuardEntity == nil {
			continue
		}
		wgID := wg.GetID()
		if wgID == 0 {
			continue
		}
		if err := m.DeleteWireGuard(userInfo, wgID); err != nil {
			return nil, err
		}
		// 尽力通知客户端删除 wg 接口：客户端离线时跳过（重连后的 pull 不再返回该 WG）
		clientID := wg.ClientID
		name := wg.Name
		go func() {
			defer func() { _ = recover() }()
			_, _ = rpc.CallClient(app.NewContext(context.Background(), ctx.GetApp()), clientID, pb.Event_EVENT_DELETE_WIREGUARD, &pb.DeleteWireGuardRequest{
				ClientId:      &clientID,
				InterfaceName: &name,
			})
		}()
	}

	if err := dao.NewMutation(ctx).DeleteNetwork(userInfo, id); err != nil {
		return nil, err
	}
	return &pb.DeleteNetworkResponse{
		Status: &pb.Status{Code: pb.RespCode_RESP_CODE_SUCCESS, Message: "success"},
	}, nil
}
