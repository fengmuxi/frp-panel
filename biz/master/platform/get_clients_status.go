package platform

import (
	"context"
	"time"

	"github.com/fengmuxi/frp-panel/common"
	"github.com/fengmuxi/frp-panel/pb"
	"github.com/fengmuxi/frp-panel/services/app"
	"github.com/fengmuxi/frp-panel/services/dao"
	"github.com/fengmuxi/frp-panel/services/rpc"
	"github.com/fengmuxi/frp-panel/utils/logger"
	"github.com/samber/lo"
	"google.golang.org/protobuf/proto"
)

func GetClientsStatus(c *app.Context, req *pb.GetClientsStatusRequest) (*pb.GetClientsStatusResponse, error) {
	userInfo := common.GetUserInfo(c)
	if !userInfo.Valid() || req == nil || len(req.GetClientIds()) == 0 || req.GetClientType() == pb.ClientType_CLIENT_TYPE_UNSPECIFIED {
		return &pb.GetClientsStatusResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "request invalid"},
		}, nil
	}

	var (
		clientIDs = req.GetClientIds()
		resps     = map[string]*pb.ClientStatus{}
	)

	for _, clientID := range clientIDs {
		// 归属校验（安全审计 P0-1 关联面）：非本人节点与"不存在"返回同样的离线占位，
		// 不产生控制通道流量，也不给出存在性 oracle
		var ownershipErr error
		if req.GetClientType() == pb.ClientType_CLIENT_TYPE_FRPS {
			ownershipErr = dao.AssertServerOwnership(c, userInfo, clientID)
		} else {
			ownershipErr = dao.AssertClientOwnership(c, userInfo, clientID)
		}
		if ownershipErr != nil {
			resps[clientID] = &pb.ClientStatus{
				ClientType: req.GetClientType(),
				ClientId:   clientID,
				Status:     pb.ClientStatus_STATUS_OFFLINE,
				Ping:       -1,
			}
			continue
		}

		mgr := c.GetApp().GetClientsManager()
		conn := mgr.Get(clientID)
		if conn == nil {
			resps[clientID] = &pb.ClientStatus{
				ClientType: req.GetClientType(),
				ClientId:   clientID,
				Status:     pb.ClientStatus_STATUS_OFFLINE,
				Ping:       -1,
			}
			continue
		}
		startTime := time.Now()
		tresp, err := rpc.CallClient(c, clientID, pb.Event_EVENT_PING, &pb.CommonRequest{})
		endTime := time.Now()
		pingTime := endTime.Sub(startTime).Milliseconds()
		if err != nil || tresp == nil {
			logger.Logger(context.Background()).WithError(err).Errorf("get client status error, client id: [%s]", clientID)
			resps[clientID] = &pb.ClientStatus{
				ClientType: req.GetClientType(),
				ClientId:   clientID,
				Status:     pb.ClientStatus_STATUS_ERROR,
				Ping:       int32(pingTime),
			}
			continue
		}

		clientVersion := &pb.ClientVersion{}
		proto.Unmarshal(tresp.GetData(), clientVersion)
		connectTime, ok := mgr.ConnectTime(clientID)
		if !ok {
			connectTime = time.Time{}
		}

		resps[clientID] = &pb.ClientStatus{
			ClientType:  req.GetClientType(),
			ClientId:    clientID,
			Status:      pb.ClientStatus_STATUS_ONLINE,
			Ping:        int32(pingTime),
			Version:     clientVersion,
			Addr:        lo.ToPtr(mgr.ClientAddr(clientID)),
			ConnectTime: lo.ToPtr(connectTime.UnixMilli()),
		}
	}

	return &pb.GetClientsStatusResponse{
		Status:  &pb.Status{Code: pb.RespCode_RESP_CODE_SUCCESS, Message: "ok"},
		Clients: resps,
	}, nil
}
