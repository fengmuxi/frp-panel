package checker

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/VaalaCat/frp-panel/utils/logger"
	"golang.org/x/sync/errgroup"
)

// RunReport 是一次完整检查的汇总结果。
type RunReport struct {
	StartedAt  time.Time         `json:"started_at"`
	Duration   time.Duration     `json:"duration"`
	Version    string            `json:"binary_version"`
	Role       string            `json:"detected_role"` // master / client / server / unknown
	Groups     []string          `json:"groups"`
	Results    []Result          `json:"results"`
	Summary    map[string]int    `json:"summary"` // status -> count
	BySeverity map[string]int    `json:"by_severity"`
	FailExit   bool              `json:"fail_exit"`               // 是否触发 --fail-on 退出码
	Skipped    map[string]string `json:"skipped_notes,omitempty"` // 组 -> 跳过原因
}

// Run 执行指定分组的所有 Checker。
//
// 并发执行由 errgroup 管理（并发度 env.Opts.Concurrency），
// 单项超时由 env.Opts.CheckerTimeout 控制，ctx 全程透传。
// 结果按 (group, severity desc, name) 排序，保证输出稳定。
func Run(ctx context.Context, env *Env, groupNames []string) (*RunReport, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, env.Opts.Timeout)
	defer cancel()

	var (
		mu      sync.Mutex
		results []Result
	)

	checkers, skipped, err := CollectCheckers(env, groupNames)
	if err != nil {
		return nil, err
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

	// 组级跳过说明也并入结果，保证报告完整可读
	for group, note := range skipped {
		results = append(results, Result{
			Checker:  group + "/(group)",
			Group:    group,
			Severity: LevelInfo,
			Status:   StatusSkip,
			Message:  note,
		})
	}

	sortResults(results)

	report := &RunReport{
		StartedAt:  start,
		Duration:   time.Since(start),
		Version:    env.Version.GitVersion,
		Role:       detectRole(env),
		Groups:     groupNames,
		Results:    results,
		Summary:    map[string]int{},
		BySeverity: map[string]int{},
		Skipped:    skipped,
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

// runOne 带单项超时与 panic 防护地执行一个 Checker。
func runOne(ctx context.Context, env *Env, c Checker) (res Result) {
	defer func() {
		if p := recover(); p != nil {
			res = Result{
				Checker:  c.Name(),
				Group:    c.Group(),
				Severity: c.Severity(),
				Status:   StatusFail,
				Message:  fmt.Sprintf("checker panicked: %v", p),
				FixHint:  "请附上完整输出并反馈给维护者",
			}
		}
	}()

	cctx, cancel := context.WithTimeout(ctx, env.Opts.CheckerTimeout)
	defer cancel()

	begin := time.Now()
	res = c.Run(cctx, env)
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

// detectRole 依据当前配置推断本机部署角色，用于报告头部展示。
func detectRole(env *Env) string {
	switch {
	case env.Cfg.Client.RPCUrl != "" && env.Cfg.Client.ID != "":
		return "client/server node"
	case env.Cfg.DB.DSN != "":
		return "master"
	default:
		return "unknown"
	}
}
