package checker

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/fengmuxi/frp-panel/biz/common/upgrade"
	"github.com/fengmuxi/frp-panel/conf"
	"github.com/fengmuxi/frp-panel/defs"
	"github.com/fengmuxi/frp-panel/models"
)

func buildEnvGroup(env *Env) ([]Checker, error) {
	return []Checker{
		&envConfigSource{baseChecker{name: "config_source", group: "env", severity: LevelMajor,
			fixHint: "在 master 工作目录创建 .env（或写入 /etc/frpp/.env），至少包含 APP_GLOBAL_SECRET / MASTER_RPC_HOST"}},
		&envGlobalSecret{baseChecker{name: "global_secret", group: "env", severity: LevelCritical,
			fixHint: "设置强随机的 APP_GLOBAL_SECRET（>=32 字符），修改后需同步重新加入所有节点"}},
		&envDeprecatedVars{baseChecker{name: "deprecated_vars", group: "env", severity: LevelMinor,
			fixHint: "删除已失效的环境变量；最新配置以 conf/settings.go 为准（docs/all-configs.md 已注明可能过期）"}},
		&envDBConnect{baseChecker{name: "db_connect", group: "env", severity: LevelCritical,
			fixHint: "检查 DB_TYPE 与 DB_DSN：sqlite 确认路径与权限；mysql/postgres 确认 DSN、网络与账号"}},
		&envPortListening{baseChecker{name: "port_listening", group: "env", severity: LevelMajor,
			fixHint: "确认对应进程已启动；云主机还需在防火墙/安全组放行 9000(REST)、9001(gRPC)、8999(server API)"}},
		&envRPCReachability{baseChecker{name: "rpc_reachability", group: "env", severity: LevelMajor,
			fixHint: "MASTER_RPC_HOST 必须是节点可访问的公网 IP/域名；检查防火墙对 MASTER_RPC_PORT 的放行"}},
		&envURLConsistency{baseChecker{name: "url_consistency", group: "env", severity: LevelMajor,
			fixHint: "让 CLIENT_RPC_URL / CLIENT_API_URL 与 MASTER_API_SCHEME/MASTER_API_HOST/MASTER_API_PORT 拼接一致，或直接清空让面板自行拼接"}},
		&envLogging{baseChecker{name: "logging", group: "env", severity: LevelMinor,
			fixHint: "修正 LOGGER_DEFAULT_LOGGER_LEVEL / LOGGER_FRP_LOGGER_LEVEL 取值（trace|debug|info|warn|error|fatal|panic）"}},
		&envProfiler{baseChecker{name: "profiler", group: "env", severity: LevelMajor,
			fixHint: "如非排查需要请关闭 DEBUG_PROFILER_ENABLED；当前实现监听 0.0.0.0:DEBUG_PROFILER_PORT，务必用防火墙限制来源"}},
		&envOTAStatus{baseChecker{name: "ota_status", group: "env", severity: LevelMajor,
			fixHint: "根据状态文件提示重试 `frpp upgrade`，或回滚 .bak 备份；升级期间请勿重复触发"}},
		&envCacheSize{baseChecker{name: "cache_size", group: "env", severity: LevelMinor,
			fixHint: "调大 MASTER_CACHE_SIZE（MB）；/auth 插件的 token 缓存依赖它"}},
		&envNodeIdentity{baseChecker{name: "node_identity", group: "env", severity: LevelMajor,
			fixHint: "节点侧需要 CLIENT_ID/CLIENT_SECRET；首次接入请使用 `frpp join -j <token>` 自动写入 /etc/frpp/.env"}},
		&envDiskWatermark{baseChecker{name: "disk_watermark", group: "env", severity: LevelMajor,
			fixHint: "磁盘写满会导致 sqlite 写入失败与统计丢失；清理数据目录或扩容，WAL 过大时用 sqlite 执行 PRAGMA wal_checkpoint(TRUNCATE)"}},
		&envFilePermissions{baseChecker{name: "file_permissions", group: "env", severity: LevelMajor,
			fixHint: ".env 含全局密钥、data.db 含租户数据：chmod 600 收紧；全局可写属于高危"}},
		&envTLSVerify{baseChecker{name: "tls_verify", group: "env", severity: LevelMajor,
			fixHint: "TLS_INSECURE_SKIP_VERIFY=true 时节点→master 链路可被 MITM 劫持并喂假 CA；将 master CA 纳入节点信任后关闭该项（安全审计 P1-10）"}},
	}, nil
}

// ---------------- env/tls_verify ----------------

type envTLSVerify struct{ baseChecker }

func (c *envTLSVerify) Run(ctx context.Context, env *Env) Result {
	refs := []string{"CLIENT_TLS_INSECURE_SKIP_VERIFY", "API_SCHEME"}
	if env.Cfg.Client.TLSInsecureSkipVerify {
		details := "节点到 master 的 RPC/API 链路不校验服务端证书：同网段攻击者可劫持流量、伪造配置下发与 OTA 响应"
		if strings.EqualFold(env.Cfg.Master.APIScheme, "http") {
			details += "；API_SCHEME=http 时面板 API 链路完全明文，凭证与 Cookie 可被嗅探"
		}
		return Result{}.Warn("TLS_INSECURE_SKIP_VERIFY=true：TLS 证书校验已关闭").
			WithDetails(details).ConfigRefs(refs...)
	}
	return Result{}.Pass("节点→master 链路启用 TLS 证书校验").ConfigRefs(refs...)
}

// ---------------- env/config_source ----------------

type envConfigSource struct{ baseChecker }

func (c *envConfigSource) Run(ctx context.Context, env *Env) Result {
	var loaded, missing []string
	for _, p := range []string{defs.CurEnvPath, defs.SysEnvPath} {
		if st, err := os.Stat(p); err == nil {
			loaded = append(loaded, fmt.Sprintf("%s (size=%d, mode=%s)", p, st.Size(), st.Mode()))
		} else {
			missing = append(missing, p)
		}
	}

	details := []string{fmt.Sprintf("已加载: %s", strings.Join(loaded, "; "))}
	if len(loaded) == 0 {
		return Result{}.Warn("未发现 .env 配置文件，当前使用运行时环境变量").
			WithDetails(append(details, fmt.Sprintf("查找过: %s", strings.Join(missing, ", ")))...)
	}
	if len(missing) > 0 {
		details = append(details, fmt.Sprintf("不存在: %s", strings.Join(missing, ", ")))
	}
	return Result{}.Pass(fmt.Sprintf("发现 %d 个配置文件", len(loaded))).WithDetails(details...)
}

// ---------------- env/global_secret ----------------

type envGlobalSecret struct{ baseChecker }

func (c *envGlobalSecret) Run(ctx context.Context, env *Env) Result {
	s := env.Cfg.App.GlobalSecret
	refs := []string{"APP_GLOBAL_SECRET", "REQUIRE_STRONG_SECRET", "conf.JWTSecret = SHA1(MASTER_API_HOST:MASTER_API_PORT:APP_GLOBAL_SECRET)"}

	// 弱钥判定与主校验（conf.ValidateGlobalSecret）保持同一口径
	weak := s == "frp-panel" || len(s) < 16

	// 用户已显式放宽（REQUIRE_STRONG_SECRET=false）时降级为警告：
	// 风险依然存在（弱钥派生的 JWT 签名密钥可被离线推导），但属于知情选择，
	// 提示轮换路径而不是每次 check 都红灯阻断。
	if weak && !env.Cfg.App.RequireStrongSecret {
		return Result{}.Warn("APP_GLOBAL_SECRET 为弱值（强钥校验已通过 REQUIRE_STRONG_SECRET=false 显式关闭），建议 `frpp migrate rekey` 轮换后重新开启").
			WithDetails(fmt.Sprintf("当前长度 %d", len(s))).ConfigRefs(refs...)
	}

	if s == "frp-panel" {
		return Result{}.Fail("APP_GLOBAL_SECRET 仍为默认值 frp-panel，节点 secret 与 JWT 均可被推导").
			WithDetails("该密钥派生 JWT 签名密钥与客户端证书签发链路，默认值等于完全暴露控制面；" +
				"过渡兼容可设 APP_REQUIRE_STRONG_SECRET=false，但应尽快轮换").ConfigRefs(refs...)
	}
	if len(s) < 8 {
		return Result{}.Fail(fmt.Sprintf("APP_GLOBAL_SECRET 长度仅 %d，强度不足", len(s))).ConfigRefs(refs...)
	}
	if len(s) < 24 {
		return Result{}.Warn(fmt.Sprintf("APP_GLOBAL_SECRET 长度 %d，建议 >=24 随机字符", len(s))).ConfigRefs(refs...)
	}
	return Result{}.Pass(fmt.Sprintf("APP_GLOBAL_SECRET 已设置（长度 %d）", len(s))).ConfigRefs(refs...)
}

// ---------------- env/deprecated_vars ----------------

// deprecatedEnvVars 列出"文档中出现过、但当前代码已不存在"的配置项。
// 依据：docs/all-configs.md 自述可能过期，conf/settings.go 中没有对应字段。
var deprecatedEnvVars = []struct {
	name string
	note string
}{
	{"MASTER_COMPATIBLE_MODE", "当前代码未读取该变量（仅旧文档提及）；官方 frp 客户端兼容由 /auth 插件与配置注入实现，无需开关"},
	{"MASTER_INTERNAL_FRP_SERVER_PORT", "当前代码无此字段；内置 frps 的地址由 MASTER_INTERNAL_FRP_SERVER_HOST（默认取 MASTER_RPC_HOST）与下发配置决定"},
	{"MASTER_INTERNAL_FRP_AUTH_SERVER_HOST", "当前代码无此字段；frps 认证插件地址由 SERVER_API_PORT 在 conf.FRPsAuthOption 中计算"},
	{"MASTER_INTERNAL_FRP_AUTH_SERVER_PORT", "同上，使用 SERVER_API_PORT（默认 8999）"},
	{"MASTER_INTERNAL_FRP_AUTH_SERVER_PATH", "同上，认证路径固定为 /auth"},
	{"APP_SECRET", "已在 cmd 层显式提示废弃（-a 参数），设置无效"},
}

type envDeprecatedVars struct{ baseChecker }

func (c *envDeprecatedVars) Run(ctx context.Context, env *Env) Result {
	var found []string
	for _, d := range deprecatedEnvVars {
		if _, ok := os.LookupEnv(d.name); ok {
			found = append(found, fmt.Sprintf("%s — %s", d.name, d.note))
		}
	}
	if len(found) == 0 {
		return Result{}.Pass("未发现已失效的旧版环境变量")
	}
	return Result{}.Warn(fmt.Sprintf("检测到 %d 个当前代码不读取的环境变量（不会生效）", len(found))).
		WithDetails(found...).ConfigRefs("conf/settings.go", "docs/all-configs.md")
}

// ---------------- env/db_connect ----------------

type envDBConnect struct{ baseChecker }

func (c *envDBConnect) Run(ctx context.Context, env *Env) Result {
	refs := []string{"DB_TYPE", "DB_DSN"}
	db, err := env.DB(ctx)
	if err != nil {
		return Result{}.Fail(fmt.Sprintf("数据库连接失败: %v", err)).
			WithDetails(SQLiteFallbackHint(env.Cfg)).ConfigRefs(refs...)
	}

	if err := db.Raw("SELECT 1").Row().Err(); err != nil {
		return Result{}.Fail(fmt.Sprintf("数据库健康查询失败: %v", err)).ConfigRefs(refs...)
	}

	// 表结构概要检查（只读）：与 models/db.go AutoMigrate 清单对齐
	var missing []string
	for name, check := range map[string]func() bool{
		"clients":             func() bool { return db.Migrator().HasTable(&models.Client{}) },
		"servers":             func() bool { return db.Migrator().HasTable(&models.Server{}) },
		"users":               func() bool { return db.Migrator().HasTable(&models.User{}) },
		"proxy_config":        func() bool { return db.Migrator().HasTable(&models.ProxyConfig{}) },
		"proxy_stats":         func() bool { return db.Migrator().HasTable(&models.ProxyStats{}) },
		"history_proxy_stats": func() bool { return db.Migrator().HasTable(&models.HistoryProxyStats{}) },
		"workers":             func() bool { return db.Migrator().HasTable(&models.Worker{}) },
		"wireguards":          func() bool { return db.Migrator().HasTable(&models.WireGuard{}) },
		"networks":            func() bool { return db.Migrator().HasTable(&models.Network{}) },
		"endpoints":           func() bool { return db.Migrator().HasTable(&models.Endpoint{}) },
		"wireguard_links":     func() bool { return db.Migrator().HasTable(&models.WireGuardLink{}) },
	} {
		if !check() {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return Result{}.Fail(fmt.Sprintf("缺少数据表: %s（DB 指向了错误的库或版本不匹配）", strings.Join(missing, ", "))).ConfigRefs(refs...)
	}
	return Result{}.Pass(fmt.Sprintf("数据库 %s 连接正常，核心表齐全", env.Cfg.DB.Type)).ConfigRefs(refs...)
}

// ---------------- env/port_listening ----------------

type envPortListening struct{ baseChecker }

func (c *envPortListening) Run(ctx context.Context, env *Env) Result {
	type portCase struct {
		name string
		port int
		ref  string
	}
	cases := []portCase{
		{"master REST API", env.Cfg.Master.APIPort, "MASTER_API_PORT"},
		{"master RPC (gRPC/TLS mux)", env.Cfg.Master.RPCPort, "MASTER_RPC_PORT"},
		{"server API (frps auth 插件宿主)", env.Cfg.Server.APIPort, "SERVER_API_PORT"},
	}
	if env.Cfg.Debug.ProfilerEnabled {
		cases = append(cases, portCase{"pprof profiler", env.Cfg.Debug.ProfilerPort, "DEBUG_PROFILER_PORT"})
	}

	var down, up []string
	for _, p := range cases {
		if p.port <= 0 {
			continue
		}
		if _, err := CanDial(ctx, defs.LocalHost, p.port); err != nil {
			down = append(down, fmt.Sprintf("%s :%d (%s) — 不可达: %v", p.name, p.port, p.ref, err))
		} else {
			up = append(up, fmt.Sprintf("%s :%d — 监听中", p.name, p.port))
		}
	}

	if len(down) > 0 {
		return Result{}.Fail(fmt.Sprintf("%d 个端口未监听", len(down))).
			WithDetails(append(down, up...)...).ConfigRefs("MASTER_API_PORT", "MASTER_RPC_PORT", "SERVER_API_PORT")
	}
	return Result{}.Pass("所有期望端口均监听中").WithDetails(up...).
		ConfigRefs("MASTER_API_PORT", "MASTER_RPC_PORT", "SERVER_API_PORT")
}

// ---------------- env/rpc_reachability ----------------

type envRPCReachability struct{ baseChecker }

func (c *envRPCReachability) Run(ctx context.Context, env *Env) Result {
	host := env.Cfg.Master.RPCHost
	refs := []string{"MASTER_RPC_HOST", "MASTER_RPC_PORT", "CLIENT_RPC_URL"}

	if host == defs.LocalHost || strings.HasPrefix(host, "192.168.") || strings.HasPrefix(host, "10.") {
		return Result{}.Warn(fmt.Sprintf("MASTER_RPC_HOST=%s 为回环/私网地址，远程节点将无法接入", host)).ConfigRefs(refs...)
	}

	if _, err := CanDial(ctx, host, env.Cfg.Master.RPCPort); err != nil {
		return Result{}.Fail(fmt.Sprintf("节点视角 RPC 地址 %s:%d 不可达: %v", host, env.Cfg.Master.RPCPort, err)).ConfigRefs(refs...)
	}
	return Result{}.Pass(fmt.Sprintf("RPC 地址 %s:%d 可达", host, env.Cfg.Master.RPCPort)).ConfigRefs(refs...)
}

// ---------------- env/url_consistency ----------------

type envURLConsistency struct{ baseChecker }

func (c *envURLConsistency) Run(ctx context.Context, env *Env) Result {
	refs := []string{"CLIENT_RPC_URL", "CLIENT_API_URL", "MASTER_API_SCHEME", "MASTER_API_HOST", "MASTER_API_PORT"}

	expectAPI := fmt.Sprintf("%s://%s:%d", env.Cfg.Master.APIScheme, env.Cfg.Master.APIHost, env.Cfg.Master.APIPort)
	var details []string
	bad := 0

	if env.Cfg.Client.APIUrl != "" {
		if err := sameEndpoint(env.Cfg.Client.APIUrl, expectAPI); err != nil {
			bad++
			details = append(details, fmt.Sprintf("CLIENT_API_URL=%s 与面板推断 %s 不一致: %v", env.Cfg.Client.APIUrl, expectAPI, err))
		} else {
			details = append(details, fmt.Sprintf("CLIENT_API_URL=%s ✓", env.Cfg.Client.APIUrl))
		}
	}

	if env.Cfg.Client.RPCUrl != "" {
		if err := validateRPCURL(env.Cfg.Client.RPCUrl); err != nil {
			bad++
			details = append(details, fmt.Sprintf("CLIENT_RPC_URL 非法: %v", err))
		} else {
			details = append(details, fmt.Sprintf("CLIENT_RPC_URL=%s ✓", env.Cfg.Client.RPCUrl))
		}
	}

	if bad > 0 {
		return Result{}.Fail(fmt.Sprintf("%d 处 URL 配置不一致，节点会连到错误地址", bad)).WithDetails(details...).ConfigRefs(refs...)
	}
	return Result{}.Pass("URL 配置一致").WithDetails(details...).ConfigRefs(refs...)
}

func sameEndpoint(got, expect string) error {
	u, err := url.Parse(got)
	if err != nil {
		return err
	}
	eu, err := url.Parse(expect)
	if err != nil {
		return err
	}
	if u.Scheme != eu.Scheme {
		return fmt.Errorf("scheme %s != %s", u.Scheme, eu.Scheme)
	}
	if u.Host != eu.Host {
		return fmt.Errorf("host %s != %s", u.Host, eu.Host)
	}
	return nil
}

func validateRPCURL(rpcURL string) error {
	u, err := url.Parse(rpcURL)
	if err != nil {
		return err
	}
	switch conf.Scheme(u.Scheme) {
	case conf.GRPC, conf.WS, conf.WSS:
		return nil
	}
	return fmt.Errorf("scheme %q 不在 grpc/ws/wss 之列", u.Scheme)
}

// ---------------- env/logging ----------------

type envLogging struct{ baseChecker }

func (c *envLogging) Run(ctx context.Context, env *Env) Result {
	valid := map[string]bool{"trace": true, "debug": true, "info": true, "warn": true, "error": true, "fatal": true, "panic": true}
	var details []string
	bad := 0

	for name, val := range map[string]string{
		"LOGGER_DEFAULT_LOGGER_LEVEL": env.Cfg.Logger.DefaultLoggerLevel,
		"LOGGER_FRP_LOGGER_LEVEL":     env.Cfg.Logger.FRPLoggerLevel,
	} {
		if !valid[strings.ToLower(val)] {
			bad++
			details = append(details, fmt.Sprintf("%s=%s 不是合法的日志级别", name, val))
		} else {
			details = append(details, fmt.Sprintf("%s=%s ✓", name, val))
		}
	}
	if env.Cfg.IsDebug {
		details = append(details, "IS_DEBUG=true：Gin 处于调试模式、SQL 输出 Debug 日志，生产环境建议关闭")
	}

	if bad > 0 {
		return Result{}.Fail(fmt.Sprintf("%d 个日志级别配置非法", bad)).WithDetails(details...).
			ConfigRefs("LOGGER_DEFAULT_LOGGER_LEVEL", "LOGGER_FRP_LOGGER_LEVEL", "IS_DEBUG")
	}
	return Result{}.Pass("日志配置合法").WithDetails(details...).
		ConfigRefs("LOGGER_DEFAULT_LOGGER_LEVEL", "LOGGER_FRP_LOGGER_LEVEL", "IS_DEBUG")
}

// ---------------- env/profiler ----------------

type envProfiler struct{ baseChecker }

func (c *envProfiler) Run(ctx context.Context, env *Env) Result {
	refs := []string{"DEBUG_PROFILER_ENABLED", "DEBUG_PROFILER_PORT"}
	if !env.Cfg.Debug.ProfilerEnabled {
		return Result{}.Pass("pprof profiler 未开启").ConfigRefs(refs...)
	}

	addr := fmt.Sprintf("%s:%d", defs.LocalHost, env.Cfg.Debug.ProfilerPort)
	code, _, body, err := env.Get(ctx, fmt.Sprintf("http://%s/debug/pprof/", addr))
	if err != nil {
		return Result{}.Warn(fmt.Sprintf("profiler 已配置但 /debug/pprof/ 不可达: %v（进程可能未重启）", err)).ConfigRefs(refs...)
	}
	if code != http.StatusOK {
		return Result{}.Warn(fmt.Sprintf("profiler 返回非 200: %d", code)).ConfigRefs(refs...)
	}
	return Result{}.Warn("pprof profiler 已开启且可访问；当前实现监听 0.0.0.0，存在信息泄露面").
		WithDetails(truncate(string(body), 120)).ConfigRefs(refs...)
}

// ---------------- env/ota_status ----------------

type envOTAStatus struct{ baseChecker }

func (c *envOTAStatus) Run(ctx context.Context, env *Env) Result {
	st, path, err := upgrade.ReadStatus("")
	if err != nil {
		return Result{}.Pass("无 OTA 升级状态记录（从未执行过 frpp upgrade）").WithDetails(err.Error())
	}
	details := []string{fmt.Sprintf("status file: %s", path)}
	if !st.Success {
		return Result{}.Fail(fmt.Sprintf("最近一次 OTA 升级失败: %s（%s）", st.Message, st.UpdatedAt.Format(time.RFC3339))).WithDetails(details...)
	}
	details = append(details, fmt.Sprintf("上次升级: %s, message: %s", st.UpdatedAt.Format(time.RFC3339), st.Message))

	// 版本错位检查：状态文件时间晚于当前二进制构建时间 → 可能未重启生效
	if buildDate := mustParseBuildDate(env.Version.BuildDate); !buildDate.IsZero() && st.UpdatedAt.After(buildDate) {
		return Result{}.Warn("OTA 状态晚于当前二进制构建时间，升级可能尚未重启生效").
			WithDetails(append(details, fmt.Sprintf("binary build=%s", env.Version.BuildDate))...)
	}
	return Result{}.Pass("OTA 状态正常").WithDetails(details...)
}

func mustParseBuildDate(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// ---------------- env/cache_size ----------------

type envCacheSize struct{ baseChecker }

func (c *envCacheSize) Run(ctx context.Context, env *Env) Result {
	size := env.Cfg.Master.CacheSize
	refs := []string{"MASTER_CACHE_SIZE"}
	switch {
	case size <= 0:
		return Result{}.Fail("MASTER_CACHE_SIZE <= 0，freecache 将无法初始化").ConfigRefs(refs...)
	case size < 10:
		return Result{}.Warn(fmt.Sprintf("MASTER_CACHE_SIZE=%dMB 偏小：/auth 插件 token 缓存放在此缓存中，过小会频繁回库", size)).ConfigRefs(refs...)
	default:
		return Result{}.Pass(fmt.Sprintf("MASTER_CACHE_SIZE=%dMB", size)).ConfigRefs(refs...)
	}
}

// ---------------- env/node_identity ----------------

type envNodeIdentity struct{ baseChecker }

func (c *envNodeIdentity) Run(ctx context.Context, env *Env) Result {
	refs := []string{"CLIENT_ID", "CLIENT_SECRET"}
	if env.Cfg.Client.ID == "" && env.Cfg.Client.Secret == "" {
		return Result{}.Skip("未配置 CLIENT_ID/CLIENT_SECRET，按 master 角色运行（跳过节点身份检查）").ConfigRefs(refs...)
	}
	var details []string
	if env.Cfg.Client.ID == "" {
		return Result{}.Warn("配置了 CLIENT_SECRET 但缺少 CLIENT_ID（将回退为主机名），可能与已有记录冲突").ConfigRefs(refs...)
	}
	details = append(details, fmt.Sprintf("CLIENT_ID=%s", env.Cfg.Client.ID))

	// 若 DB 可用，确认本机身份在 clients/servers 表中的登记情况（只读计数）
	if db, err := env.DB(ctx); err == nil {
		var cnt int64
		if err := db.Model(&models.Client{}).Unscoped().Where("client_id = ?", env.Cfg.Client.ID).Count(&cnt).Error; err == nil {
			details = append(details, fmt.Sprintf("clients 表中匹配记录: %d", cnt))
		}
		if err := db.Model(&models.Server{}).Unscoped().Where("server_id = ?", env.Cfg.Client.ID).Count(&cnt).Error; err == nil {
			details = append(details, fmt.Sprintf("servers 表中匹配记录: %d", cnt))
		}
	}
	return Result{}.Pass("节点身份已配置").WithDetails(details...).ConfigRefs(refs...)
}

// ---------------- env/disk_watermark ----------------

type envDiskWatermark struct{ baseChecker }

func (c *envDiskWatermark) Run(ctx context.Context, env *Env) Result {
	refs := []string{"DB_DSN（数据目录）", "sqlite -wal 文件"}
	isSQLite := env.Cfg.DB.Type == defs.DBTypeSQLite3 || env.Cfg.DB.Type == defs.DBTypeSqlite
	if !isSQLite {
		return Result{}.Skip("DB 为远端 mysql/postgres，请在 DB 宿主机检查磁盘水位").ConfigRefs(refs...)
	}
	dbPath := sqliteFilePath(env.Cfg.DB.DSN)
	if dbPath == "" {
		return Result{}.Skip("内存库无磁盘水位").ConfigRefs(refs...)
	}

	total, free, err := diskUsage(filepath.Dir(dbPath))
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("读取磁盘用量失败: %v", err)).ConfigRefs(refs...)
	}
	if total == 0 {
		return Result{}.Skip("文件系统容量为 0，跳过").ConfigRefs(refs...)
	}
	freePct := float64(free) / float64(total) * 100
	details := []string{
		fmt.Sprintf("数据目录: %s", filepath.Dir(dbPath)),
		fmt.Sprintf("磁盘: 总 %s / 剩余 %s（剩 %.1f%%）", humanBytes(int64(total)), humanBytes(int64(free)), freePct),
	}

	// sqlite 库与 WAL 体积
	var dbSize, walSize int64
	if st, err := os.Stat(dbPath); err == nil {
		dbSize = st.Size()
		details = append(details, fmt.Sprintf("库文件: %s", humanBytes(dbSize)))
	}
	if st, err := os.Stat(dbPath + "-wal"); err == nil {
		walSize = st.Size()
		details = append(details, fmt.Sprintf("WAL: %s", humanBytes(walSize)))
	}
	if walSize > 256<<20 {
		details = append(details, "WAL 超过 256MB：长期未 checkpoint，执行 PRAGMA wal_checkpoint(TRUNCATE) 回收")
		return Result{}.Warn("sqlite WAL 文件过大").WithDetails(details...).ConfigRefs(refs...)
	}

	switch {
	case free < 1<<30 || freePct < 10:
		return Result{}.Fail(fmt.Sprintf("磁盘剩余 %s（%.1f%%），即将写满", humanBytes(int64(free)), freePct)).WithDetails(details...).ConfigRefs(refs...)
	case free < 5<<30 || freePct < 20:
		return Result{}.Warn(fmt.Sprintf("磁盘剩余 %s（%.1f%%），建议关注", humanBytes(int64(free)), freePct)).WithDetails(details...).ConfigRefs(refs...)
	}
	return Result{}.Pass("磁盘水位健康").WithDetails(details...).ConfigRefs(refs...)
}

// ---------------- env/file_permissions ----------------

type envFilePermissions struct{ baseChecker }

func (c *envFilePermissions) Run(ctx context.Context, env *Env) Result {
	refs := []string{".env 文件", "DB_DSN 指向的库文件"}
	if runtime.GOOS == "windows" {
		return Result{}.Skip("Windows 无 POSIX 权限模型，跳过").ConfigRefs(refs...)
	}
	if env.Cfg.DB.Type != defs.DBTypeSQLite3 && env.Cfg.DB.Type != defs.DBTypeSqlite {
		return Result{}.Skip("仅 sqlite 库文件可本地检查；mysql/postgres 请在 DB 宿主机核查").ConfigRefs(refs...)
	}

	var candidates []string
	candidates = append(candidates, defs.CurEnvPath, defs.SysEnvPath)
	if p := sqliteFilePath(env.Cfg.DB.DSN); p != "" {
		candidates = append(candidates, p)
	}

	var worldWritable, readable, missing []string
	checked := 0
	for _, p := range candidates {
		st, err := os.Stat(p)
		if err != nil {
			missing = append(missing, p)
			continue
		}
		checked++
		mode := st.Mode().Perm()
		switch {
		case mode&0o002 != 0:
			worldWritable = append(worldWritable, fmt.Sprintf("%s 全局可写（%s）—— 高危，立即 chmod 600", p, mode))
		case mode&(0o040|0o004) != 0:
			readable = append(readable, fmt.Sprintf("%s 组/其他用户可读（%s）—— 建议 chmod 600", p, mode))
		}
	}
	if checked == 0 {
		return Result{}.Skip("未找到可检查的 .env/库文件（库可能未初始化）").WithDetails(missing...).ConfigRefs(refs...)
	}
	if len(worldWritable) > 0 {
		return Result{}.Fail(fmt.Sprintf("%d 个敏感文件全局可写", len(worldWritable))).
			WithDetails(append(worldWritable, readable...)...).ConfigRefs(refs...)
	}
	if len(readable) > 0 {
		return Result{}.Warn(fmt.Sprintf("%d 个敏感文件组/其他用户可读", len(readable))).WithDetails(readable...).ConfigRefs(refs...)
	}
	return Result{}.Pass(fmt.Sprintf("%d 个敏感文件权限收紧（0600）", checked)).ConfigRefs(refs...)
}
