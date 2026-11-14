package checker

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/VaalaCat/frp-panel/conf"
	"github.com/VaalaCat/frp-panel/pb"
	"github.com/VaalaCat/frp-panel/utils"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
)

// Options 承载 `frpp check` 的命令行参数。
type Options struct {
	Format          string        // text | json | markdown
	Out             string        // 报告输出文件路径；空则仅打印终端
	FailOn          Level         // 达到该级别的 Fail 时退出码非 0
	Timeout         time.Duration // 全局超时
	CheckerTimeout  time.Duration // 单项超时
	Concurrency     int           // Checker 并发度
	Destructive     bool          // 允许产生副作用（用例结束后自动清理）
	Node            string        // 定向节点过滤（wg/client/server 组使用）
	AdminUser       string        // 登录用户名（用于需要 JWT 的检查）
	AdminPassword   string        // 登录密码
	PerfDuration    time.Duration // perf 压测时长
	PerfConcurrency int           // perf 压测并发
	NoColor         bool          // 关闭终端彩色输出
	List            bool          // 仅列出 Checker 清单
}

// Env 是 Checker 的运行环境：配置、HTTP 客户端、懒加载数据库连接与登录态。
// 每个 `frpp check` 进程创建一个实例；其中的懒加载字段以 sync.Once 保护，
// 不存在包级可变状态。
type Env struct {
	Cfg     conf.Config
	Version *conf.VersionInfo
	Opts    Options

	httpClient *http.Client

	dbOnce sync.Once
	db     *gorm.DB
	dbErr  error

	loginOnce sync.Once
	token     string
	loginErr  error
}

// NewEnv 创建检查环境。cfg 应来自 conf.NewConfig()（已按
// .env -> /etc/frpp/.env -> 环境变量 的顺序加载）。
func NewEnv(cfg conf.Config, opts Options) *Env {
	return &Env{
		Cfg:     cfg,
		Version: conf.GetVersion(),
		Opts:    opts,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig:   &tls.Config{InsecureSkipVerify: cfg.Client.TLSInsecureSkipVerify},
				ForceAttemptHTTP2: true,
			},
		},
	}
}

// APIBase 返回 Master REST API 前缀（不含尾斜杠），复用项目 conf.GetAPIURL：
// 优先 CLIENT_API_URL，否则由 MASTER_API_SCHEME/HOST/PORT 拼接。
func (e *Env) APIBase() string {
	return conf.GetAPIURL(e.Cfg)
}

// HasCredentials 判断是否提供了登录凭证（用于需要 JWT 的检查项）。
func (e *Env) HasCredentials() bool {
	return e.Opts.AdminUser != "" && e.Opts.AdminPassword != ""
}

// Login 使用提供的凭证登录 Master 并缓存 JWT。
// 复用项目登录协议：POST /api/v1/auth/login（protobuf）。
func (e *Env) Login(ctx context.Context) (string, error) {
	e.loginOnce.Do(func() {
		if !e.HasCredentials() {
			e.loginErr = fmt.Errorf("no credentials provided (use --login-user/--login-password)")
			return
		}
		var resp pb.LoginResponse
		status, _, body, err := e.PostProto(ctx, "/api/v1/auth/login",
			&pb.LoginRequest{
				Username: proto.String(e.Opts.AdminUser),
				Password: proto.String(e.Opts.AdminPassword),
			})
		if err != nil {
			e.loginErr = fmt.Errorf("login request failed: %w", err)
			return
		}
		if status != http.StatusOK {
			e.loginErr = fmt.Errorf("login http status %d: %s", status, truncate(string(body), 200))
			return
		}
		if err = proto.Unmarshal(body, &resp); err != nil {
			e.loginErr = fmt.Errorf("decode login response: %w", err)
			return
		}
		if resp.GetStatus().GetCode() != pb.RespCode_RESP_CODE_SUCCESS {
			e.loginErr = fmt.Errorf("login rejected: %s", resp.GetStatus().GetMessage())
			return
		}
		e.token = resp.GetToken()
	})
	return e.token, e.loginErr
}

// Token 返回缓存的 JWT（可能为空），供需要鉴权的检查项使用。
func (e *Env) Token(ctx context.Context) string {
	t, _ := e.Login(ctx)
	return t
}

// DB 懒加载数据库连接（只读用途：框架内所有查询均为 SELECT）。
// 连接参数完全来自 DB_TYPE / DB_DSN，与 master 的打开方式保持一致。
func (e *Env) DB(ctx context.Context) (*gorm.DB, error) {
	e.dbOnce.Do(func() {
		e.db, e.dbErr = openDB(e.Cfg)
	})
	if e.dbErr != nil {
		return nil, e.dbErr
	}
	return e.db.WithContext(ctx), nil
}

// HasDB 报告数据库是否已成功打开。
func (e *Env) HasDB(ctx context.Context) bool {
	_, err := e.DB(ctx)
	return err == nil
}

// NodeFilter 命中判断：未指定 --node 时始终命中；指定时按
// client id / server id / interface name 匹配。
func (e *Env) NodeFilter(ids ...string) bool {
	if e.Opts.Node == "" {
		return true
	}
	for _, id := range ids {
		if id == e.Opts.Node {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// sha1Of 暴露项目 utils 的 SHA1，供 JWT secret 校验使用。
var sha1Of = utils.SHA1
