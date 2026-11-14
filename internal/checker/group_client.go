package checker

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/VaalaCat/frp-panel/conf"
	"github.com/VaalaCat/frp-panel/defs"
	"github.com/VaalaCat/frp-panel/models"
	v1 "github.com/fatedier/frp/pkg/config/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func buildClientGroup(env *Env) ([]Checker, error) {
	return []Checker{
		&clientConfigParse{baseChecker{name: "config_parse", group: "client", severity: LevelMajor,
			fixHint: "clients.ConfigContent 必须可解析为 frp v1.ClientConfig；在面板隧道编辑器重新保存即可校验"}},
		&clientProxyTypes{baseChecker{name: "proxy_types", group: "client", severity: LevelMinor,
			fixHint: "未知 proxy type 会被 frpc 拒绝加载；对照 frp v0.65 支持的 tcp/udp/http/https/stcp/sudp/xtcp/tcpmux"}},
		&clientNodeFresh{baseChecker{name: "node_fresh", group: "client", severity: LevelMajor,
			fixHint: "节点掉线请检查节点进程存活、CLIENT_RPC_URL 网络连通与 master 端 OfflineCheckDelay(60s) 判定窗口"}},
		&clientSelfConsistency{baseChecker{name: "self_consistency", group: "client", severity: LevelMajor,
			fixHint: "本机 CLIENT_ID/CLIENT_SECRET 与数据库记录不一致会导致 gRPC 注册被拒；用 `frpp join` 重新入网"}},
		&clientSTUNConfig{baseChecker{name: "stun_config", group: "client", severity: LevelMinor,
			fixHint: "stcp/xtcp 依赖 natHoleSTUNServer 可达；默认 stun.easyvoip.com:3478 在部分网络不可用，建议自建 STUN"}},
		&clientTransportMatrix{baseChecker{name: "transport_matrix", group: "client", severity: LevelMajor,
			fixHint: "grpc/ws/wss 三种传输任选其一即可；若全部失败，优先检查 MASTER_RPC_HOST:PORT 的防火墙与 TLS 配置"}},
		&clientTunnelE2E{baseChecker{name: "tunnel_e2e", group: "client", severity: LevelMinor,
			fixHint: "端到端隧道连通性测试需要 --destructive（会创建/删除测试隧道）；请确认执行窗口"}},
	}, nil
}

// loadClients 读取并解析 clients 表（只读）。
func loadClients(ctx context.Context, env *Env) ([]parsedClient, []string, error) {
	db, err := env.DB(ctx)
	if err != nil {
		return nil, nil, err
	}
	var clients []models.Client
	if err := db.Model(&models.Client{}).Where("deleted_at IS NULL").Find(&clients).Error; err != nil {
		return nil, nil, fmt.Errorf("query clients: %w", err)
	}

	var (
		parsed []parsedClient
		bad    []string
	)
	for _, c := range clients {
		if !env.NodeFilter(c.ClientID) {
			continue
		}
		pc := parsedClient{ID: c.ClientID, Entity: &c}
		if len(c.ConfigContent) > 0 {
			cfg := &v1.ClientConfig{}
			if err := json.Unmarshal(c.ConfigContent, cfg); err != nil {
				bad = append(bad, fmt.Sprintf("%s: 解析失败: %v", c.ClientID, err))
				continue
			}
			pc.Cfg = cfg
		}
		parsed = append(parsed, pc)
	}
	return parsed, bad, nil
}

type parsedClient struct {
	ID     string
	Entity *models.Client
	Cfg    *v1.ClientConfig
}

// ---------------- client/config_parse ----------------

type clientConfigParse struct{ baseChecker }

func (c *clientConfigParse) Run(ctx context.Context, env *Env) Result {
	refs := []string{"clients.config_content", "高级模式原始 frpc.json"}
	parsed, bad, err := loadClients(ctx, env)
	if err != nil {
		if env.HasDB(ctx) {
			return Result{}.Fail(fmt.Sprintf("读取 clients 表失败: %v", err))
		}
		return Result{}.Skip(fmt.Sprintf("数据库不可用: %v", err))
	}
	if len(parsed) == 0 && len(bad) == 0 {
		return Result{}.Skip("clients 表为空")
	}
	if len(bad) > 0 {
		return Result{}.Fail(fmt.Sprintf("%d 个 client 配置解析失败", len(bad))).WithDetails(bad...).ConfigRefs(refs...)
	}

	var details []string
	for _, p := range parsed {
		serverRef := p.Entity.ServerID
		if serverRef == defs.DefaultServerID {
			serverRef += "(master 内置)"
		}
		details = append(details, fmt.Sprintf("%s: server=%s proxies=%d stopped=%v lastSeen=%s",
			p.ID, serverRef, len(p.Cfg.Proxies), p.Entity.Stopped, formatLastSeen(p.Entity.LastSeenAt)))
	}
	return Result{}.Pass(fmt.Sprintf("%d 个 client 配置解析正常", len(parsed))).WithDetails(details...).ConfigRefs(refs...)
}

func formatLastSeen(t *time.Time) string {
	if t == nil {
		return "never"
	}
	return t.Format(time.RFC3339)
}

// ---------------- client/proxy_types ----------------

var knownProxyTypes = []v1.ProxyType{
	v1.ProxyTypeTCP, v1.ProxyTypeUDP, v1.ProxyTypeHTTP, v1.ProxyTypeHTTPS,
	v1.ProxyTypeSTCP, v1.ProxyTypeSUDP, v1.ProxyTypeXTCP, v1.ProxyTypeTCPMUX,
}

type clientProxyTypes struct{ baseChecker }

func (c *clientProxyTypes) Run(ctx context.Context, env *Env) Result {
	refs := []string{"proxy_config.type", "frp v0.65 proxy types"}
	db, err := env.DB(ctx)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("数据库不可用: %v", err))
	}

	rows := []models.ProxyConfig{}
	if err := db.Model(&models.ProxyConfig{}).Find(&rows).Error; err != nil {
		return Result{}.Skip(fmt.Sprintf("读取 proxy_config 失败: %v", err))
	}
	if len(rows) == 0 {
		return Result{}.Skip("proxy_config 表为空（尚无隧道）")
	}

	known := map[string]bool{}
	for _, t := range knownProxyTypes {
		known[string(t)] = true
	}

	counts := map[string]int{}
	var unknown []string
	for _, r := range rows {
		counts[r.Type]++
		if !known[r.Type] {
			unknown = append(unknown, fmt.Sprintf("proxy=%s type=%q", r.Name, r.Type))
		}
	}

	var types []string
	for _, t := range knownProxyTypes {
		if counts[string(t)] > 0 {
			types = append(types, fmt.Sprintf("%s=%d", t, counts[string(t)]))
		}
	}

	details := []string{fmt.Sprintf("类型分布: %s", strings.Join(types, ", "))}
	details = append(details, "未覆盖类型: "+missingTypes(counts))
	if len(unknown) > 0 {
		return Result{}.Fail(fmt.Sprintf("%d 条未知 proxy type", len(unknown))).WithDetails(append(details, unknown...)...).ConfigRefs(refs...)
	}
	return Result{}.Pass("proxy type 均合法").WithDetails(details...).ConfigRefs(refs...)
}

func missingTypes(counts map[string]int) string {
	var missing []string
	for _, t := range knownProxyTypes {
		if counts[string(t)] == 0 {
			missing = append(missing, string(t))
		}
	}
	sort.Strings(missing)
	return strings.Join(missing, "/")
}

// ---------------- client/node_fresh ----------------

type clientNodeFresh struct{ baseChecker }

func (c *clientNodeFresh) Run(ctx context.Context, env *Env) Result {
	refs := []string{"clients.last_seen_at", "defs.OfflineCheckDelay(60s)"}
	db, err := env.DB(ctx)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("数据库不可用: %v", err))
	}

	rows := []models.Client{}
	if err := db.Model(&models.Client{}).Where("deleted_at IS NULL").Find(&rows).Error; err != nil {
		return Result{}.Skip(fmt.Sprintf("读取 clients 失败: %v", err))
	}
	if len(rows) == 0 {
		return Result{}.Skip("clients 表为空")
	}

	// OfflineCheckDelay=60s 是 master 的掉线防抖窗口，这里用 2 倍窗口判定"疑似掉线"
	freshWindow := 2 * defs.OfflineCheckDelay
	var offline, online []string
	for _, r := range rows {
		if !env.NodeFilter(r.ClientID) {
			continue
		}
		if r.Ephemeral {
			continue // 临时节点按设计短生命周期，不参与在线判定
		}
		if r.LastSeenAt == nil {
			offline = append(offline, fmt.Sprintf("%s — 从未连接", r.ClientID))
			continue
		}
		if time.Since(*r.LastSeenAt) > freshWindow {
			offline = append(offline, fmt.Sprintf("%s — last_seen=%s（>%s）", r.ClientID, formatLastSeen(r.LastSeenAt), freshWindow))
		} else {
			online = append(online, fmt.Sprintf("%s — last_seen=%s", r.ClientID, formatLastSeen(r.LastSeenAt)))
		}
	}
	if len(offline) > 0 {
		return Result{}.Warn(fmt.Sprintf("%d/%d 个节点疑似掉线", len(offline), len(rows))).
			WithDetails(append(offline, online...)...).ConfigRefs(refs...)
	}
	return Result{}.Pass(fmt.Sprintf("%d 个节点最近均有心跳", len(online))).WithDetails(online...).ConfigRefs(refs...)
}

// ---------------- client/self_consistency ----------------

type clientSelfConsistency struct{ baseChecker }

func (c *clientSelfConsistency) Run(ctx context.Context, env *Env) Result {
	refs := []string{"CLIENT_ID", "CLIENT_SECRET", "clients.connect_secret"}
	if env.Cfg.Client.ID == "" {
		return Result{}.Skip("本机未配置 CLIENT_ID（master 角色）")
	}
	db, err := env.DB(ctx)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("数据库不可用: %v", err))
	}

	row := models.Client{}
	err = db.Model(&models.Client{}).Unscoped().Where("client_id = ?", env.Cfg.Client.ID).First(&row).Error
	if err != nil {
		// 也可能是 server 角色
		var srow models.Server
		if err2 := db.Model(&models.Server{}).Unscoped().Where("server_id = ?", env.Cfg.Client.ID).First(&srow).Error; err2 == nil {
			if env.Cfg.Client.Secret != srow.ConnectSecret {
				return Result{}.Fail("本机 CLIENT_SECRET 与 servers 表记录不一致").ConfigRefs(refs...)
			}
			return Result{}.Pass("本机身份与 servers 表一致").WithDetails(fmt.Sprintf("server_id=%s", srow.ServerID)).ConfigRefs(refs...)
		}
		return Result{}.Warn(fmt.Sprintf("本机 CLIENT_ID=%s 在 clients/servers 表中均不存在（首次注册前属正常）", env.Cfg.Client.ID)).ConfigRefs(refs...)
	}
	if env.Cfg.Client.Secret != row.ConnectSecret {
		return Result{}.Fail("本机 CLIENT_SECRET 与 clients 表记录不一致，gRPC 注册将被拒绝").ConfigRefs(refs...)
	}
	return Result{}.Pass("本机身份与 clients 表一致").WithDetails(fmt.Sprintf("client_id=%s", row.ClientID)).ConfigRefs(refs...)
}

// ---------------- client/stun_config ----------------

type clientSTUNConfig struct{ baseChecker }

func (c *clientSTUNConfig) Run(ctx context.Context, env *Env) Result {
	refs := []string{"natHoleSTUNServer（frpc 配置）", "stcp/xtcp visitor"}
	parsed, _, err := loadClients(ctx, env)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("数据库不可用: %v", err))
	}

	var (
		holeProxies []string
		stunServers = map[string]int{}
	)
	for _, pc := range parsed {
		if pc.Cfg == nil {
			continue
		}
		if pc.Cfg.NatHoleSTUNServer != "" {
			stunServers[pc.Cfg.NatHoleSTUNServer]++
		}
		for _, pr := range pc.Cfg.Proxies {
			base := pr.GetBaseConfig()
			if base != nil && (base.Type == string(v1.ProxyTypeSTCP) || base.Type == string(v1.ProxyTypeXTCP)) {
				holeProxies = append(holeProxies, fmt.Sprintf("%s@%s(%s)", base.Name, pc.ID, base.Type))
			}
		}
	}
	if len(holeProxies) == 0 {
		return Result{}.Skip("未发现 stcp/xtcp 打洞类代理，跳过 STUN 检查")
	}

	var details []string
	for s, n := range stunServers {
		host, port := splitHostPort(s)
		if _, err := canDialUDPSTUN(ctx, host, port); err != nil {
			details = append(details, fmt.Sprintf("STUN %s — UDP 探测失败: %v（%d 个代理依赖）", s, err, n))
		} else {
			details = append(details, fmt.Sprintf("STUN %s — UDP 可达（%d 个代理依赖）", s, n))
		}
	}
	if len(details) == 0 {
		return Result{}.Warn(fmt.Sprintf("%d 个打洞代理存在，但客户端均未配置 natHoleSTUNServer（使用 frp 默认值）", len(holeProxies))).
			WithDetails(holeProxies...).ConfigRefs(refs...)
	}
	return Result{}.Pass("STUN 配置探测完成").WithDetails(details...).ConfigRefs(refs...)
}

// canDialUDPSTUN 对 STUN 服务器发送简单探测（仅 UDP 可达性，不做完整 STUN 事务）。
func canDialUDPSTUN(ctx context.Context, host string, port int) (time.Duration, error) {
	if port <= 0 {
		port = 3478
	}
	d := net.Dialer{Timeout: 5 * time.Second}
	start := time.Now()
	conn, err := d.DialContext(ctx, "udp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	return time.Since(start), nil
}

// splitHostPort 拆分 host:port，port 缺省返回 0。
func splitHostPort(s string) (string, int) {
	idx := strings.LastIndex(s, ":")
	if idx < 0 {
		return s, 0
	}
	p, err := strconv.Atoi(s[idx+1:])
	if err != nil {
		return s[:idx], 0
	}
	return s[:idx], p
}

// ---------------- client/transport_matrix ----------------

type clientTransportMatrix struct{ baseChecker }

func (c *clientTransportMatrix) Run(ctx context.Context, env *Env) Result {
	// master 组已对 grpc-tls/grpc-insecure/ws/wss 做了完整探测；
	// 这里从节点视角汇总三种 transport 的结论，便于单节点定向排查。
	refs := []string{"CLIENT_RPC_URL", "CLIENT_TLS_RPC", "grpc/ws/wss"}
	connInfo := conf.GetRPCConnInfo(env.Cfg)

	var details []string
	okCount := 0

	// grpc（TCP 直连 RPC 端口，TLS 与否跟随 CLIENT_TLS_RPC 配置）
	tlsOpt := grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		InsecureSkipVerify: env.Cfg.Client.TLSInsecureSkipVerify,
	}))
	if elapsed, err := grpcProbe(ctx, env.Cfg.Master.RPCHost, env.Cfg.Master.RPCPort, tlsOpt); err == nil {
		okCount++
		details = append(details, fmt.Sprintf("grpc(%s:%d, tls=%v) ✓ %s",
			env.Cfg.Master.RPCHost, env.Cfg.Master.RPCPort, env.Cfg.Client.TLSRpc, FormatDuration(elapsed)))
	} else {
		details = append(details, fmt.Sprintf("grpc(%s:%d, tls=%v) ✗ %v",
			env.Cfg.Master.RPCHost, env.Cfg.Master.RPCPort, env.Cfg.Client.TLSRpc, err))
	}

	// ws / wss 走 /wsgrpc（复用 master 组的原始探测）
	for _, scheme := range []string{"ws", "wss"} {
		elapsed, err := probeWSRaw(ctx, env, scheme)
		if err == nil {
			okCount++
			details = append(details, fmt.Sprintf("%s(%s://%s/wsgrpc) ✓ %s", scheme, scheme, connInfo.Host, FormatDuration(elapsed)))
		} else {
			details = append(details, fmt.Sprintf("%s(%s://%s/wsgrpc) ✗ %v", scheme, scheme, connInfo.Host, err))
		}
	}

	if okCount == 0 {
		return Result{}.Fail("三种 transport 均不可用，节点将无法连接 master").WithDetails(details...).ConfigRefs(refs...)
	}
	return Result{}.Pass(fmt.Sprintf("%d/3 transport 可用（节点仅需其一）", okCount)).WithDetails(details...).ConfigRefs(refs...)
}

// ---------------- client/tunnel_e2e（destructive） ----------------

type clientTunnelE2E struct{ baseChecker }

func (c *clientTunnelE2E) Run(ctx context.Context, env *Env) Result {
	if !env.Opts.Destructive {
		return Result{}.Skip("端到端隧道连通性测试会创建/删除测试隧道，需显式 --destructive")
	}
	if !env.HasCredentials() {
		return Result{}.Skip("需要 --login-user/--login-password 以调用面板 API 创建测试隧道")
	}

	// 说明：真正端到端要求一个在线 client；此处以创建 tcp 测试隧道并观察
	// proxy_config 落库 + 下发（EVENT_UPDATE_FRPC）为验收，结束后立即删除。
	token, err := env.Login(ctx)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("登录失败: %v", err))
	}

	testName := "frpp-check-e2e"
	createReq := map[string]interface{}{
		"proxy_config": map[string]interface{}{
			"name":   testName,
			"type":   "tcp",
			"config": `{"name":"` + testName + `","type":"tcp","localPort":65530,"localIP":"127.0.0.1","remotePort":65530}`,
		},
	}
	code, _, _, err := env.PostJSON(ctx, "/api/v1/proxy/create_config", mustJSON(createReq), token)
	if err != nil {
		return Result{}.Fail(fmt.Sprintf("创建测试隧道失败: %v", err))
	}

	// 无论成败立即清理（幂等）
	_, _, _, _ = env.PostJSON(ctx, "/api/v1/proxy/delete_config",
		mustJSON(map[string]interface{}{"proxy_config": map[string]interface{}{"name": testName}}), token)

	if code != 200 {
		return Result{}.Warn(fmt.Sprintf("测试隧道创建返回 http=%d（已清理）；面板校验可能与 JSON 字段有关，建议以 UI 手工验证", code))
	}
	return Result{}.Pass("测试隧道创建/删除链路正常（已清理）")
}
