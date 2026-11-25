package client

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/VaalaCat/frp-panel/common"
	"github.com/VaalaCat/frp-panel/defs"
	"github.com/VaalaCat/frp-panel/models"
	"github.com/VaalaCat/frp-panel/pb"
	"github.com/VaalaCat/frp-panel/services/app"
	"github.com/VaalaCat/frp-panel/services/dao"
	"github.com/VaalaCat/frp-panel/services/rpc"
	"github.com/VaalaCat/frp-panel/utils"
	"github.com/VaalaCat/frp-panel/utils/logger"
	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/pkg/config/v1/validation"
	"github.com/samber/lo"
	"github.com/spf13/cast"
)

func UpdateFrpcHander(c *app.Context, req *pb.UpdateFRPCRequest) (*pb.UpdateFRPCResponse, error) {
	logger.Logger(c).Infof("update frpc, req: [%+v]", req)
	var (
		content     = req.GetConfig()
		serverID    = req.GetServerId()
		reqClientID = req.GetClientId() // may be shadow or child
		userInfo    = common.GetUserInfo(c)
	)
	q := dao.NewQuery(c)
	m := dao.NewMutation(c)

	cliCfg, err := utils.LoadClientConfigNormal(content, true)
	if err != nil {
		logger.Logger(c).WithError(err).Errorf("cannot load config")
		return &pb.UpdateFRPCResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: err.Error()},
		}, err
	}

	cliRecord, err := q.GetClientByClientID(userInfo, reqClientID)
	if err != nil {
		logger.Logger(c).WithError(err).Errorf("cannot get client, id: [%s]", reqClientID)
		return &pb.UpdateFRPCResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "cannot get client"},
		}, fmt.Errorf("cannot get client")
	}

	cli := cliRecord.ClientEntity

	if cli.IsShadow {
		cli, _, err = ChildClientForServer(c, serverID, cli)
		if err != nil {
			logger.Logger(c).WithError(err).Errorf("cannot get child client, id: [%s]", reqClientID)
			return &pb.UpdateFRPCResponse{
				Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "cannot get child client"},
			}, fmt.Errorf("cannot get child client")
		}
	}

	if cli.IsShadow && len(cli.ConfigContent) == 0 {
		logger.Logger(c).Warnf("client is shadowed, cannot update, id: [%s]", reqClientID)
		return nil, fmt.Errorf("client is shadowed, cannot update")
	}

	if !cli.IsShadow && len(cli.OriginClientID) == 0 {
		logger.Logger(c).Warnf("client is not shadowed, make it shadow, id: [%s]", reqClientID)
		cli, err = MakeClientShadowed(c, serverID, cli)
		if err != nil {
			logger.Logger(c).WithError(err).Errorf("cannot make client shadowed, id: [%s]", reqClientID)
			return &pb.UpdateFRPCResponse{
				Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "cannot make client shadowed"},
			}, fmt.Errorf("cannot make client shadowed")
		}
	}

	srv, err := q.GetServerByServerID(userInfo, req.GetServerId())
	if err != nil || srv == nil || len(srv.ServerIP) == 0 || len(srv.ConfigContent) == 0 {
		logger.Logger(c).WithError(err).Errorf("cannot get server, server is not prepared, id: [%s]", req.GetServerId())
		return &pb.UpdateFRPCResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "cannot get server"},
		}, fmt.Errorf("cannot get server")
	}

	if len(srv.ConfigContent) == 0 {
		logger.Logger(c).Errorf("cannot get server, server is not prepared, id: [%s]", req.GetServerId())
		return &pb.UpdateFRPCResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "server is not prepared, pls update server config first"},
		}, fmt.Errorf("server is not prepared, pls update server config first")
	}

	srvConf, err := srv.GetConfigContent()
	if srvConf == nil || err != nil {
		logger.Logger(c).WithError(err).Errorf("cannot get server, id: [%s]", serverID)
		return nil, err
	}

	cliCfg.ServerAddr = srv.ServerIP
	switch cliCfg.Transport.Protocol {
	case "kcp":
		cliCfg.ServerPort = srvConf.KCPBindPort
	case "quic":
		cliCfg.ServerPort = srvConf.QUICBindPort
	default:
		cliCfg.ServerPort = srvConf.BindPort
	}

	// 覆盖地址三态语义（proto3 optional 字段）：
	//   - 未提供（req.FrpsUrl == nil）：不改动，沿用库中已有覆盖（追加隧道/配置同步场景）
	//   - 显式非空：校验后覆盖 ServerAddr/ServerPort（协议选择器优先，见下）
	//   - 显式清空（字段存在但值为空串）：移除覆盖，恢复上方服务端默认 IP + 协议端口
	// 注意不能用 len(GetFrpsUrl())>0 判定——那会把"清空"当成"没带字段"退回旧值，
	// 表现为界面清空覆盖地址后保存无效。
	if req.FrpsUrl != nil || len(cli.FrpsUrl) > 0 {
		urlToParse := ""
		if req.FrpsUrl != nil {
			urlToParse = strings.TrimSpace(req.GetFrpsUrl())
			if len(urlToParse) == 0 {
				cli.FrpsUrl = "" // 显式清空，ServerAddr/ServerPort 保持服务端默认
			}
		} else {
			urlToParse = strings.TrimSpace(cli.FrpsUrl) // 未提供该字段：沿用旧覆盖
		}

		if len(urlToParse) > 0 {
			parsedFrpsUrl, err := ValidateFrpsUrl(urlToParse)
			if err != nil {
				logger.Logger(c).WithError(err).Errorf("invalid frps url, url: [%s]", urlToParse)
				return &pb.UpdateFRPCResponse{
					Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: err.Error()},
				}, err
			}

			cliCfg.ServerAddr = parsedFrpsUrl.Hostname()
			cliCfg.ServerPort = cast.ToInt(parsedFrpsUrl.Port())
			// 协议以配置（界面协议选择器）优先：只有配置为默认 tcp、
			// 而 URL 显式指定了其他协议时才用 scheme 覆盖，
			// 否则填了 tcp:// 覆盖地址后，界面选 quic/kcp 保存会被静默改回 tcp
			if cliCfg.Transport.Protocol == "tcp" && parsedFrpsUrl.Scheme != "tcp" {
				cliCfg.Transport.Protocol = parsedFrpsUrl.Scheme
			}
			cli.FrpsUrl = urlToParse
		}
	}

	cliCfg.User = userInfo.GetUserName()

	if cliCfg.Metadatas == nil {
		cliCfg.Metadatas = make(map[string]string)
	}

	cliCfg.Metadatas[defs.FRPAuthTokenKey] = userInfo.GetToken()
	cliCfg.Metadatas[defs.FRPClientIDKey] = reqClientID

	newCfg := struct {
		v1.ClientCommonConfig
		Proxies  []v1.ProxyConfigurer   `json:"proxies,omitempty"`
		Visitors []v1.VisitorConfigurer `json:"visitors,omitempty"`
	}{
		ClientCommonConfig: cliCfg.ClientCommonConfig,
		Proxies:            lo.Map(cliCfg.Proxies, func(item v1.TypedProxyConfig, _ int) v1.ProxyConfigurer { return item.ProxyConfigurer }),
		Visitors:           lo.Map(cliCfg.Visitors, func(item v1.TypedVisitorConfig, _ int) v1.VisitorConfigurer { return item.VisitorConfigurer }),
	}

	// 保存前校验，直接拒绝会导致 frpc 启动失败的非法配置。
	// 与 frpc 官方加载流程保持一致：先 Complete 补全默认值再 Validate。
	// frp 的校验要求 proxy 的 bandwidthLimitMode 必须为 client/server，
	// 而默认值 "client" 是由 Complete 填充的；前端提交的配置不含该字段，
	// 若跳过 Complete 直接校验，空字符串会被误判为非法
	// （报错形如 "proxy xxx: bandwidth limit mode should be client or server"）。
	// Complete("") 只补默认值，不改变 proxy 名称（namePrefix 为空时无前缀）。
	for _, proxyCfg := range newCfg.Proxies {
		proxyCfg.Complete("")
	}
	if _, err := validation.ValidateAllClientConfig(&newCfg.ClientCommonConfig, newCfg.Proxies, newCfg.Visitors); err != nil {
		logger.Logger(c).WithError(err).Errorf("invalid client config, id: [%s]", reqClientID)
		return &pb.UpdateFRPCResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: err.Error()},
		}, err
	}

	rawCliConf, err := json.Marshal(newCfg)
	if err != nil {
		logger.Logger(c).WithError(err).Error("cannot marshal config")
		return nil, err
	}

	cli.ConfigContent = rawCliConf
	cli.ServerID = serverID
	if req.Comment != nil {
		newComment := strings.TrimSpace(req.GetComment())
		// 名称不允许重复（排除自身）。此处 cli 可能是影子子副本，
		// 而影子本体与其 comment 相同且在查重过滤器内，必须一并排除，
		// 否则影子客户端在高级模式下保存会误报 "client name already exists"
		excludeIDs := []string{cli.ClientID, cli.OriginClientID}
		dupCount, dupErr := q.CountClientsByComment(userInfo, newComment, excludeIDs...)
		if dupErr != nil {
			logger.Logger(c).WithError(dupErr).Errorf("count clients by comment failed, comment: [%s]", newComment)
			return nil, dupErr
		}
		if dupCount > 0 {
			return &pb.UpdateFRPCResponse{
				Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "client name already exists"},
			}, nil
		}
		cli.Comment = newComment
	}

	if err := m.UpdateClient(userInfo, cli); err != nil {
		logger.Logger(c).WithError(err).Errorf("cannot update client, id: [%s]", cli.ClientID)
		return nil, err
	}

	if err := m.RebuildProxyConfigFromClient(userInfo, &models.Client{ClientEntity: cli}); err != nil {
		logger.Logger(c).WithError(err).Errorf("cannot rebuild proxy config from client, id: [%s]", cli.ClientID)
		return nil, err
	}

	cliReq := &pb.UpdateFRPCRequest{
		ClientId: lo.ToPtr(cli.ClientID),
		ServerId: lo.ToPtr(serverID),
		Config:   rawCliConf,
	}

	go func() {
		childCtx := c.Background()
		cliToUpdate, err := dao.NewQuery(childCtx).GetClientByFilter(userInfo, &models.ClientEntity{ClientID: cli.OriginClientID}, nil)
		if err != nil || cliToUpdate == nil {
			// 注意：此处 cliToUpdate 可能为 nil（origin 被并发删除/DB 抖动），
			// 不能解引用；goroutine 无 recover，panic 会击穿 master 进程
			logger.Logger(childCtx).WithError(err).Errorf("cannot get origin client, id: [%s]", cli.OriginClientID)
			return
		}

		if cliToUpdate.Stopped {
			logger.Logger(childCtx).Infof("client [%s] is stopped, do not send update event", cliToUpdate.OriginClientID)
			return
		}

		resp, err := rpc.CallClient(childCtx, cliToUpdate.ClientID, pb.Event_EVENT_UPDATE_FRPC, cliReq)
		if err != nil {
			logger.Logger(childCtx).WithError(err).Errorf("update event send to client error, server: [%s], client: [%+v], updated client: [%+v]", serverID, cliToUpdate, cli)
		}

		if resp == nil {
			logger.Logger(childCtx).Errorf("cannot get response, server: [%s], client: [%s]", serverID, cliToUpdate.OriginClientID)
		}
	}()

	logger.Logger(c).Infof("update frpc success, client id: [%s]", reqClientID)
	return &pb.UpdateFRPCResponse{
		Status: &pb.Status{Code: pb.RespCode_RESP_CODE_SUCCESS, Message: "ok"},
	}, nil
}
