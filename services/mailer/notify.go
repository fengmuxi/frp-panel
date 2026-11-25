package mailer

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fengmuxi/frp-panel/defs"
	"github.com/fengmuxi/frp-panel/models"
	"github.com/fengmuxi/frp-panel/services/app"
	"github.com/fengmuxi/frp-panel/services/dao"
	"github.com/fengmuxi/frp-panel/utils/logger"
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

// ---- 节点掉线统一状态（断开事件与周期对账共用）----
//
// 背景：掉线检测全在内存，master 重启会清空 ClientsManager 与防抖状态——
// 重启前已掉线且未重连的节点永远不会再产生掉线通知（漏报窗口=重启周期）。
// 周期对账任务（ReconcileNodeOffline）与断开事件共用本状态机消除漏报与双重通知：
//   - 断开事件（CheckNodeOffline）：断开后满防抖时长仍未重连 → 发送并标记 notified；
//   - 周期对账（ReconcileNodeOffline）：以 ClientsManager 实际在线为准，
//     未连接满防抖时长且未通知 → 发送并标记；重连则清状态。
type nodeOfflineState struct {
	since    time.Time // 首次观察到离线的时刻（防抖起点）
	notified bool      // 本轮掉线是否已发送通知
}

var (
	nodeOfflineMu     sync.Mutex
	nodeOfflineStates = map[string]*nodeOfflineState{} // key: nodeOfflineKey(cliType, clientID)
)

func nodeOfflineKey(cliType, clientID string) string {
	return cliType + "\x00" + clientID
}

// markNodeOffline 确保节点处于"离线观察中"状态；首次观察以 now 为防抖起点。
func markNodeOffline(cliType, clientID string, now time.Time) *nodeOfflineState {
	key := nodeOfflineKey(cliType, clientID)
	nodeOfflineMu.Lock()
	defer nodeOfflineMu.Unlock()
	st, ok := nodeOfflineStates[key]
	if !ok {
		st = &nodeOfflineState{since: now}
		nodeOfflineStates[key] = st
	}
	return st
}

func nodeOfflineNotified(cliType, clientID string) bool {
	key := nodeOfflineKey(cliType, clientID)
	nodeOfflineMu.Lock()
	defer nodeOfflineMu.Unlock()
	st, ok := nodeOfflineStates[key]
	return ok && st.notified
}

func markNodeOfflineNotified(cliType, clientID string) {
	key := nodeOfflineKey(cliType, clientID)
	nodeOfflineMu.Lock()
	defer nodeOfflineMu.Unlock()
	if st, ok := nodeOfflineStates[key]; ok {
		st.notified = true
	}
}

// clearNodeOfflineState 节点重连时清除掉线状态（下一轮掉线重新计数）
func clearNodeOfflineState(cliType, clientID string) {
	key := nodeOfflineKey(cliType, clientID)
	nodeOfflineMu.Lock()
	defer nodeOfflineMu.Unlock()
	delete(nodeOfflineStates, key)
}

// notifyResTypeFor 由节点类型映射通知资源类型（服务端/客户端）
func notifyResTypeFor(cliType string) string {
	switch cliType {
	case defs.CliTypeServer:
		return models.NotifyResTypeServer
	case defs.CliTypeClient:
		return models.NotifyResTypeClient
	}
	return ""
}

// notifyOfflineEnabled 读取指定类型节点掉线提醒的生效开关（资源级 > 用户级）
func notifyOfflineEnabled(prefs *models.UserNotifySettings, cliType string) bool {
	if prefs == nil {
		return false
	}
	switch cliType {
	case defs.CliTypeServer:
		return prefs.EnableServerOffline
	case defs.CliTypeClient:
		return prefs.EnableClientOffline
	}
	return false
}

// notifyNodeDisplayName 解析节点展示名（备注优先，回退 ID/原值）
func notifyNodeDisplayName(ctx *app.Context, resType, resID, fallback string) string {
	switch resType {
	case models.NotifyResTypeServer:
		if srv, err := dao.NewQuery(ctx).AdminGetServerByServerID(resID); err == nil && srv != nil && strings.TrimSpace(srv.Comment) != "" {
			return srv.Comment
		}
	case models.NotifyResTypeClient:
		if cli, err := dao.NewQuery(ctx).AdminGetClientByClientID(resID); err == nil && cli != nil && strings.TrimSpace(cli.Comment) != "" {
			return cli.Comment
		}
	}
	if strings.TrimSpace(fallback) != "" {
		return fallback
	}
	return resID
}

// reconcileNodeOnce 对单个节点执行一轮掉线对账：
// 在线 → 清除离线状态；离线 → 未观察过则记账，满防抖时长且未通知则发送。
// 返回本轮是否发送了通知。
func reconcileNodeOnce(ctx *app.Context, cfg *models.SMTPSettings, cliType, clientID string, connected bool, now time.Time) bool {
	userID, displayName := 0, ""
	var prefs *models.UserNotifySettings
	resType := notifyResTypeFor(cliType)
	switch cliType {
	case defs.CliTypeServer:
		srv, err := dao.NewQuery(ctx).AdminGetServerByServerID(clientID)
		if err != nil || srv == nil {
			// 节点已从面板删除：清状态并跳过
			clearNodeOfflineState(cliType, clientID)
			return false
		}
		userID = srv.UserID
		displayName = srv.Comment
		prefs = resolvePrefs(ctx, resType, clientID, userID)
	case defs.CliTypeClient:
		cli, err := dao.NewQuery(ctx).AdminGetClientByClientID(clientID)
		if err != nil || cli == nil || cli.ClientEntity == nil {
			clearNodeOfflineState(cliType, clientID)
			return false
		}
		userID = cli.UserID
		displayName = cli.Comment
		prefs = resolvePrefs(ctx, resType, clientID, userID)
	default:
		return false
	}

	st := markNodeOffline(cliType, clientID, now)
	if connected {
		clearNodeOfflineState(cliType, clientID)
		return false
	}
	if !notifyOfflineEnabled(prefs, cliType) {
		// 通知关闭：保持观察但不发送（开关打开后从观察起点重新计数）
		return false
	}
	if now.Sub(st.since) < offlineDelay(prefs, cliType) || st.notified {
		return false
	}

	if strings.TrimSpace(displayName) == "" {
		displayName = clientID
	}
	displayName = notifyNodeDisplayName(ctx, resType, clientID, displayName)
	email := resolveEmail(ctx, userID)
	if email == "" {
		return false
	}

	kind := "服务端"
	if cliType == defs.CliTypeClient {
		kind = "客户端"
	}
	SendAsync(ctx, cfg, []string{email}, NodeOfflineContent(kind, displayName, clientID, now))
	markNodeOfflineNotified(cliType, clientID)
	return true
}

// ReconcileNodeOffline 节点掉线的周期对账（每分钟）：以 ClientsManager 实际在线为准
// 对全部未停止的服务端/客户端做一轮离线判定。
//
// 背景：掉线检测状态全在内存，master 重启会清空——重启前已掉线且未重连的节点
// 在旧实现中永远不会产生掉线通知（漏报窗口=重启周期）。本任务消除该漏报，
// 同时在 CheckNodeOffline goroutine 因任意原因丢失时兜底。
func ReconcileNodeOffline(appInstance app.Application) error {
	defer func() {
		if r := recover(); r != nil {
			logger.Logger(context.Background()).Errorf("reconcile node offline panic: %v", r)
		}
	}()

	ctx := app.NewContext(context.Background(), appInstance)
	cfg := loadSettings(ctx)
	if cfg == nil || cfg.Host == "" {
		return nil
	}
	now := time.Now()

	servers, err := dao.NewQuery(ctx).AdminGetAllServers()
	if err != nil {
		logger.Logger(ctx).WithError(err).Errorf("reconcile node offline: cannot list servers")
		return nil
	}
	for _, srv := range servers {
		if srv == nil || srv.ServerEntity == nil {
			continue
		}
		_, connected := appInstance.GetClientsManager().ConnectTime(srv.ServerID)
		reconcileNodeOnce(ctx, cfg, defs.CliTypeServer, srv.ServerID, connected, now)
	}

	clients, err := dao.NewQuery(ctx).AdminGetAllClients()
	if err != nil {
		logger.Logger(ctx).WithError(err).Errorf("reconcile node offline: cannot list clients")
		return nil
	}
	for _, cli := range clients {
		if cli == nil || cli.ClientEntity == nil || cli.Stopped {
			continue
		}
		_, connected := appInstance.GetClientsManager().ConnectTime(cli.ClientID)
		reconcileNodeOnce(ctx, cfg, defs.CliTypeClient, cli.ClientID, connected, now)
	}
	return nil
}

// CheckNodeOffline 节点 gRPC 连接断开后的掉线确认（阻塞，调用方需用 go 启动）。
// 防抖：断开后等待该类型的防抖时长（可在通知设置里按类型配置），若期间节点
// 重连（ConnectTime 更新）则不发通知。
// 与周期对账任务（ReconcileNodeOffline）共用同一掉线状态机，互相消除漏报与双重通知。
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

	// 断开事件写入共享掉线状态（对账任务据此继续计数）
	markNodeOffline(cliType, clientID, time.Now())

	// 用户配置的该类型防抖时长
	time.Sleep(offlineDelay(prefs, cliType))

	cur, ok := ctx.GetApp().GetClientsManager().ConnectTime(clientID)
	if !ok {
		// 连接记录已被清理（非本进程注册或期间被移除）——按掉线处理，
		// 未通知过则发送（与周期对账口径一致）
	} else if cur.After(connTime) {
		// 断开期间已重连，不算掉线
		clearNodeOfflineState(cliType, clientID)
		return
	}

	cfg := loadSettings(ctx)
	if cfg == nil || cfg.Host == "" {
		return
	}

	if strings.TrimSpace(name) == "" {
		name = clientID
	}

	if nodeOfflineNotified(cliType, clientID) {
		// 周期对账已发送过本轮掉线通知，不重复发送
		return
	}

	email := resolveEmail(ctx, userID)
	if email == "" {
		return
	}

	SendAsync(ctx, cfg, []string{email}, NodeOfflineContent(kind, name, clientID, time.Now()))
	markNodeOfflineNotified(cliType, clientID)
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

	// 重连即复位该节点的掉线状态：下一轮掉线重新计数（与断开事件/对账任务联动）
	clearNodeOfflineState(cliType, clientID)

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

	cfg := loadSettings(ctx)

	proxyMu.Lock()

	// frps 首次同步或无活跃隧道（推送盲区）时不判定，仅重置该 server 状态
	if firstSync || len(activeNames) == 0 {
		resetProxyStateLocked(srv.ServerID)
		proxyMu.Unlock()
		return
	}

	now := time.Now()
	disappearedSet := map[string]bool{}
	var toOnlineSend []ProxyOfflineItem
	// 待判定候选（首次消失已记账、尚未通知）：锁内只做收集，
	// 通知开关/防抖时长的解析（DB 查询）移到锁外，避免全局锁内做 DB I/O
	var due []*proxyPendingState

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
		due = append(due, st)
	}

	// 隧道重新出现：复位防抖状态；若此前已通知过掉线，补发一条上线提醒（与掉线配对）。
	// 上线补发必须以 activeNames 真实命中为准：disappearedSet 只含"配置存在且未停止"的
	// 隧道，用户手动停用隧道/停用客户端后该隧道不在 disappeared 里、但也不在
	// activeNames 里——那是人为关闭，不能补发"隧道已上线"的假通知
	prefix := srv.ServerID + "\x00"
	for key := range proxyPending {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		name := strings.SplitN(key, prefix, 2)[1]
		if !disappearedSet[name] {
			st := proxyPending[key]
			if st.notified && activeNames[name] {
				toOnlineSend = append(toOnlineSend, st.item)
			}
			delete(proxyPending, key)
		}
	}

	proxyMu.Unlock()

	if len(toOnlineSend) == 0 {
		return
	}

	if cfg == nil || cfg.Host == "" {
		return
	}

	// 逐候选在锁外判定：开关/防抖时长/收件人全部通过才标记 notified 并发送。
	// 标记后置就是为了"同步失败点（配置/收件人缺失）不吞通知"——
	// 这些失败点发生在标记之前，下一轮推送会重试；SMTP 投递失败仍为 fire-and-forget
	for _, st := range due {
		prefs := prefsOf(st.item)
		if now.Sub(st.firstSeen) < offlineDelay(prefs, notifyKindProxy) {
			continue
		}
		if !prefs.EnableProxyOffline {
			continue
		}
		email := resolveEmail(ctx, st.item.UserID)
		if email == "" {
			continue
		}

		proxyMu.Lock()
		st.notified = true
		proxyMu.Unlock()

		SendAsync(ctx, cfg, []string{email}, TunnelOfflineContent(st.item.ProxyName, st.item.ServerID, time.Now()))
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
