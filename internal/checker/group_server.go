package checker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/VaalaCat/frp-panel/defs"
	"github.com/VaalaCat/frp-panel/models"
	v1 "github.com/fatedier/frp/pkg/config/v1"
)

func buildServerGroup(env *Env) ([]Checker, error) {
	return []Checker{
		&serverConfigParse{baseChecker{name: "config_parse", group: "server", severity: LevelMajor,
			fixHint: "servers.ConfigContent 必须可解析为 frp v1.ServerConfig（高级模式原始 json 亦需合法）；在面板中重新保存一次即可校验"}},
		&serverBindPortConflict{baseChecker{name: "bindport_conflict", group: "server", severity: LevelMajor,
			fixHint: "修正冲突的 bindPort/vhost 端口；多 frps 假设部署在不同主机，同机部署请保证端口互斥"}},
		&serverAuthPlugin{baseChecker{name: "auth_plugin", group: "server", severity: LevelMajor,
			fixHint: "SERVER_API_PORT 上的 /auth 是 frps multiuser 认证插件宿主，必须可达且拒绝空请求"}},
		&serverStatsFresh{baseChecker{name: "stats_fresh", group: "server", severity: LevelMinor,
			fixHint: "PushProxyInfo 周期为 30s（defs.PushProxyInfoDuration）；长时间未更新说明 server 侧上报中断"}},
		&serverStatsMonotonic{baseChecker{name: "stats_monotonic", group: "server", severity: LevelMajor,
			fixHint: "流量计数出现回退/溢出通常意味着 ProxyInfo 重置或 int64 溢出；检查 server 侧统计聚合逻辑"}},
	}, nil
}

// parsedServer 汇总解析后的 server 配置要点。
type parsedServer struct {
	ID    string
	Name  string
	Cfg   *v1.ServerConfig
	Ports []portRef
}

type portRef struct {
	port  int
	field string
}

// loadServers 读取并解析 servers 表（只读）。
func loadServers(ctx context.Context, env *Env) ([]parsedServer, []string, error) {
	db, err := env.DB(ctx)
	if err != nil {
		return nil, nil, err
	}
	var servers []models.Server
	if err := db.Model(&models.Server{}).Where("deleted_at IS NULL").Find(&servers).Error; err != nil {
		return nil, nil, fmt.Errorf("query servers: %w", err)
	}

	var (
		parsed []parsedServer
		bad    []string
	)
	for _, s := range servers {
		if !env.NodeFilter(s.ServerID) {
			continue
		}
		if len(s.ConfigContent) == 0 {
			bad = append(bad, fmt.Sprintf("%s: 配置为空", s.ServerID))
			continue
		}
		cfg := &v1.ServerConfig{}
		if err := json.Unmarshal(s.ConfigContent, cfg); err != nil {
			bad = append(bad, fmt.Sprintf("%s: 解析失败: %v", s.ServerID, err))
			continue
		}
		ps := parsedServer{ID: s.ServerID, Cfg: cfg}
		if cfg.BindPort > 0 {
			ps.Ports = append(ps.Ports, portRef{int(cfg.BindPort), "bindPort"})
		}
		if cfg.KCPBindPort > 0 {
			ps.Ports = append(ps.Ports, portRef{int(cfg.KCPBindPort), "kcpBindPort"})
		}
		if cfg.QUICBindPort > 0 {
			ps.Ports = append(ps.Ports, portRef{int(cfg.QUICBindPort), "quicBindPort"})
		}
		if cfg.VhostHTTPPort > 0 {
			ps.Ports = append(ps.Ports, portRef{int(cfg.VhostHTTPPort), "vhostHTTPPort"})
		}
		if cfg.VhostHTTPSPort > 0 {
			ps.Ports = append(ps.Ports, portRef{int(cfg.VhostHTTPSPort), "vhostHTTPSPort"})
		}
		if cfg.WebServer.Port > 0 {
			ps.Ports = append(ps.Ports, portRef{int(cfg.WebServer.Port), "webServer.port"})
		}
		if cfg.SSHTunnelGateway.BindPort > 0 {
			ps.Ports = append(ps.Ports, portRef{int(cfg.SSHTunnelGateway.BindPort), "sshTunnelGateway.bindPort"})
		}
		parsed = append(parsed, ps)
	}
	return parsed, bad, nil
}

// ---------------- server/config_parse ----------------

type serverConfigParse struct{ baseChecker }

func (c *serverConfigParse) Run(ctx context.Context, env *Env) Result {
	refs := []string{"servers.config_content", "高级模式原始 frps.json"}
	parsed, bad, err := loadServers(ctx, env)
	if err != nil {
		if env.HasDB(ctx) {
			return Result{}.Fail(fmt.Sprintf("读取 servers 表失败: %v", err))
		}
		return Result{}.Skip(fmt.Sprintf("数据库不可用: %v", err))
	}
	if len(parsed) == 0 && len(bad) == 0 {
		return Result{}.Skip("servers 表为空（未创建 frps）")
	}
	if len(bad) > 0 {
		return Result{}.Fail(fmt.Sprintf("%d 个 server 配置解析失败", len(bad))).WithDetails(bad...).ConfigRefs(refs...)
	}

	var details []string
	for _, p := range parsed {
		details = append(details, fmt.Sprintf("%s: bindPort=%d auth.token=%s(已隐藏长度) httpPlugins=%d",
			p.ID, p.Cfg.BindPort, maskSecret(p.Cfg.Auth.Token), len(p.Cfg.HTTPPlugins)))
	}
	return Result{}.Pass(fmt.Sprintf("%d 个 server 配置解析正常", len(parsed))).WithDetails(details...).ConfigRefs(refs...)
}

func maskSecret(s string) string {
	if s == "" {
		return "<空>"
	}
	return "<len=" + strconv.Itoa(len(s)) + ">"
}

// ---------------- server/bindport_conflict ----------------

type serverBindPortConflict struct{ baseChecker }

func (c *serverBindPortConflict) Run(ctx context.Context, env *Env) Result {
	refs := []string{"servers.config_content(bindPort)", "MASTER_API_PORT/MASTER_RPC_PORT/SERVER_API_PORT"}
	parsed, _, err := loadServers(ctx, env)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("数据库不可用: %v", err))
	}
	if len(parsed) == 0 {
		return Result{}.Skip("servers 表为空")
	}

	masterPorts := map[int]string{
		env.Cfg.Master.APIPort:     "MASTER_API_PORT",
		env.Cfg.Master.RPCPort:     "MASTER_RPC_PORT",
		env.Cfg.Server.APIPort:     "SERVER_API_PORT",
		env.Cfg.Debug.ProfilerPort: "DEBUG_PROFILER_PORT",
	}

	seen := map[int]string{}
	var conflicts, hits []string
	for _, p := range parsed {
		for _, pr := range p.Ports {
			if owner, ok := seen[pr.port]; ok {
				conflicts = append(conflicts, fmt.Sprintf("端口 %d(%s) 同时被 %s 与 %s 使用", pr.port, pr.field, owner, p.ID))
			} else {
				seen[pr.port] = p.ID
			}
			// 与 master 自身端口冲突（profiler 未启用时不算）
			if ref, ok := masterPorts[pr.port]; ok {
				if ref != "DEBUG_PROFILER_PORT" || env.Cfg.Debug.ProfilerEnabled {
					conflicts = append(conflicts, fmt.Sprintf("%s 的 %s=%d 与 master 的 %s 冲突", p.ID, pr.field, pr.port, ref))
				}
			}
			// 本机占用快速验证（若 frps 在本机，端口应已被监听；否则提示）
			if _, err := CanDial(ctx, defs.LocalHost, pr.port); err == nil {
				hits = append(hits, fmt.Sprintf("%s %s=%d — 本机已有进程监听", p.ID, pr.field, pr.port))
			}
		}
	}
	if len(conflicts) > 0 {
		return Result{}.Fail(fmt.Sprintf("%d 处端口冲突", len(conflicts))).WithDetails(conflicts...).ConfigRefs(refs...)
	}
	return Result{}.Pass("无端口冲突").WithDetails(hits...).ConfigRefs(refs...)
}

// ---------------- server/auth_plugin ----------------

type serverAuthPlugin struct{ baseChecker }

func (c *serverAuthPlugin) Run(ctx context.Context, env *Env) Result {
	refs := []string{"SERVER_API_PORT", "POST /auth", "biz/server/rpc_pull_config.go:InjectAuthPlugin"}
	// 与 conf.FRPsAuthOption 相同的宿主地址：http://127.0.0.1:{SERVER_API_PORT}/auth
	authURL := fmt.Sprintf("http://%s:%d/auth", defs.LocalHost, env.Cfg.Server.APIPort)
	code, _, body, err := env.PostJSON(ctx, authURL, "{}")
	if err != nil {
		return Result{}.Fail(fmt.Sprintf("认证插件宿主 %s 不可达: %v", authURL, err)).ConfigRefs(refs...)
	}
	// 空请求应被 BindJSON 或业务逻辑拒绝（reject=true 或 400），而非 200 通过
	if code == http.StatusOK && strings.Contains(string(body), "\"unchange\":true") {
		return Result{}.Fail("空请求通过了 frps 认证插件，鉴权失效").ConfigRefs(refs...)
	}
	return Result{}.Pass("认证插件宿主可达且拒绝空请求").WithDetails(fmt.Sprintf("http=%d", code), authURL).ConfigRefs(refs...)
}

// ---------------- server/stats_fresh ----------------

type serverStatsFresh struct{ baseChecker }

func (c *serverStatsFresh) Run(ctx context.Context, env *Env) Result {
	refs := []string{"proxy_stats.updated_at", "EVENT_GET_PROXY_INFO", "defs.PushProxyInfoDuration"}
	db, err := env.DB(ctx)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("数据库不可用: %v", err))
	}

	var stale []string
	var total int64
	rows := []models.ProxyStats{}
	if err := db.Model(&models.ProxyStats{}).Where("deleted_at IS NULL").Find(&rows).Error; err != nil {
		return Result{}.Skip(fmt.Sprintf("读取 proxy_stats 失败: %v", err))
	}
	total = int64(len(rows))
	for _, r := range rows {
		if !env.NodeFilter(r.ServerID, r.ClientID) {
			continue
		}
		if time.Since(r.UpdatedAt) > 5*time.Minute {
			stale = append(stale, fmt.Sprintf("proxy=%s/%s(%s) last=%s", r.ServerID, r.ClientID, r.Name, r.UpdatedAt.Format(time.RFC3339)))
		}
	}
	if len(stale) > 0 {
		return Result{}.Warn(fmt.Sprintf("%d/%d 条流量统计超过 5 分钟未更新", len(stale), total)).
			WithDetails(stale...).ConfigRefs(refs...)
	}
	if total == 0 {
		return Result{}.Skip("proxy_stats 为空（尚无隧道流量上报）")
	}
	return Result{}.Pass(fmt.Sprintf("%d 条流量统计均为新鲜状态", total)).ConfigRefs(refs...)
}

// ---------------- server/stats_monotonic ----------------

type serverStatsMonotonic struct{ baseChecker }

func (c *serverStatsMonotonic) Run(ctx context.Context, env *Env) Result {
	refs := []string{"proxy_stats.today/history", "history_proxy_stats"}
	db, err := env.DB(ctx)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("数据库不可用: %v", err))
	}

	var problems []string
	rows := []models.ProxyStats{}
	if err := db.Model(&models.ProxyStats{}).Where("deleted_at IS NULL").Find(&rows).Error; err != nil {
		return Result{}.Skip(fmt.Sprintf("读取 proxy_stats 失败: %v", err))
	}
	for _, r := range rows {
		if !env.NodeFilter(r.ServerID, r.ClientID) {
			continue
		}
		key := fmt.Sprintf("proxy=%s/%s/%s", r.ServerID, r.ClientID, r.Name)
		if r.TodayTrafficIn < 0 || r.TodayTrafficOut < 0 || r.HistoryTrafficIn < 0 || r.HistoryTrafficOut < 0 {
			problems = append(problems, key+" 存在负数流量计数（溢出或脏数据）")
			continue
		}
		if r.HistoryTrafficIn > 0 && r.TodayTrafficIn > r.HistoryTrafficIn {
			problems = append(problems, fmt.Sprintf("%s today_in(%d) > history_in(%d)：历史累计未随日结滚动", key, r.TodayTrafficIn, r.HistoryTrafficIn))
		}
	}

	// history_proxy_stats 中不应出现负值
	var negHistory int64
	if err := db.Model(&models.HistoryProxyStats{}).
		Where("traffic_in < 0 OR traffic_out < 0").Count(&negHistory).Error; err == nil && negHistory > 0 {
		problems = append(problems, fmt.Sprintf("history_proxy_stats 有 %d 条负数记录", negHistory))
	}

	if len(problems) > 0 {
		return Result{}.Fail(fmt.Sprintf("%d 条流量统计异常", len(problems))).WithDetails(problems...).ConfigRefs(refs...)
	}
	return Result{}.Pass("流量计数单调性正常").WithDetails(fmt.Sprintf("检查了 %d 条 proxy_stats", len(rows))).ConfigRefs(refs...)
}
