package server

import (
	"strings"

	"github.com/VaalaCat/frp-panel/models"
	"github.com/VaalaCat/frp-panel/pb"
	"github.com/VaalaCat/frp-panel/services/app"
	"github.com/VaalaCat/frp-panel/services/dao"
	"github.com/VaalaCat/frp-panel/services/mailer"
	"github.com/VaalaCat/frp-panel/utils/logger"
)

func PushProxyInfo(ctx *app.Context, req *pb.PushProxyInfoReq) (*pb.PushProxyInfoResp, error) {
	var srv *models.ServerEntity
	var err error

	if srv, err = ValidateServerRequest(ctx, req.GetBase()); err != nil {
		return nil, err
	}

	// 统计更新前计算隧道掉线候选（基于上次快照，失败不影响统计主流程）
	trackProxyOffline(ctx, srv, req.GetProxyInfos())

	if err = dao.NewMutation(ctx).AdminUpdateProxyStats(srv, req.GetProxyInfos()); err != nil {
		return nil, err
	}
	return &pb.PushProxyInfoResp{
		Status: &pb.Status{Code: pb.RespCode_RESP_CODE_SUCCESS, Message: "ok"},
	}, nil
}

// trackProxyOffline 对比本次 frps 推送与数据库中的隧道快照，把消失的隧道交给 mailer 做防抖确认。
// 判定前提：
//   - firstSync 为 true（frps 首次同步）或本次无活跃隧道时不判定；
//   - 隧道需曾经活跃过（存在于 proxy_stats）、未被手动停止（proxy_config.Stopped=false）、
//     且所属客户端节点未整体停止（client.Stopped=false），避免主动操作误报。
//
// 全部按节点维度查询（不限用户）：统计行归属各隧道的 client 归属用户，
// 节点 owner 的用户过滤查询会漏掉其他用户连接本节点的隧道。
func trackProxyOffline(ctx *app.Context, srv *models.ServerEntity, infos []*pb.ProxyInfo) {
	firstSync := false
	prefixes := serverProxyNamePrefixes(ctx, srv)
	activeNames := map[string]bool{}
	for _, info := range infos {
		if info == nil {
			continue
		}
		if info.GetFirstSync() {
			firstSync = true
		}
		if name := normalizeProxyName(prefixes, info.GetName()); name != "" {
			activeNames[name] = true
		}
	}

	query := dao.NewQuery(ctx)

	stats, err := query.AdminGetProxyStatsByServerID(srv.ServerID)
	if err != nil {
		logger.Logger(ctx).WithError(err).Warnf("track proxy offline: list proxy stats failed, server: [%s]", srv.ServerID)
		return
	}
	configs, err := query.AdminListProxyConfigsWithFilters(&models.ProxyConfigEntity{ServerID: srv.ServerID})
	if err != nil {
		logger.Logger(ctx).WithError(err).Warnf("track proxy offline: list proxy configs failed, server: [%s]", srv.ServerID)
		return
	}
	clients, err := query.AdminGetClientsByServerID(srv.ServerID)
	if err != nil {
		logger.Logger(ctx).WithError(err).Warnf("track proxy offline: list clients failed, server: [%s]", srv.ServerID)
		return
	}

	statsNames := make(map[string]bool, len(stats))
	for _, st := range stats {
		statsNames[st.Name] = true
	}
	stoppedClients := make(map[string]bool)
	for _, cli := range clients {
		if cli.Stopped {
			stoppedClients[cli.ClientID] = true
		}
	}

	var disappeared []mailer.ProxyOfflineItem
	for _, cfg := range configs {
		if cfg.Stopped || !statsNames[cfg.Name] {
			continue
		}
		if stoppedClients[cfg.ClientID] {
			continue
		}
		if !activeNames[cfg.Name] {
			disappeared = append(disappeared, mailer.ProxyOfflineItem{
				// 收件人与资源级开关判定必须锚定隧道自己的属主（cfg.UserID）：
				// 同一节点可挂多个用户的隧道，用节点属主（srv.UserID）会把通知发给他人
				UserID:        cfg.UserID,
				ServerID:      srv.ServerID,
				ProxyName:     cfg.Name,
				ProxyConfigID: cfg.ID, // 通知覆盖配置按行主键关联（与改名无关）
			})
		}
	}

	mailer.TrackProxyOffline(ctx, srv, activeNames, disappeared, firstSync)
}

// serverProxyNamePrefixes 返回连接本节点的所有 client 归属用户的 "userName."
// 前缀集合（含节点 owner）。frps multiuser 插件给隧道名加该前缀，处理推送时
// 需按 client 所属用户去前缀才能对上配置里的裸隧道名。
func serverProxyNamePrefixes(ctx *app.Context, srv *models.ServerEntity) []string {
	clients, err := dao.NewQuery(ctx).AdminGetClientsByServerID(srv.ServerID)
	if err != nil {
		logger.Logger(ctx).WithError(err).Warnf("track proxy offline: list clients failed, server: [%s]", srv.ServerID)
		return nil
	}
	userIDs := map[int]struct{}{srv.UserID: {}}
	for _, c := range clients {
		userIDs[c.UserID] = struct{}{}
	}
	prefixes := make([]string, 0, len(userIDs))
	for id := range userIDs {
		user, err := dao.NewQuery(ctx).GetUserByUserID(id)
		if err != nil || user == nil {
			continue
		}
		if user.UserName != "" {
			prefixes = append(prefixes, user.UserName+".")
		}
	}
	return prefixes
}

// normalizeProxyName 与 AdminUpdateProxyStats 的去前缀规则保持一致：
// 去掉 frps multiuser 插件添加的 "userName." 前缀（尝试所有已知用户前缀）。
func normalizeProxyName(prefixes []string, rawName string) string {
	name := strings.TrimSpace(rawName)
	if name == "" {
		return ""
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return strings.TrimPrefix(name, prefix)
		}
	}
	return name
}
