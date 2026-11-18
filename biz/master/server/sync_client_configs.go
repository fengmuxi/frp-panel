package server

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

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
	"github.com/sourcegraph/conc/pool"
)

// downstreamSyncConcurrency 下游配置同步的推送并发上限：
// 避免大量客户端同时断开旧连接、重连新地址形成重连风暴
const downstreamSyncConcurrency = 5

// downstreamSyncResult 汇总一次下游连接配置同步的结果。
type downstreamSyncResult struct {
	Total          int // 该服务端下的下游客户端总数
	Updated        int // 存储配置已重建的客户端数
	Pushed         int // 在线热更新成功的客户端数
	PushFailed     int // 在线热更新失败的客户端数（存储已是新值，重连后自动生效）
	SkippedFrpsURL int // 配置了 frps_url（域名间接层）而跳过的客户端数
	SkippedSame    int // 连接地址无实际变化而跳过的客户端数
	SkippedInvalid int // 配置解析/校验失败而跳过的客户端数
}

// connFieldsChanged 判断连接相关字段（对外 IP 与各协议接入端口）是否发生变化。
// 仅注释/名称等非连接字段变化时不触发下游同步。
func connFieldsChanged(oldServerIP string, oldCfg *v1.ServerConfig, newServerIP string, newCfg *v1.ServerConfig) bool {
	if oldServerIP != newServerIP {
		return true
	}
	if oldCfg == nil || newCfg == nil {
		// 无旧配置说明服务端此前未就绪、下游不可能有生效的连接配置
		return false
	}
	return oldCfg.BindPort != newCfg.BindPort ||
		oldCfg.KCPBindPort != newCfg.KCPBindPort ||
		oldCfg.QUICBindPort != newCfg.QUICBindPort
}

// checkTCPReachable 预检新控制地址的 TCP 可达性（frpc 控制连接的入口）。
// 防火墙/安全组未放行或 DNS 未生效时提前中止下发，避免下游批量连向死地址。
func checkTCPReachable(ip string, port int) error {
	// JoinHostPort 保证 IPv6 地址带方括号（[::1]:7000），"%s:%d" 拼法对 IPv6 无效
	addr := net.JoinHostPort(ip, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return err
	}
	return conn.Close()
}

// SyncDownstreamClientConfigs 服务端连接配置（IP/端口）变更后，重建该服务端下
// 所有下游客户端（shadow/child）的 frpc 连接字段并热更新。
//
// 风险控制（与手动编辑单条隧道走同一套语义，见 biz/master/client/update_tunnel.go）：
//   - 只改连接字段（ServerAddr/ServerPort），proxies/visitors/transport 原样保留
//     （采用顶层 json.RawMessage 重写，无损其余字段）；
//   - FrpsUrl 非空的客户端走域名间接层，与 IP 无关，跳过；
//   - 新旧连接地址一致的客户端跳过，避免无谓的断连重连；
//   - 每份新配置先过 frp 官方校验，非法则跳过该客户端；
//   - 已停止的客户端仅更新存储配置；在线的逐个热更新（并发限流），
//     推送失败的客户端存储已是新值，重连后自动生效。
func SyncDownstreamClientConfigs(ctx *app.Context, srv *models.ServerEntity, srvCfg *v1.ServerConfig) *downstreamSyncResult {
	res := &downstreamSyncResult{}

	userInfo := &models.UserEntity{UserID: srv.UserID, TenantID: srv.TenantID}
	clients, err := dao.NewQuery(ctx).AdminGetClientsByServerID(srv.ServerID)
	if err != nil {
		logger.Logger(ctx).WithError(err).Errorf("downstream sync: cannot list clients, server id: [%s]", srv.ServerID)
		return res
	}

	// 只处理 shadow/child 客户端（OriginClientID 非空）：实际部署到守护进程的
	// frpc 连接配置都存放在 child 上；origin 自身是可编辑模板，其内嵌的
	// serverAddr 会在用户下次编辑隧道时由 UpdateFrpcHander 重新解析，无需此处同步
	var children []*models.Client
	for _, client := range clients {
		if client.ClientEntity == nil || client.OriginClientID == "" {
			continue
		}
		children = append(children, client)
	}
	res.Total = len(children)

	// 推送并发限流：避免大量客户端同时断开旧连接、重连新地址形成重连风暴
	p := pool.New().WithMaxGoroutines(downstreamSyncConcurrency)
	var mu sync.Mutex
	for _, child := range children {
		child := child
		p.Go(func() {
			// frps_url 非空：客户端连接信息走域名间接层，与服务端 IP/端口变化无关
			if child.FrpsUrl != "" {
				mu.Lock()
				res.SkippedFrpsURL++
				mu.Unlock()
				return
			}

			cfg, err := utils.LoadClientConfigNormal(child.ConfigContent, false)
			if err != nil || cfg == nil {
				mu.Lock()
				res.SkippedInvalid++
				mu.Unlock()
				logger.Logger(ctx).Warnf("downstream sync: cannot parse client config, skip, client id: [%s]", child.ClientID)
				return
			}

			var newPort int
			switch cfg.Transport.Protocol {
			case "kcp":
				newPort = srvCfg.KCPBindPort
			case "quic":
				newPort = srvCfg.QUICBindPort
			default:
				newPort = srvCfg.BindPort
			}
			if cfg.ServerAddr == srv.ServerIP && cfg.ServerPort == newPort {
				// 连接地址无实际变化（可能已手动同步过），避免无谓断连
				mu.Lock()
				res.SkippedSame++
				mu.Unlock()
				return
			}

			// 顶层 RawMessage 重写连接字段：除 serverAddr/serverPort 外逐字节保留原配置
			var top map[string]json.RawMessage
			if err := json.Unmarshal(child.ConfigContent, &top); err != nil {
				mu.Lock()
				res.SkippedInvalid++
				mu.Unlock()
				logger.Logger(ctx).Warnf("downstream sync: cannot rewrite client config, skip, client id: [%s]", child.ClientID)
				return
			}
			serverAddrRaw, _ := json.Marshal(srv.ServerIP)
			serverPortRaw, _ := json.Marshal(newPort)
			top["serverAddr"] = serverAddrRaw
			top["serverPort"] = serverPortRaw
			newContent, err := json.Marshal(top)
			if err != nil {
				mu.Lock()
				res.SkippedInvalid++
				mu.Unlock()
				logger.Logger(ctx).Warnf("downstream sync: cannot marshal client config, skip, client id: [%s]", child.ClientID)
				return
			}

			// 与手动编辑隧道相同的校验链：先 Complete 补默认值再官方校验
			newCfg, err := utils.LoadClientConfigNormal(newContent, true)
			if err != nil || newCfg == nil {
				mu.Lock()
				res.SkippedInvalid++
				mu.Unlock()
				logger.Logger(ctx).Warnf("downstream sync: rebuilt client config invalid, skip, client id: [%s]", child.ClientID)
				return
			}
			proxies := lo.Map(newCfg.Proxies, func(item v1.TypedProxyConfig, _ int) v1.ProxyConfigurer { return item.ProxyConfigurer })
			visitors := lo.Map(newCfg.Visitors, func(item v1.TypedVisitorConfig, _ int) v1.VisitorConfigurer { return item.VisitorConfigurer })
			for _, proxyCfg := range proxies {
				proxyCfg.Complete("")
			}
			if _, err := validation.ValidateAllClientConfig(&newCfg.ClientCommonConfig, proxies, visitors); err != nil {
				mu.Lock()
				res.SkippedInvalid++
				mu.Unlock()
				logger.Logger(ctx).Warnf("downstream sync: rebuilt client config failed validation, skip, client id: [%s], err: %v", child.ClientID, err)
				return
			}

			oldAddr := fmt.Sprintf("%s:%d", cfg.ServerAddr, cfg.ServerPort)
			child.ConfigContent = newContent
			if err := dao.NewMutation(ctx).UpdateClient(userInfo, child.ClientEntity); err != nil {
				mu.Lock()
				res.PushFailed++
				mu.Unlock()
				logger.Logger(ctx).WithError(err).Errorf("downstream sync: cannot update client config, client id: [%s] (old addr: %s)", child.ClientID, oldAddr)
				return
			}

			mu.Lock()
			res.Updated++
			mu.Unlock()

			// 已停止的客户端仅更新存储配置，不推送
			if child.Stopped {
				return
			}

			// 热更新：与手动编辑隧道一致，推给归属的客户端守护进程（origin）
			pushReq := &pb.UpdateFRPCRequest{
				ClientId: lo.ToPtr(child.ClientID),
				ServerId: lo.ToPtr(srv.ServerID),
				Config:   newContent,
			}
			if _, err := rpc.CallClient(ctx, child.OriginClientID, pb.Event_EVENT_UPDATE_FRPC, pushReq); err != nil {
				mu.Lock()
				res.PushFailed++
				mu.Unlock()
				// 存储配置已更新，客户端重连拉取后自动生效
				logger.Logger(ctx).WithError(err).Warnf("downstream sync: push to daemon failed (client will pick up on reconnect), client id: [%s], daemon: [%s], old addr: %s",
					child.ClientID, child.OriginClientID, oldAddr)
				return
			}

			mu.Lock()
			res.Pushed++
			mu.Unlock()
			logger.Logger(ctx).Infof("downstream sync: client config updated, client id: [%s], daemon: [%s], %s -> %s:%d",
				child.ClientID, child.OriginClientID, oldAddr, srv.ServerIP, newPort)
		})
	}
	p.Wait()
	return res
}
