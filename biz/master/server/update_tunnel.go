package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/VaalaCat/frp-panel/common"
	"github.com/VaalaCat/frp-panel/conf"
	"github.com/VaalaCat/frp-panel/defs"
	"github.com/VaalaCat/frp-panel/pb"
	"github.com/VaalaCat/frp-panel/services/app"
	"github.com/VaalaCat/frp-panel/services/dao"
	"github.com/VaalaCat/frp-panel/services/rpc"
	"github.com/VaalaCat/frp-panel/utils"
	"github.com/VaalaCat/frp-panel/utils/logger"
	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/pkg/config/v1/validation"
	"github.com/samber/lo"
)

func UpdateFrpsHander(c *app.Context, req *pb.UpdateFRPSRequest) (*pb.UpdateFRPSResponse, error) {
	logger.Logger(c).Infof("update frps, req: [%+v]", req)
	var (
		serverID  = req.GetServerId()
		configStr = req.GetConfig()
		userInfo  = common.GetUserInfo(c)
	)

	if len(configStr) == 0 || len(serverID) == 0 {
		return nil, fmt.Errorf("request invalid")
	}

	q := dao.NewQuery(c)
	srv, err := q.GetServerByServerID(userInfo, serverID)
	if srv == nil || err != nil {
		logger.Logger(context.Background()).WithError(err).Errorf("cannot get server, id: [%s]", serverID)
		return nil, err
	}

	srvCfg, err := utils.LoadServerConfig(configStr, true)
	if srvCfg == nil || err != nil {
		logger.Logger(context.Background()).WithError(err).Errorf("cannot load server config")
		return nil, err
	}

	// 保存前校验，直接拒绝会导致 frps 启动失败的非法配置
	if _, err := validation.ValidateServerConfig(srvCfg); err != nil {
		logger.Logger(context.Background()).WithError(err).Errorf("invalid server config, id: [%s]", serverID)
		return &pb.UpdateFRPSResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: err.Error()},
		}, err
	}

	lo.Filter(srvCfg.HTTPPlugins, func(item v1.HTTPPluginOptions, _ int) bool {
		return item.Name != defs.FRP_Plugin_Multiuser
	})
	srvCfg.HTTPPlugins = append(srvCfg.HTTPPlugins, conf.FRPsAuthOption(c.GetApp().GetConfig()))

	if err := srv.SetConfigContent(srvCfg); err != nil {
		logger.Logger(context.Background()).WithError(err).Errorf("cannot set server config")
		return nil, err
	}

	// optional 字段：未传则保持原值（避免配置表单未携带名称时被清空）
	if req.Comment != nil {
		newComment := strings.TrimSpace(req.GetComment())
		// 名称不允许重复（排除自身；默认服务端归属 user_id=0，查重时单独纳入）
		dupCount, dupErr := q.CountServersByComment(userInfo, newComment, serverID)
		if dupErr != nil {
			logger.Logger(c).WithError(dupErr).Errorf("count servers by comment failed, comment: [%s]", newComment)
			return nil, dupErr
		}
		if dupCount > 0 {
			return &pb.UpdateFRPSResponse{
				Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "server name already exists"},
			}, nil
		}
		srv.Comment = newComment
	}
	if len(req.GetServerIp()) > 0 {
		srv.ServerIP = req.GetServerIp()
	}

	if len(req.GetFrpsUrls()) > 0 {
		srv.FrpsUrls = req.GetFrpsUrls()
	}

	if err := dao.NewMutation(c).UpdateServer(userInfo, srv); err != nil {
		logger.Logger(context.Background()).WithError(err).Errorf("cannot update server, id: [%s]", serverID)
		return nil, err
	}

	go func() {
		resp, err := rpc.CallClient(app.NewContext(context.Background(), c.GetApp()), req.GetServerId(), pb.Event_EVENT_UPDATE_FRPS, req)
		if err != nil {
			logger.Logger(context.Background()).WithError(err).Errorf("update event send to server error, server id: [%s]", req.GetServerId())
		}
		if resp == nil {
			logger.Logger(c).Errorf("cannot get response, server id: [%s]", req.GetServerId())
		}
	}()

	logger.Logger(c).Infof("update frps success, id: [%s]", serverID)
	return &pb.UpdateFRPSResponse{
		Status: &pb.Status{Code: pb.RespCode_RESP_CODE_SUCCESS, Message: "ok"},
	}, nil
}
