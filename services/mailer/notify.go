package mailer

import (
	"fmt"
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

// loadNotifyPrefs 读取指定用户自己的通知开关配置（读取失败按全关闭处理）。
// 通知开关按用户独立（user_notify_settings 表），发给谁就用谁的开关。
func loadNotifyPrefs(ctx *app.Context, userID int) *models.UserNotifySettings {
	prefs, err := dao.NewQuery(ctx).GetNotifyPrefs(userID)
	if err != nil {
		logger.Logger(ctx).WithError(err).Errorf("load notify prefs failed, user id: [%d]", userID)
		return &models.UserNotifySettings{UserID: userID}
	}
	return prefs
}

// resolvePrefs 按「资源级 > 用户级」的优先级解析出生效的通知开关：
//
//	mode=off      → 全零值（该资源所有提醒关闭，最高优先级）
//	mode=override → 用资源自身覆盖字段构造
//	inherit/无记录/异常 → 用户全局（loadNotifyPrefs，含旧全局回退）
//
// resType/resID 为空表示无资源维度（或资源标识异常的兜底），直接回退用户全局。
// 返回与 UserNotifySettings 同形状，下游 offlineDelay()/开关判断无需感知差异。
func resolvePrefs(ctx *app.Context, resType, resID string, userID int) *models.UserNotifySettings {
	if resType == "" || resID == "" {
		return loadNotifyPrefs(ctx, userID)
	}
	rp, err := dao.NewQuery(ctx).GetResourceNotifyPrefs(resType, resID)
	if err != nil || rp == nil {
		logger.Logger(ctx).WithError(err).Warnf("load resource notify prefs failed, %s: [%s]", resType, resID)
		return loadNotifyPrefs(ctx, userID)
	}
	switch rp.Mode {
	case models.NotifyModeOff:
		// 该资源任何提醒一律不发（开关全 false；防抖字段无需关心，走不到发送）
		return &models.UserNotifySettings{UserID: userID}
	case models.NotifyModeOverride:
		p := &models.UserNotifySettings{UserID: userID}
		switch resType {
		case models.NotifyResTypeServer:
			p.EnableServerOffline = rp.EnableOffline
			p.EnableServerOnline = rp.EnableOnline
			p.OfflineDelayServer = rp.OfflineDelay
		case models.NotifyResTypeClient:
			p.EnableClientOffline = rp.EnableOffline
			p.EnableClientOnline = rp.EnableOnline
			p.OfflineDelayClient = rp.OfflineDelay
		case models.NotifyResTypeProxy:
			p.EnableProxyOffline = rp.EnableOffline
			p.EnableProxyOnline = rp.EnableOnline
			p.OfflineDelayProxy = rp.OfflineDelay
			p.EnableQuotaExceeded = rp.EnableQuotaExceeded
			p.EnableScheduleToggle = rp.EnableScheduleToggle
		}
		return p
	default: // inherit
		return loadNotifyPrefs(ctx, userID)
	}
}

// proxyResID 隧道资源标识（proxy_config 行主键，与改名无关）。
// ID 异常为 0 时返回空串，使 resolvePrefs 回退用户全局兜底。
func proxyResID(cfg *models.ProxyConfig) string {
	if cfg == nil || cfg.ID == 0 {
		return ""
	}
	return fmt.Sprint(cfg.ID)
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

// 下线通知防抖的类型标识（取可配置延迟用）
const (
	notifyKindServer = "server"
	notifyKindClient = "client"
	notifyKindProxy  = "proxy"
)

// offlineDelay 返回指定用户某类型下线通知的防抖时长。
// 设置为 0（含未配置/读取失败）时回退到内置默认 defs.OfflineCheckDelay。
func offlineDelay(prefs *models.UserNotifySettings, kind string) time.Duration {
	if prefs == nil {
		return defs.OfflineCheckDelay
	}
	var delaySec int
	switch kind {
	case notifyKindServer:
		delaySec = prefs.OfflineDelayServer
	case notifyKindClient:
		delaySec = prefs.OfflineDelayClient
	case notifyKindProxy:
		delaySec = prefs.OfflineDelayProxy
	}
	if delaySec <= 0 {
		return defs.OfflineCheckDelay
	}
	return time.Duration(delaySec) * time.Second
}

// CheckNodeOffline 节点 gRPC 连接断开后的掉线确认（阻塞，调用方需用 go 启动）。
// 防抖：断开后等待该类型的防抖时长（可在通知设置里按类型配置），若期间节点
// 重连（ConnectTime 更新）则不发通知。
// 注意：连接断开不会从 ClientsManager 中 Remove，因此不能用 Get() 判在线，必须比较 ConnectTime。
func CheckNodeOffline(ctx *app.Context, cliType, clientID string, connTime time.Time) {
	defer func() {
		if r := recover(); r != nil {
			logger.Logger(ctx).Errorf("check node offline panic: %v", r)
		}
	}()

	// 先定位节点与所属用户，用该用户自己的通知开关判断：
	// 未开启则直接返回（无需进入防抖等待）
	var (
		enabled bool
		name    string
		userID  int
		kind    string
		prefs   *models.UserNotifySettings
	)

	switch cliType {
	case defs.CliTypeServer:
		kind = "服务端"
		srv, err := dao.NewQuery(ctx).AdminGetServerByServerID(clientID)
		if err != nil || srv == nil {
			logger.Logger(ctx).WithError(err).Infof("server not found, skip offline notify, id: [%s]", clientID)
			return
		}
		name, userID = srv.Comment, srv.UserID
		prefs = resolvePrefs(ctx, models.NotifyResTypeServer, clientID, userID)
		enabled = prefs.EnableServerOffline
	case defs.CliTypeClient:
		kind = "客户端"
		cli, err := dao.NewQuery(ctx).AdminGetClientByClientID(clientID)
		if err != nil || cli == nil {
			logger.Logger(ctx).WithError(err).Infof("client not found, skip offline notify, id: [%s]", clientID)
			return
		}
		name, userID = cli.Comment, cli.UserID
		prefs = resolvePrefs(ctx, models.NotifyResTypeClient, clientID, userID)
		enabled = prefs.EnableClientOffline
	default:
		return
	}

	if !enabled {
		return
	}

	// 用户配置的该类型防抖时长
	time.Sleep(offlineDelay(prefs, cliType))

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

	if strings.TrimSpace(name) == "" {
		name = clientID
	}

	email := resolveEmail(ctx, userID)
	if email == "" {
		return
	}

	SendAsync(ctx, cfg, []string{email}, NodeOfflineContent(kind, name, clientID, time.Now()))
}

// nodeOnlineNotified 节点上线提醒防抖：同节点 60s 内只通知一次
// （网络闪断时 gRPC 可能短时间内反复 Register，避免刷屏）。
var (
	nodeOnlineMu       sync.Mutex
	nodeOnlineLastSeen = map[string]time.Time{}
)

const nodeOnlineDebounce = 60 * time.Second

// NotifyNodeOnline 节点（服务端/客户端）注册成功后的上线提醒。
// 内部判断开关与收件人，调用方用 go 启动；防抖由节点注册语义保证低频，
// 此处仅做 60s 去重兜底。
func NotifyNodeOnline(ctx *app.Context, cliType, clientID string) {
	defer func() {
		if r := recover(); r != nil {
			logger.Logger(ctx).Errorf("notify node online panic: %v", r)
		}
	}()

	// 先定位节点与所属用户，用该用户自己的通知开关判断
	var (
		enabled bool
		name    string
		userID  int
		kind    string
	)
	switch cliType {
	case defs.CliTypeServer:
		kind = "服务端"
		srv, err := dao.NewQuery(ctx).AdminGetServerByServerID(clientID)
		if err != nil || srv == nil {
			logger.Logger(ctx).WithError(err).Infof("server not found, skip online notify, id: [%s]", clientID)
			return
		}
		name, userID = srv.Comment, srv.UserID
		enabled = resolvePrefs(ctx, models.NotifyResTypeServer, clientID, userID).EnableServerOnline
	case defs.CliTypeClient:
		kind = "客户端"
		cli, err := dao.NewQuery(ctx).AdminGetClientByClientID(clientID)
		if err != nil || cli == nil {
			logger.Logger(ctx).WithError(err).Infof("client not found, skip online notify, id: [%s]", clientID)
			return
		}
		name, userID = cli.Comment, cli.UserID
		enabled = resolvePrefs(ctx, models.NotifyResTypeClient, clientID, userID).EnableClientOnline
	default:
		return
	}

	if !enabled {
		return
	}
	if strings.TrimSpace(name) == "" {
		name = clientID
	}

	// 防抖：同节点 60s 内只发一次上线提醒
	key := cliType + "\x00" + clientID
	nodeOnlineMu.Lock()
	if last, ok := nodeOnlineLastSeen[key]; ok && time.Since(last) < nodeOnlineDebounce {
		nodeOnlineMu.Unlock()
		return
	}
	nodeOnlineLastSeen[key] = time.Now()
	nodeOnlineMu.Unlock()

	cfg := loadSettings(ctx)
	if cfg == nil || cfg.Host == "" {
		return
	}

	email := resolveEmail(ctx, userID)
	if email == "" {
		return
	}

	SendAsync(ctx, cfg, []string{email}, NodeOnlineContent(kind, name, clientID, time.Now()))
}

// NotifyLogin 用户登录成功后的通知（用登录用户自己的开关，内部判断邮箱，调用方用 go 启动）。
func NotifyLogin(ctx *app.Context, userID int, username, ip string) {
	defer func() {
		if r := recover(); r != nil {
			logger.Logger(ctx).Errorf("notify login panic: %v", r)
		}
	}()

	if !loadNotifyPrefs(ctx, userID).EnableLoginNotify {
		return
	}

	cfg := loadSettings(ctx)
	if cfg == nil || cfg.Host == "" {
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
	UserID        int
	ServerID      string
	ProxyName     string
	ProxyConfigID uint // proxy_config 行主键：通知覆盖配置的关联标识（与隧道改名无关）
}

// proxyPendingState 单条隧道的掉线防抖状态。
// 携带完整候选信息，隧道复位补发上线提醒时仍能拿到行主键与所属用户。
type proxyPendingState struct {
	firstSeen time.Time
	notified  bool
	item      ProxyOfflineItem
}

var (
	proxyMu      sync.Mutex
	proxyPending = map[string]*proxyPendingState{} // key: proxyKey(serverID, proxyName)
)

func proxyKey(serverID, proxyName string) string {
	return serverID + "\x00" + proxyName
}

// TrackProxyOffline 基于 frps 的 proxy 推送做隧道掉线防抖确认（每次收到推送都应调用）。
// activeNames: 本次推送中该 server 的活跃隧道名（已规范化）；
// disappeared: 本次计算出的"应在线但未出现"的隧道；
// firstSync: frps 进程首次同步标记，为 true 时重置状态不判定。
// 防抖规则：首次消失记 pending，持续消失满防抖时长（可在通知设置里配置）后发送一次；
// 重现则复位。状态维护不依赖 SMTP 配置（避免配置后瞬间触发一批旧掉线），仅发送受配置控制。
func TrackProxyOffline(ctx *app.Context, srv *models.ServerEntity, activeNames map[string]bool, disappeared []ProxyOfflineItem, firstSync bool) {
	if srv == nil {
		return
	}

	// 单轮内按资源维度缓存通知开关（每用户+隧道最多查一次），
	// 判定走「资源级 > 用户级」优先级链（resolvePrefs）
	prefsCache := map[string]*models.UserNotifySettings{}
	prefsOf := func(item ProxyOfflineItem) *models.UserNotifySettings {
		key := fmt.Sprintf("%d:%d", item.UserID, item.ProxyConfigID)
		if p, ok := prefsCache[key]; ok {
			return p
		}
		resID := ""
		if item.ProxyConfigID != 0 {
			resID = fmt.Sprint(item.ProxyConfigID)
		}
		p := resolvePrefs(ctx, models.NotifyResTypeProxy, resID, item.UserID)
		prefsCache[key] = p
		return p
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
	// 隧道重现且此前通知过掉线时的上线提醒队列
	var toOnlineSend []ProxyOfflineItem

	for _, item := range disappeared {
		if item.ServerID != srv.ServerID || item.ProxyName == "" {
			continue
		}
		disappearedSet[item.ProxyName] = true
		key := proxyKey(srv.ServerID, item.ProxyName)
		st, ok := proxyPending[key]
		if !ok {
			// 首次消失，仅记录时间与候选信息，等下一轮确认
			proxyPending[key] = &proxyPendingState{firstSeen: now, item: item}
			continue
		}
		if st.notified {
			continue
		}
		// 持续消失满该资源配置的防抖时长才发送
		if now.Sub(st.firstSeen) >= offlineDelay(prefsOf(st.item), notifyKindProxy) {
			st.notified = true
			toSend = append(toSend, st.item)
		}
	}

	// 隧道重新出现：复位防抖状态；若此前已通知过掉线，补发一条上线提醒（与掉线配对）
	prefix := srv.ServerID + "\x00"
	for key := range proxyPending {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		name := strings.SplitN(key, prefix, 2)[1]
		if !disappearedSet[name] {
			st := proxyPending[key]
			if st.notified {
				toOnlineSend = append(toOnlineSend, st.item)
			}
			delete(proxyPending, key)
		}
	}

	proxyMu.Unlock()

	if len(toSend) == 0 && len(toOnlineSend) == 0 {
		return
	}

	cfg := loadSettings(ctx)
	if cfg == nil || cfg.Host == "" {
		return
	}

	if len(toSend) > 0 {
		for _, item := range toSend {
			// 逐条按「资源级 > 用户级」开关判断
			if !prefsOf(item).EnableProxyOffline {
				continue
			}
			email := resolveEmail(ctx, item.UserID)
			if email == "" {
				continue
			}
			SendAsync(ctx, cfg, []string{email}, TunnelOfflineContent(item.ProxyName, item.ServerID, time.Now()))
		}
	}

	// 隧道上线提醒：重现且此前通知过掉线时补发，与掉线提醒配对
	if len(toOnlineSend) > 0 {
		for _, item := range toOnlineSend {
			if !prefsOf(item).EnableProxyOnline {
				continue
			}
			email := resolveEmail(ctx, item.UserID)
			if email == "" {
				continue
			}
			SendAsync(ctx, cfg, []string{email}, TunnelOnlineContent(item.ProxyName, item.ServerID, time.Now()))
		}
	}
}

// ---- 隧道自动规则通知（流量超额 / 定时开关），均由对账任务在状态切换成功后异步调用 ----

// sendTunnelNotify 隧道类通知的公共发送路径：开关判断 → SMTP 配置 → 收件人 → 异步发送
func sendTunnelNotify(ctx *app.Context, enabled bool, userID int, content MailContent) {
	if !enabled {
		return
	}
	smtp := loadSettings(ctx)
	if smtp == nil || smtp.Host == "" {
		return
	}
	email := resolveEmail(ctx, userID)
	if email == "" {
		return
	}
	SendAsync(ctx, smtp, []string{email}, content)
}

// NotifyTunnelQuotaExceeded 隧道流量达到限额被自动关闭时的提醒（按所属用户开关）。
// quotaUsed 为关闭时刻的已用流量（字节）。
func NotifyTunnelQuotaExceeded(ctx *app.Context, cfg *models.ProxyConfig, quotaUsed int64) {
	defer func() {
		if r := recover(); r != nil {
			logger.Logger(ctx).Errorf("notify tunnel quota exceeded panic: %v", r)
		}
	}()
	if cfg == nil {
		return
	}
	prefs := resolvePrefs(ctx, models.NotifyResTypeProxy, proxyResID(cfg), cfg.UserID)
	sendTunnelNotify(ctx, prefs.EnableQuotaExceeded, cfg.UserID,
		QuotaExceededContent(cfg.Name, cfg.ServerID, cfg.QuotaType, cfg.QuotaDirection, cfg.QuotaLimit, quotaUsed, time.Now()))
}

// NotifyTunnelScheduleStopped 隧道到达定时关闭窗口被自动停用时的提醒（按所属用户开关）。
func NotifyTunnelScheduleStopped(ctx *app.Context, cfg *models.ProxyConfig) {
	defer func() {
		if r := recover(); r != nil {
			logger.Logger(ctx).Errorf("notify tunnel schedule stopped panic: %v", r)
		}
	}()
	if cfg == nil {
		return
	}
	prefs := resolvePrefs(ctx, models.NotifyResTypeProxy, proxyResID(cfg), cfg.UserID)
	sendTunnelNotify(ctx, prefs.EnableScheduleToggle, cfg.UserID,
		ScheduleToggleContent(cfg.Name, cfg.ServerID, "关闭", time.Now()))
}

// NotifyTunnelScheduleStarted 隧道到达定时开启窗口被自动启用时的提醒（按所属用户开关）。
func NotifyTunnelScheduleStarted(ctx *app.Context, cfg *models.ProxyConfig) {
	defer func() {
		if r := recover(); r != nil {
			logger.Logger(ctx).Errorf("notify tunnel schedule started panic: %v", r)
		}
	}()
	if cfg == nil {
		return
	}
	prefs := resolvePrefs(ctx, models.NotifyResTypeProxy, proxyResID(cfg), cfg.UserID)
	sendTunnelNotify(ctx, prefs.EnableScheduleToggle, cfg.UserID,
		ScheduleToggleContent(cfg.Name, cfg.ServerID, "开启", time.Now()))
}

// resetProxyStateLocked 清除指定 server 的全部防抖状态（需在持锁状态下调用）
func resetProxyStateLocked(serverID string) {
	prefix := serverID + "\x00"
	for key := range proxyPending {
		if strings.HasPrefix(key, prefix) {
			delete(proxyPending, key)
		}
	}
}
