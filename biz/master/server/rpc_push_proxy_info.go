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
func trackProxyOffline(ctx *app.Context, srv *models.ServerEntity, infos []*pb.ProxyInfo) {
	firstSync := false
	activeNames := map[string]bool{}
	for _, info := range infos {
		if info == nil {
			continue
		}
		if info.GetFirstSync() {
			firstSync = true
		}
		if name := normalizeProxyName(ctx, srv, info.GetName()); name != "" {
			activeNames[name] = true
		}
	}

	userInfo := &models.UserEntity{UserID: srv.UserID, TenantID: srv.TenantID}
	query := dao.NewQuery(ctx)

	stats, err := query.GetProxyStatsByServerID(userInfo, srv.ServerID)
	if err != nil {
		logger.Logger(ctx).WithError(err).Warnf("track proxy offline: list proxy stats failed, server: [%s]", srv.ServerID)
		return
	}
	configs, err := query.AdminListProxyConfigsWithFilters(&models.ProxyConfigEntity{ServerID: srv.ServerID})
	if err != nil {
		logger.Logger(ctx).WithError(err).Warnf("track proxy offline: list proxy configs failed, server: [%s]", srv.ServerID)
		return
	}
	clients, err := query.GetAllClients(userInfo)
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
				UserID:    srv.UserID,
				ServerID:  srv.ServerID,
				ProxyName: cfg.Name,
			})
		}
	}

	mailer.TrackProxyOffline(ctx, srv, activeNames, disappeared, firstSync)
}

// normalizeProxyName 与 AdminUpdateProxyStats 保持一致：去掉 frps multiuser 插件添加的 "userName." 前缀
func normalizeProxyName(ctx *app.Context, srv *models.ServerEntity, rawName string) string {
	name := strings.TrimSpace(rawName)
	if name == "" {
		return ""
	}
	user, err := dao.NewQuery(ctx).GetUserByUserID(srv.UserID)
	if err != nil || user == nil {
		return name
	}
	return strings.TrimPrefix(name, user.UserName+".")
}
