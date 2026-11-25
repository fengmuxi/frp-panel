package checker

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/fengmuxi/frp-panel/utils/logger"
	"golang.org/x/sync/errgroup"
)

// reportSchemaVersion 是报告 JSON 结构版本，字段发生不兼容变更时递增，
// 供下游消费方（CI、聚合工具）做兼容判断。
const reportSchemaVersion = 1

// RunOptsSnapshot 回显本次运行的参数，便于事后审计；不包含登录凭证。
type RunOptsSnapshot struct {
	Format         string   `json:"format"`
	Out            string   `json:"out,omitempty"`
	FailOn         string   `json:"fail_on"`
	Timeout        string   `json:"timeout"`
	CheckerTimeout string   `json:"checker_timeout"`
	Concurrency    int      `json:"concurrency"`
	Destructive    bool     `json:"destructive"`
	Node           string   `json:"node,omitempty"`
	HasCredentials bool     `json:"has_credentials"`
	Only           []string `json:"only,omitempty"`
	Skip           []string `json:"skip,omitempty"`
}

// RunReport 是一次完整检查的汇总结果。
type RunReport struct {
	SchemaVersion int             `json:"schema_version"`
	Host          string          `json:"host,omitempty"` // 产生报告的主机名（report merge 的节点标识）
	StartedAt     time.Time       `json:"started_at"`
	Duration      time.Duration   `json:"duration"`
	Version       string          `json:"binary_version"`
	Role          string          `json:"detected_role"` // master / client / server / unknown
	Groups        []string        `json:"groups"`
	Opts          RunOptsSnapshot `json:"opts"`
	Results       []Result        `json:"results"`
	Summary       map[string]int  `json:"summary"` // status -> count
	BySeverity    map[string]int  `json:"by_severity"`
	FailExit      bool            `json:"fail_exit"` // 是否触发 --fail-on 退出码
}

// Run 执行指定分组的所有 Checker。
//
// 并发执行由 errgroup 管理（并发度 env.Opts.Concurrency），
// 单项超时由 env.Opts.CheckerTimeout 控制（runOne 内置看门狗，超时项降级为 fail），
// ctx 全程透传。结果按 (group, severity desc, name) 排序，保证输出稳定。
func Run(ctx context.Context, env *Env, groupNames []string) (*RunReport, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, env.Opts.Timeout)
	defer cancel()

	var (
		mu      sync.Mutex
		results []Result
	)

	checkers, err := CollectCheckers(env, groupNames)
	if err != nil {
		return nil, err
	}
	if len(checkers) == 0 {
		return nil, fmt.Errorf("--only/--skip 过滤后没有可运行的检查项，请核对 pattern（支持 group、group/name 或 checker 名）")
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(env.Opts.Concurrency)

	for _, c := range checkers {
		c := c
		g.Go(func() error {
			res := runOne(gctx, env, c)
			mu.Lock()
			results = append(results, res)
			mu.Unlock()
			return nil // 单项失败不中断整体：排查工具应尽可能产出完整报告
		})
	}
	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("checker wait: %w", err)
	}

	sortResults(results)

	report := &RunReport{
		Host:          hostname(),
		SchemaVersion: reportSchemaVersion,
		StartedAt:     start,
		Duration:      time.Since(start),
		Version:       env.Version.GitVersion,
		Role:          detectRole(env),
		Groups:        groupNames,
		Opts: RunOptsSnapshot{
			Format:         env.Opts.Format,
			Out:            env.Opts.Out,
			FailOn:         env.Opts.FailOn.String(),
			Timeout:        env.Opts.Timeout.String(),
			CheckerTimeout: env.Opts.CheckerTimeout.String(),
			Concurrency:    env.Opts.Concurrency,
			Destructive:    env.Opts.Destructive,
			Node:           env.Opts.Node,
			HasCredentials: env.Opts.AdminUser != "",
			Only:           env.Opts.Only,
			Skip:           env.Opts.Skip,
		},
		Results:    results,
		Summary:    map[string]int{},
		BySeverity: map[string]int{},
	}

	failGate, _ := ParseLevel(env.Opts.FailOn.String())
	for _, r := range results {
		report.Summary[string(r.Status)]++
		report.BySeverity[r.Severity.String()]++
		if r.Status == StatusFail && r.Severity >= failGate {
			report.FailExit = true
		}
	}
	return report, nil
}

// runOne 带单项超时看门狗地执行一个 Checker。
//
// Checker 在独立 goroutine 中运行：超时或 panic 时主流程立即得到降级为 fail 的
// Result，不会因单项阻塞拖死整次检查（泄漏的 goroutine 通过缓冲 channel 收尾，
// 不会阻塞）。panic 的 recover 必须位于该 goroutine 内部才能捕获。
func runOne(ctx context.Context, env *Env, c Checker) Result {
	cctx, cancel := context.WithTimeout(ctx, env.Opts.CheckerTimeout)
	defer cancel()

	done := make(chan Result, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				done <- Result{
					Status:  StatusFail,
					Message: fmt.Sprintf("checker panicked: %v", p),
					FixHint: "请附上完整输出并反馈给维护者",
				}
			}
		}()
		done <- c.Run(cctx, env)
	}()

	begin := time.Now()
	select {
	case res := <-done:
		res.Duration = time.Since(begin)
		res.Checker = c.Name()
		res.Group = c.Group()
		res.Severity = c.Severity()
		res.FixHint = c.FixHint()
		logger.Logger(context.Background()).
			WithField("checker", res.Group+"/"+res.Checker).
			WithField("status", string(res.Status)).
			Debugf("checker finished in %s", res.Duration)
		return res
	case <-cctx.Done():
		return Result{
			Checker:  c.Name(),
			Group:    c.Group(),
			Severity: c.Severity(),
			Status:   StatusFail,
			Duration: time.Since(begin),
			Message:  fmt.Sprintf("checker timeout after %s (内部可能阻塞)", env.Opts.CheckerTimeout),
			FixHint:  "请反馈该检查项名称与现场，便于定位阻塞点",
		}
	}
}

func sortResults(rs []Result) {
	sevRank := map[Level]int{LevelCritical: 0, LevelMajor: 1, LevelMinor: 2, LevelInfo: 3}
	sort.Slice(rs, func(i, j int) bool {
		if rs[i].Group != rs[j].Group {
			return rs[i].Group < rs[j].Group
		}
		if si, sj := sevRank[rs[i].Severity], sevRank[rs[j].Severity]; si != sj {
			return si < sj
		}
		return rs[i].Checker < rs[j].Checker
	})
}

// hostname 返回主机名（report merge 的节点标识），失败为空串。
func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
}

// detectRole 依据当前配置推断本机部署角色，用于报告头部展示。
// 注意 DB_TYPE/DB_DSN 在 conf 中带默认值（结构体字段恒非空），
// 不能以字段判空区分"真的配了 master"，这里改用环境变量是否显式设置
// （含 .env 加载结果，与 master 的配置来源一致）。
func detectRole(env *Env) string {
	switch {
	case env.Cfg.Client.RPCUrl != "" && env.Cfg.Client.ID != "":
		return "client/server node"
	case os.Getenv("DB_TYPE") != "" || os.Getenv("DB_DSN") != "":
		return "master"
	default:
		return "unknown"
	}
}
