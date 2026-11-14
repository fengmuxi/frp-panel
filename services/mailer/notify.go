package mailer

import (
	"strings"
	"sync"
	"time"

	"github.com/VaalaCat/frp-panel/defs"
	"github.com/VaalaCat/frp-panel/models"
	"github.com/VaalaCat/frp-panel/services/app"
	"github.com/VaalaCat/frp-panel/services/dao"
	"github.com/VaalaCat/frp-panel/utils/logger"
)

// loadSettings 实时读取 SMTP 配置（事件低频，不缓存）；读取失败返回 nil。
func loadSettings(ctx *app.Context) *models.SMTPSettings {
	cfg, err := dao.NewQuery(ctx).GetSMTPSettings()
	if err != nil {
		logger.Logger(ctx).WithError(err).Errorf("load smtp settings failed")
		return nil
	}
	return cfg
}

// resolveEmail 解析收件人邮箱，用户不存在或邮箱为空返回空串（调用方跳过并记日志）。
func resolveEmail(ctx *app.Context, userID int) string {
	user, err := dao.NewQuery(ctx).GetUserByUserID(userID)
	if err != nil || user == nil {
		logger.Logger(ctx).WithError(err).Warnf("resolve email failed, user id: [%d]", userID)
		return ""
	}
	if strings.TrimSpace(user.Email) == "" {
		logger.Logger(ctx).Warnf("user has no email, skip notify, user id: [%d]", userID)
		return ""
	}
	return user.Email
}

// CheckNodeOffline 节点 gRPC 连接断开后的掉线确认（阻塞，调用方需用 go 启动）。
// 防抖：断开后等待 OfflineCheckDelay，若期间节点重连（ConnectTime 更新）则不发通知。
// 注意：连接断开不会从 ClientsManager 中 Remove，因此不能用 Get() 判在线，必须比较 ConnectTime。
func CheckNodeOffline(ctx *app.Context, cliType, clientID string, connTime time.Time) {
	defer func() {
		if r := recover(); r != nil {
			logger.Logger(ctx).Errorf("check node offline panic: %v", r)
		}
	}()

	time.Sleep(defs.OfflineCheckDelay)

	cur, ok := ctx.GetApp().GetClientsManager().ConnectTime(clientID)
	if !ok {
		// 连接记录已被清理（非本进程注册或期间被移除），不判定
		return
	}
	if cur.After(connTime) {
		// 断开期间已重连，不算掉线
		return
	}

	cfg := loadSettings(ctx)
	if cfg == nil || cfg.Host == "" {
		return
	}

	var (
		enabled bool
		name    string
		userID  int
		kind    string
	)

	switch cliType {
	case defs.CliTypeServer:
		enabled = cfg.EnableServerOffline
		kind = "服务端"
		srv, err := dao.NewQuery(ctx).AdminGetServerByServerID(clientID)
		if err != nil || srv == nil {
			logger.Logger(ctx).WithError(err).Infof("server not found, skip offline notify, id: [%s]", clientID)
			return
		}
		name, userID = srv.Comment, srv.UserID
	case defs.CliTypeClient:
		enabled = cfg.EnableClientOffline
		kind = "客户端"
		cli, err := dao.NewQuery(ctx).AdminGetClientByClientID(clientID)
		if err != nil || cli == nil {
			logger.Logger(ctx).WithError(err).Infof("client not found, skip offline notify, id: [%s]", clientID)
			return
		}
		name, userID = cli.Comment, cli.UserID
	default:
		return
	}

	if !enabled {
		return
	}
	if strings.TrimSpace(name) == "" {
		name = clientID
	}

	email := resolveEmail(ctx, userID)
	if email == "" {
		return
	}

	SendAsync(ctx, cfg, []string{email}, NodeOfflineContent(kind, name, clientID, time.Now()))
}

// NotifyLogin 用户登录成功后的通知（内部判断开关与邮箱，调用方用 go 启动）。
func NotifyLogin(ctx *app.Context, userID int, username, ip string) {
	defer func() {
		if r := recover(); r != nil {
			logger.Logger(ctx).Errorf("notify login panic: %v", r)
		}
	}()

	cfg := loadSettings(ctx)
	if cfg == nil || !cfg.EnableLoginNotify || cfg.Host == "" {
		return
	}

	email := resolveEmail(ctx, userID)
	if email == "" {
		return
	}

	SendAsync(ctx, cfg, []string{email}, LoginContent(username, ip, time.Now()))
}

// ProxyOfflineItem 隧道掉线候选（由 biz 层计算差集后传入）
type ProxyOfflineItem struct {
	UserID    int
	ServerID  string
	ProxyName string
}

var (
	proxyMu       sync.Mutex
	proxyPending  = map[string]time.Time{} // 首次消失时间
	proxyNotified = map[string]bool{}      // 本次事故是否已通知
)

func proxyKey(serverID, proxyName string) string {
	return serverID + "\x00" + proxyName
}

// TrackProxyOffline 基于 frps 的 proxy 推送做隧道掉线防抖确认（每次收到推送都应调用）。
// activeNames: 本次推送中该 server 的活跃隧道名（已规范化）；
// disappeared: 本次计算出的"应在线但未出现"的隧道；
// firstSync: frps 进程首次同步标记，为 true 时重置状态不判定。
// 防抖规则：首次消失记 pending，持续消失满 OfflineCheckDelay 后发送一次；重现则复位。
func TrackProxyOffline(ctx *app.Context, srv *models.ServerEntity, activeNames map[string]bool, disappeared []ProxyOfflineItem, firstSync bool) {
	if srv == nil {
		return
	}

	proxyMu.Lock()

	// frps 首次同步或无活跃隧道（推送盲区）时不判定，仅重置该 server 状态
	if firstSync || len(activeNames) == 0 {
		resetProxyStateLocked(srv.ServerID)
		proxyMu.Unlock()
		return
	}

	now := time.Now()
	disappearedSet := map[string]bool{}
	var toSend []ProxyOfflineItem

	for _, item := range disappeared {
		if item.ServerID != srv.ServerID || item.ProxyName == "" {
			continue
		}
		disappearedSet[item.ProxyName] = true
		key := proxyKey(srv.ServerID, item.ProxyName)
		if _, ok := proxyPending[key]; !ok {
			// 首次消失，仅记录时间，等下一轮确认
			proxyPending[key] = now
			continue
		}
		if proxyNotified[key] {
			continue
		}
		if now.Sub(proxyPending[key]) >= defs.OfflineCheckDelay {
			proxyNotified[key] = true
			toSend = append(toSend, item)
		}
	}

	// 隧道重新出现则复位（允许下一次事故再次通知）
	prefix := srv.ServerID + "\x00"
	for key := range proxyPending {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		name := strings.SplitN(key, prefix, 2)[1]
		if !disappearedSet[name] {
			delete(proxyPending, key)
			delete(proxyNotified, key)
		}
	}

	proxyMu.Unlock()

	if len(toSend) == 0 {
		return
	}

	cfg := loadSettings(ctx)
	if cfg == nil || !cfg.EnableProxyOffline || cfg.Host == "" {
		return
	}

	for _, item := range toSend {
		email := resolveEmail(ctx, item.UserID)
		if email == "" {
			continue
		}
		SendAsync(ctx, cfg, []string{email}, TunnelOfflineContent(item.ProxyName, item.ServerID, time.Now()))
	}
}

// resetProxyStateLocked 清除指定 server 的全部防抖状态（需在持锁状态下调用）
func resetProxyStateLocked(serverID string) {
	prefix := serverID + "\x00"
	for key := range proxyPending {
		if strings.HasPrefix(key, prefix) {
			delete(proxyPending, key)
			delete(proxyNotified, key)
		}
	}
}
