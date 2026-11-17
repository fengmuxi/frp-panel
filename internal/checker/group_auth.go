package checker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/VaalaCat/frp-panel/conf"
	"github.com/VaalaCat/frp-panel/defs"
	"github.com/VaalaCat/frp-panel/models"
	"github.com/VaalaCat/frp-panel/pb"
	"github.com/VaalaCat/frp-panel/utils"
	"github.com/samber/lo"
	"google.golang.org/protobuf/proto"
)

func buildAuthGroup(env *Env) ([]Checker, error) {
	return []Checker{
		&authRegisterGate{baseChecker{name: "register_gate", group: "auth", severity: LevelMajor,
			fixHint: "生产环境保持 APP_ENABLE_REGISTER=false；首管理员注册完成后务必关闭"}},
		&authLoginInvalid{baseChecker{name: "login_invalid", group: "auth", severity: LevelCritical,
			fixHint: "若错误口令能登录，检查登录处理逻辑与数据库口令哈希（utils.HashPassword/bcrypt）"}},
		&authLoginValid{baseChecker{name: "login_valid", group: "auth", severity: LevelMinor,
			fixHint: "使用 --login-user/--login-password 提供凭证以启用登录链路与 Cookie 属性检查"}},
		&authJWTForged{baseChecker{name: "jwt_forged", group: "auth", severity: LevelCritical,
			fixHint: "若伪造 token 通过校验，说明 APP_GLOBAL_SECRET 与 JWT 派生链路异常，立即更换密钥并全员重新登录"}},
		&authJWTExpired{baseChecker{name: "jwt_expired", group: "auth", severity: LevelCritical,
			fixHint: "过期 token 仍可用说明 jwt v5 的 exp 校验未生效，检查 utils.ParseToken 与中间件版本"}},
		&authProtectedMatrix{baseChecker{name: "protected_matrix", group: "auth", severity: LevelMajor,
			fixHint: "受保护路由对匿名请求必须返回 401；出现 200/404 说明中间件挂载不完整"}},
		&authFRPPluginReject{baseChecker{name: "frp_plugin_reject", group: "auth", severity: LevelMajor,
			fixHint: "frps multiuser 认证插件必须拒绝非法 token；检查 /auth 端点与 freecache 初始化"}},
		&authFRPPluginAccept{baseChecker{name: "frp_plugin_accept", group: "auth", severity: LevelMinor,
			fixHint: "需要数据库可读；确认 users 表存在且用户 token 非空（user.token 用于 frps 认证）"}},
		&authCertReject{baseChecker{name: "cert_reject", group: "auth", severity: LevelMajor,
			fixHint: "/api/v1/auth/cert 用错误 secret 必须拒绝，否则任意节点可伪造证书接入 gRPC"}},
		&authWSGRPCOrigin{baseChecker{name: "wsgrpc_auth", group: "auth", severity: LevelMajor,
			fixHint: "/wsgrpc 是节点的 gRPC-over-WebSocket 通道；CheckOrigin 必须校验 Origin 与 Host 一致，否则任意网页可发起跨站 WS 连接探测节点（安全审计 P2-9）"}},
		&authTokenInURL{baseChecker{name: "token_in_url", group: "auth", severity: LevelMinor,
			fixHint: "?token= 查询参数会落入反代/Referer 日志；建议逐步迁移到 Header/Cookie 通道（ws-transport 节点当前仍依赖，保留观测）"}},
		&authRateLimit{baseChecker{name: "rate_limit", group: "auth", severity: LevelMajor,
			fixHint: "登录/注册/cert 插件路径必须挂限流中间件，否则可无限爆破口令与节点 secret（安全审计 P1-5）"}},
	}, nil
}

// ---------------- auth/wsgrpc_auth（静态配置面检查：CheckOrigin 校验是否在位） ----------------

type authWSGRPCOrigin struct{ baseChecker }

func (c *authWSGRPCOrigin) Run(ctx context.Context, env *Env) Result {
	refs := []string{"/wsgrpc", "cmd/frpp/shared/providers.go NewWSUpgrader", "biz/master/shell/handler.go getUpgrader"}
	// 本项检查的是部署的二进制是否携带修复：间接以登录可用性为前提探测升级面。
	// 匿名 POST /wsgrpc：修复后的实现要求 WebSocket 升级头 + Origin 校验，
	// 普通 HTTP 请求返回 400/426/401 等即视为升级面有门禁；200 或 101 之外的静默 0 字节响应可疑。
	code, _, _, err := env.PostProto(ctx, "/wsgrpc", &pb.LoginRequest{})
	if err != nil {
		// 网络层错误但端口可达（连接被升级逻辑中断等）视为有门禁
		return Result{}.Pass("/wsgrpc 拒绝普通 HTTP 请求（升级面有门禁）").ConfigRefs(refs...)
	}
	switch code {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusMethodNotAllowed, http.StatusUpgradeRequired, http.StatusNotFound:
		return Result{}.Pass(fmt.Sprintf("/wsgrpc 对非 WS 请求返回 %d，升级面有门禁", code)).ConfigRefs(refs...)
	default:
		return Result{}.Warn(fmt.Sprintf("/wsgrpc 对匿名 POST 返回 http=%d，请人工确认 CheckOrigin 与升级鉴权在位", code)).
			ConfigRefs(refs...)
	}
}

// ---------------- auth/token_in_url（配置观测项：?token= 通道是否启用） ----------------

type authTokenInURL struct{ baseChecker }

func (c *authTokenInURL) Run(ctx context.Context, env *Env) Result {
	refs := []string{"middleware/jwt.go ?token= 查询参数通道", "defs.TokenKey"}
	// 该通道当前被 ws-transport 节点与前端兼容依赖，无法直接从配置关闭；
	// 观测项：提示其日志泄露面。用无效 token 探测仅验证中间件不崩。
	probe := "/api/v1/user/info?token=invalid-probe-token"
	code, _, _, err := env.Get(ctx, probe)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("master API 不可达: %v", err)).ConfigRefs(refs...)
	}
	if code == http.StatusUnauthorized {
		return Result{}.Warn("?token= 查询参数认证通道处于启用状态（无效 token 已正确拒绝）").
			WithDetails("URL 中的 token 会落入反代访问日志与 Referer 头；ws-transport 节点迁移到 Header/Cookie 后可移除该通道").ConfigRefs(refs...)
	}
	return Result{}.Warn(fmt.Sprintf("?token= 探测返回 http=%d（预期 401），请人工核对该通道行为", code)).ConfigRefs(refs...)
}

// ---------------- auth/rate_limit（部署面检查：登录爆破限流是否生效） ----------------

type authRateLimit struct{ baseChecker }

func (c *authRateLimit) Run(ctx context.Context, env *Env) Result {
	refs := []string{"middleware/ratelimit.go", "/api/v1/auth/login"}
	// 连发 N 次错误口令登录：挂了限流的部署在窗口内应出现 429。
	const attempts = 8
	var lastCode int
	for i := 0; i < attempts; i++ {
		code, _, _, err := env.PostProto(ctx, "/api/v1/auth/login", &pb.LoginRequest{
			Username: lo.ToPtr("checker-rate-limit-probe"), Password: lo.ToPtr("wrong-password"),
		})
		if err != nil {
			return Result{}.Skip(fmt.Sprintf("master API 不可达: %v", err)).ConfigRefs(refs...)
		}
		lastCode = code
		if code == http.StatusTooManyRequests {
			return Result{}.Pass(fmt.Sprintf("第 %d 次失败登录被 429 限流", i+1)).ConfigRefs(refs...)
		}
	}
	return Result{}.Warn(fmt.Sprintf("连续 %d 次失败登录未被限流（最后 http=%d）：登录口令可被无限爆破", attempts, lastCode)).
		ConfigRefs(refs...)
}

// ---------------- auth/register_gate ----------------

type authRegisterGate struct{ baseChecker }

func (c *authRegisterGate) Run(ctx context.Context, env *Env) Result {
	refs := []string{"APP_ENABLE_REGISTER", "/api/v1/auth/register"}

	// 空请求：任何情况下都应被参数校验拒绝，且不会产生记录
	code, _, body, err := env.PostProto(ctx, "/api/v1/auth/register", &pb.RegisterRequest{})
	if err != nil {
		return Result{}.Fail(fmt.Sprintf("注册接口不可达: %v", err)).ConfigRefs(refs...)
	}
	resp := &pb.RegisterResponse{}
	_ = proto.Unmarshal(body, resp)

	if env.Cfg.App.EnableRegister {
		return Result{}.Warn("APP_ENABLE_REGISTER=true：任意来访者都可注册用户，确认是否有意开放").
			WithDetails(fmt.Sprintf("空请求返回 http=%d code=%s", code, resp.GetStatus().GetCode())).ConfigRefs(refs...)
	}

	// 注册关闭时：空请求被参数校验拒绝（invalid），或用户已存在时被 register is disabled 拒绝
	if resp.GetStatus().GetCode() == pb.RespCode_RESP_CODE_INVALID ||
		code == http.StatusBadRequest || strings.Contains(resp.GetStatus().GetMessage(), "disabled") {
		return Result{}.Pass("注册关闭且接口拒绝匿名注册").
			WithDetails(fmt.Sprintf("http=%d, code=%s(%s)", code, resp.GetStatus().GetCode(), resp.GetStatus().GetMessage())).ConfigRefs(refs...)
	}
	return Result{}.Warn(fmt.Sprintf("APP_ENABLE_REGISTER=false 但接口返回异常：http=%d code=%s msg=%s",
		code, resp.GetStatus().GetCode(), resp.GetStatus().GetMessage())).ConfigRefs(refs...)
}

// ---------------- auth/login_invalid ----------------

type authLoginInvalid struct{ baseChecker }

func (c *authLoginInvalid) Run(ctx context.Context, env *Env) Result {
	refs := []string{"/api/v1/auth/login", "users.password(bcrypt)"}
	code, hdr, body, err := env.PostProto(ctx, "/api/v1/auth/login",
		&pb.LoginRequest{Username: proto.String("frpp-check-invalid-user"), Password: proto.String("frpp-check-invalid-pass")})
	if err != nil {
		return Result{}.Fail(fmt.Sprintf("登录接口不可达: %v", err)).ConfigRefs(refs...)
	}
	resp := &pb.LoginResponse{}
	if err := proto.Unmarshal(body, resp); err != nil {
		return Result{}.Fail(fmt.Sprintf("登录响应非 protobuf: %v", err)).ConfigRefs(refs...)
	}
	if resp.GetStatus().GetCode() == pb.RespCode_RESP_CODE_SUCCESS || resp.GetToken() != "" {
		return Result{}.Fail("错误口令登录成功！").ConfigRefs(refs...)
	}
	if hdr.Get("Set-Cookie") != "" {
		return Result{}.Fail("登录失败却下发了会话 Cookie，可能导致会话混淆").ConfigRefs(refs...)
	}
	_ = code
	return Result{}.Pass("错误口令被拒绝").WithDetails(fmt.Sprintf("msg=%s", resp.GetStatus().GetMessage())).ConfigRefs(refs...)
}

// ---------------- auth/login_valid ----------------

type authLoginValid struct{ baseChecker }

func (c *authLoginValid) Run(ctx context.Context, env *Env) Result {
	refs := []string{"APP_COOKIE_NAME", "APP_COOKIE_AGE", "APP_COOKIE_SECURE", "APP_COOKIE_HTTP_ONLY", "APP_COOKIE_PATH", "APP_COOKIE_DOMAIN"}
	if !env.HasCredentials() {
		return Result{}.Skip("未提供登录凭证（--login-user/--login-password）").ConfigRefs(refs...)
	}

	code, hdr, body, err := env.PostProto(ctx, "/api/v1/auth/login",
		&pb.LoginRequest{Username: proto.String(env.Opts.AdminUser), Password: proto.String(env.Opts.AdminPassword)})
	if err != nil {
		return Result{}.Fail(fmt.Sprintf("登录请求失败: %v", err)).ConfigRefs(refs...)
	}
	resp := &pb.LoginResponse{}
	_ = proto.Unmarshal(body, resp)
	if code != http.StatusOK || resp.GetStatus().GetCode() != pb.RespCode_RESP_CODE_SUCCESS {
		return Result{}.Fail(fmt.Sprintf("正确凭证登录失败: http=%d msg=%s", code, resp.GetStatus().GetMessage())).ConfigRefs(refs...)
	}

	var details []string

	// 1) token 可用项目派生密钥本地解析，且携带 x-vaala-userid
	token := resp.GetToken()
	claims, err := utils.ParseToken(conf.JWTSecret(env.Cfg), token)
	if err != nil {
		details = append(details, fmt.Sprintf("token 无法用本地派生密钥解析: %v", err))
		return Result{}.Fail("JWT 派生链路（APIHost:APIPort:GlobalSecret）与登录签发不一致").WithDetails(details...).ConfigRefs(refs...)
	}
	details = append(details, fmt.Sprintf("token 解析成功，uid=%v, exp=%v", claims[defs.UserIDKey], claims["exp"]))

	// 2) Set-Cookie 属性与配置一致
	setCookies := hdr.Values("Set-Cookie")
	if len(setCookies) == 0 {
		return Result{}.Fail("登录成功但未下发 Set-Cookie").WithDetails(details...).ConfigRefs(refs...)
	}
	cookieProblems := checkCookieAttrs(setCookies, env)
	if len(cookieProblems) > 0 {
		return Result{}.Fail("Cookie 属性与配置不符").WithDetails(append(details, cookieProblems...)...).ConfigRefs(refs...)
	}
	details = append(details, fmt.Sprintf("Set-Cookie 属性符合配置: name=%s httponly=%v secure=%v path=%s",
		env.Cfg.App.CookieName, env.Cfg.App.CookieHTTPOnly, env.Cfg.App.CookieSecure, env.Cfg.App.CookiePath))
	return Result{}.Pass("登录链路与 Cookie 属性检查通过").WithDetails(details...).ConfigRefs(refs...)
}

// checkCookieAttrs 校验 Set-Cookie 的关键属性是否符合 conf 配置。
func checkCookieAttrs(setCookies []string, env *Env) []string {
	var problems []string
	var matched *parsedCookie
	for _, raw := range setCookies {
		pc := parseSetCookie(raw)
		if pc.name == env.Cfg.App.CookieName {
			matched = pc
			break
		}
	}
	if matched == nil {
		return []string{fmt.Sprintf("未找到名为 %s 的 Cookie，实际: %v", env.Cfg.App.CookieName, setCookies)}
	}
	if matched.httpOnly != env.Cfg.App.CookieHTTPOnly {
		problems = append(problems, fmt.Sprintf("HttpOnly=%v 期望 %v", matched.httpOnly, env.Cfg.App.CookieHTTPOnly))
	}
	if matched.secure != env.Cfg.App.CookieSecure {
		problems = append(problems, fmt.Sprintf("Secure=%v 期望 %v", matched.secure, env.Cfg.App.CookieSecure))
	}
	if env.Cfg.App.CookiePath != "" && matched.path != env.Cfg.App.CookiePath {
		problems = append(problems, fmt.Sprintf("Path=%s 期望 %s", matched.path, env.Cfg.App.CookiePath))
	}
	if env.Cfg.App.CookieDomain != "" && matched.domain != env.Cfg.App.CookieDomain {
		problems = append(problems, fmt.Sprintf("Domain=%s 期望 %s", matched.domain, env.Cfg.App.CookieDomain))
	}
	if matched.maxAge > 0 && matched.maxAge != env.Cfg.App.CookieAge {
		problems = append(problems, fmt.Sprintf("Max-Age=%d 期望 %d", matched.maxAge, env.Cfg.App.CookieAge))
	}
	// SameSite 为 CSRF 纵深防御属性（master 自 2026-11 起显式下发 SameSite=Lax）；
	// 缺失时浏览器按各版本默认值兜底，跨站防线不保证，因此提示而非放行
	if matched.sameSite == "" {
		problems = append(problems, "缺少 SameSite 属性（建议后端 SetSameSite(http.SameSiteLaxMode) 显式下发）")
	}
	if strings.EqualFold(matched.sameSite, "none") && !matched.secure {
		problems = append(problems, "SameSite=None 必须配合 Secure=true，否则现代浏览器将拒绝该 Cookie")
	}
	return problems
}

type parsedCookie struct {
	name     string
	path     string
	domain   string
	secure   bool
	httpOnly bool
	maxAge   int
	sameSite string
}

// parseSetCookie 解析单条 Set-Cookie 头（足够覆盖本项目的属性集）。
func parseSetCookie(raw string) *parsedCookie {
	pc := &parsedCookie{}
	parts := strings.Split(raw, ";")
	if len(parts) == 0 {
		return pc
	}
	kv := strings.SplitN(strings.TrimSpace(parts[0]), "=", 2)
	pc.name = kv[0]
	for _, attr := range parts[1:] {
		attr = strings.TrimSpace(attr)
		lower := strings.ToLower(attr)
		switch {
		case lower == "secure":
			pc.secure = true
		case lower == "httponly":
			pc.httpOnly = true
		case strings.HasPrefix(lower, "path="):
			pc.path = strings.TrimPrefix(attr, "Path=")
			pc.path = strings.TrimPrefix(pc.path, "path=")
		case strings.HasPrefix(lower, "domain="):
			pc.domain = strings.TrimPrefix(attr, "Domain=")
			pc.domain = strings.TrimPrefix(pc.domain, "domain=")
		case strings.HasPrefix(lower, "max-age="):
			fmt.Sscanf(lower, "max-age=%d", &pc.maxAge)
		case strings.HasPrefix(lower, "samesite="):
			pc.sameSite = strings.TrimPrefix(strings.TrimPrefix(attr, "SameSite="), "samesite=")
		}
	}
	return pc
}

// ---------------- auth/jwt_forged / jwt_expired ----------------

type authJWTForged struct{ baseChecker }

func (c *authJWTForged) Run(ctx context.Context, env *Env) Result {
	refs := []string{"APP_GLOBAL_SECRET", "conf.JWTSecret"}
	// 用错误密钥签发格式合法但签名错误的 token
	forged, err := utils.GetJwtTokenFromMap("frpp-check-wrong-secret", time.Now().Unix(), 3600,
		map[string]interface{}{defs.UserIDKey: 1})
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("生成本地测试 token 失败: %v", err)).ConfigRefs(refs...)
	}

	code, _, body, err := env.PostProto(ctx, "/api/v1/user/get", &pb.GetUserInfoRequest{}, forged)
	if err != nil {
		return Result{}.Fail(fmt.Sprintf("请求失败: %v", err)).ConfigRefs(refs...)
	}
	if code == http.StatusUnauthorized {
		return Result{}.Pass("伪造签名的 token 被拒绝(401)").ConfigRefs(refs...)
	}
	return Result{}.Fail(fmt.Sprintf("伪造 token 未被 401 拒绝（http=%d, body=%s）", code, truncate(string(body), 120))).ConfigRefs(refs...)
}

type authJWTExpired struct{ baseChecker }

func (c *authJWTExpired) Run(ctx context.Context, env *Env) Result {
	refs := []string{"APP_COOKIE_AGE", "utils.ParseToken"}
	age := int64(env.Cfg.App.CookieAge)
	if age <= 0 {
		age = 86400
	}
	// 用正确密钥签发早已过期的 token
	expired, err := utils.GetJwtTokenFromMap(conf.JWTSecret(env.Cfg), time.Now().Add(-2*time.Duration(age)*time.Second).Unix(), age,
		map[string]interface{}{defs.UserIDKey: 1})
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("生成本地测试 token 失败: %v", err)).ConfigRefs(refs...)
	}

	code, _, body, err := env.PostProto(ctx, "/api/v1/user/get", &pb.GetUserInfoRequest{}, expired)
	if err != nil {
		return Result{}.Fail(fmt.Sprintf("请求失败: %v", err)).ConfigRefs(refs...)
	}
	if code == http.StatusUnauthorized {
		return Result{}.Pass("过期 token 被拒绝(401)").ConfigRefs(refs...)
	}
	return Result{}.Fail(fmt.Sprintf("过期 token 未被拒绝（http=%d, body=%s）", code, truncate(string(body), 120))).ConfigRefs(refs...)
}

// ---------------- auth/protected_matrix ----------------

type authProtectedMatrix struct{ baseChecker }

func (c *authProtectedMatrix) Run(ctx context.Context, env *Env) Result {
	// 每个受保护路由组取一个代表端点 + 两个流式入口
	cases := []struct {
		group string
		path  string
	}{
		{"user", "/api/v1/user/get"},
		{"platform", "/api/v1/platform/clientsstatus"},
		{"client", "/api/v1/client/list"},
		{"server", "/api/v1/server/list"},
		{"frpc", "/api/v1/frpc/update"},
		{"frps", "/api/v1/frps/update"},
		{"proxy", "/api/v1/proxy/list_configs"},
		{"worker", "/api/v1/worker/list"},
		{"wg", "/api/v1/wg/list"},
		{"streamlog", "/api/v1/log"},
		{"pty", "/api/v1/pty/frpp-check-node"},
	}

	var leaked, okList []string
	netErrs := 0
	for _, tc := range cases {
		code, _, _, err := env.PostProto(ctx, tc.path, &pb.CommonRequest{})
		if err != nil {
			netErrs++
			okList = append(okList, fmt.Sprintf("%s %s — 网络错误: %v", tc.group, tc.path, err))
			continue
		}
		if code == http.StatusUnauthorized || code == http.StatusBadRequest {
			okList = append(okList, fmt.Sprintf("%-9s %s — 401 ✓", tc.group, tc.path))
			continue
		}
		leaked = append(leaked, fmt.Sprintf("%-9s %s — http=%d（未拒绝）", tc.group, tc.path, code))
	}

	// REST API 完全不可达属于连通性问题，不能按"拒绝通过"
	if netErrs == len(cases) {
		return Result{}.Fail("REST API 完全不可达，无法验证鉴权矩阵（先解决 env/port_listening）").
			WithDetails(okList...).ConfigRefs("middleware.JWTAuth -> AuthCtx -> RBAC", "biz/master/handler.go")
	}

	if len(leaked) > 0 {
		return Result{}.Fail(fmt.Sprintf("%d 个受保护端点对匿名请求未返回 401", len(leaked))).
			WithDetails(append(leaked, okList...)...).ConfigRefs("middleware.JWTAuth -> AuthCtx -> RBAC", "biz/master/handler.go")
	}
	return Result{}.Pass(fmt.Sprintf("%d 个受保护端点全部拒绝匿名请求", len(cases))).WithDetails(okList...).
		ConfigRefs("middleware.JWTAuth -> AuthCtx -> RBAC", "biz/master/handler.go")
}

// ---------------- auth/frp_plugin_* ----------------

type authFRPPluginReject struct{ baseChecker }

// frp multiuser 插件请求体格式（fatedier/frp pkg/plugin/server）。
type frpPluginRequest struct {
	Version string                `json:"version"`
	Op      string                `json:"op"`
	Content frpPluginLoginContent `json:"content"`
}

type frpPluginLoginContent struct {
	User       string            `json:"user"`
	Metas      map[string]string `json:"metas"`
	ClientAddr string            `json:"client_addr"`
}

type frpPluginResponse struct {
	Reject       bool   `json:"reject"`
	RejectReason string `json:"reject_reason"`
	Unchange     bool   `json:"unchange"`
}

func (c *authFRPPluginReject) Run(ctx context.Context, env *Env) Result {
	refs := []string{"POST /auth（frps multiuser HTTP 插件）", "SERVER_API_PORT"}
	body := frpPluginRequest{
		Version: "0.1.0",
		Op:      "Login",
		Content: frpPluginLoginContent{
			User:       "frpp-check-invalid-user",
			Metas:      map[string]string{"token": "frpp-check-invalid-token"},
			ClientAddr: defs.LocalHost + ":0",
		},
	}
	code, _, respBody, err := env.PostJSON(ctx, "/auth", mustJSON(body))
	if err != nil {
		return Result{}.Fail(fmt.Sprintf("/auth 插件端点不可达: %v", err)).ConfigRefs(refs...)
	}
	resp := &frpPluginResponse{}
	if err := json.Unmarshal(respBody, resp); err != nil {
		return Result{}.Fail(fmt.Sprintf("/auth 响应非 JSON: %v (http=%d)", err, code)).ConfigRefs(refs...)
	}
	if resp.Reject {
		return Result{}.Pass("非法 token 被插件拒绝").WithDetails("reason: " + resp.RejectReason).ConfigRefs(refs...)
	}
	return Result{}.Fail("非法 token 未被拒绝，frps 认证形同虚设").ConfigRefs(refs...)
}

type authFRPPluginAccept struct{ baseChecker }

func (c *authFRPPluginAccept) Run(ctx context.Context, env *Env) Result {
	refs := []string{"POST /auth", "users.token", "MASTER_CACHE_SIZE"}
	db, err := env.DB(ctx)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("数据库不可用，无法取真实用户 token: %v", err)).ConfigRefs(refs...)
	}

	var users []models.User
	if err := db.Model(&models.User{}).Where("token != ''").Limit(1).Find(&users).Error; err != nil || len(users) == 0 {
		return Result{}.Skip("users 表无带 token 的用户").ConfigRefs(refs...)
	}
	u := users[0]

	body := frpPluginRequest{
		Version: "0.1.0",
		Op:      "Login",
		Content: frpPluginLoginContent{
			User:       u.UserName,
			Metas:      map[string]string{"token": u.Token},
			ClientAddr: defs.LocalHost + ":0",
		},
	}
	code, _, respBody, err := env.PostJSON(ctx, "/auth", mustJSON(body))
	if err != nil {
		return Result{}.Fail(fmt.Sprintf("/auth 请求失败: %v", err)).ConfigRefs(refs...)
	}
	resp := &frpPluginResponse{}
	_ = json.Unmarshal(respBody, resp)

	// 正常路径：unchange=true 表示认证通过；reject=false 亦可
	if resp.Unchange || (!resp.Reject && code == http.StatusOK) {
		return Result{}.Pass("合法用户 token 通过插件认证").
			WithDetails(fmt.Sprintf("user=%s http=%d unchange=%v reject=%v", u.UserName, code, resp.Unchange, resp.Reject)).ConfigRefs(refs...)
	}
	return Result{}.Warn(fmt.Sprintf("合法 token 被拒绝（user=%s reason=%s）——检查缓存与 users.token",
		u.UserName, resp.RejectReason)).ConfigRefs(refs...)
}

// ---------------- auth/cert_reject ----------------

type authCertReject struct{ baseChecker }

func (c *authCertReject) Run(ctx context.Context, env *Env) Result {
	refs := []string{"/api/v1/auth/cert", "CLIENT_SECRET"}
	code, _, body, err := env.PostProto(ctx, "/api/v1/auth/cert",
		&pb.GetClientCertRequest{
			ClientType:   pb.ClientType_CLIENT_TYPE_FRPC,
			ClientId:     "frpp-check-invalid",
			ClientSecret: "frpp-check-invalid",
		})
	if err != nil {
		return Result{}.Fail(fmt.Sprintf("cert 接口不可达: %v", err)).ConfigRefs(refs...)
	}
	resp := &pb.GetClientCertResponse{}
	_ = proto.Unmarshal(body, resp)

	if len(resp.GetCert()) > 0 {
		return Result{}.Fail("错误凭证仍签发了客户端证书！").WithDetails(fmt.Sprintf("http=%d", code)).ConfigRefs(refs...)
	}
	if code == http.StatusOK && resp.GetStatus().GetCode() == pb.RespCode_RESP_CODE_SUCCESS {
		return Result{}.Fail("错误凭证返回 SUCCESS 但无证书，行为异常").ConfigRefs(refs...)
	}
	return Result{}.Pass("错误凭证被拒绝签发").WithDetails(fmt.Sprintf("http=%d code=%s", code, resp.GetStatus().GetCode())).ConfigRefs(refs...)
}

// mustJSON 序列化为紧凑 JSON，失败时返回空对象（调用方已在协议层处理失败）。
func mustJSON(v interface{}) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(raw)
}
