package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/fengmuxi/frp-panel/common"
	"github.com/fengmuxi/frp-panel/conf"
	"github.com/fengmuxi/frp-panel/defs"
	"github.com/fengmuxi/frp-panel/pb"
	"github.com/fengmuxi/frp-panel/services/app"
	"github.com/fengmuxi/frp-panel/services/dao"
	"github.com/fengmuxi/frp-panel/services/rpc"
	"github.com/fengmuxi/frp-panel/utils"
	"github.com/fengmuxi/frp-panel/utils/logger"
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

	// 记录连接相关字段的旧值（变更门控：无变化则不做下游同步）
	oldServerIP := srv.ServerIP
	oldCfg, _ := srv.GetConfigContent()

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

	// 后台串行执行三步，任一步失败即中止下游同步（配置已保存，可重新保存触发）：
	//  1. 等待 frps 节点确认应用新配置（顺序保证：下游先等 frps 生效再切地址）
	//  2. 预检新控制地址可达（防火墙/DNS 未就绪时中止，避免下游连向死地址）
	//  3. 按需重建并热更新下游客户端连接配置（变更门控 + 逐客户端 diff + 限流）
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Logger(context.Background()).Errorf("server update downstream sync panic: %v", r)
			}
		}()

		ctx := app.NewContext(context.Background(), c.GetApp())

		resp, err := rpc.CallClient(ctx, req.GetServerId(), pb.Event_EVENT_UPDATE_FRPS, req)
		if err != nil {
			logger.Logger(ctx).WithError(err).Errorf("update event send to server error, server id: [%s]", req.GetServerId())
		}
		if resp == nil {
			logger.Logger(ctx).Errorf("cannot get response, server id: [%s], skip downstream client sync", req.GetServerId())
			return
		}
		if !req.GetSyncClients() {
			logger.Logger(ctx).Infof("sync_clients disabled, skip downstream client sync, server id: [%s]", req.GetServerId())
			return
		}
		if !connFieldsChanged(oldServerIP, oldCfg, srv.ServerIP, srvCfg) {
			logger.Logger(ctx).Infof("server connection fields unchanged, skip downstream client sync, server id: [%s]", req.GetServerId())
			return
		}
		if srv.ServerIP == "" {
			logger.Logger(ctx).Warnf("server ip is empty, skip downstream client sync, server id: [%s]", req.GetServerId())
			return
		}
		if err := checkTCPReachable(srv.ServerIP, srvCfg.BindPort); err != nil {
			logger.Logger(ctx).WithError(err).Errorf("new frps control address %s:%d unreachable, skip downstream client sync, server id: [%s] (fix network/firewall then re-save server config)",
				srv.ServerIP, srvCfg.BindPort, req.GetServerId())
			return
		}

		res := SyncDownstreamClientConfigs(ctx, srv, srvCfg)
		logger.Logger(ctx).Infof("downstream client sync done, server id: [%s], total: %d, updated: %d, pushed: %d, push_failed: %d, skipped(frps_url): %d, skipped(same): %d, skipped(invalid): %d",
			req.GetServerId(), res.Total, res.Updated, res.Pushed, res.PushFailed, res.SkippedFrpsURL, res.SkippedSame, res.SkippedInvalid)
	}()

	logger.Logger(c).Infof("update frps success, id: [%s]", serverID)
	return &pb.UpdateFRPSResponse{
		Status: &pb.Status{Code: pb.RespCode_RESP_CODE_SUCCESS, Message: "ok"},
	}, nil
}
