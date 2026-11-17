package checker

import (
	"context"
	"testing"
	"time"

	"github.com/VaalaCat/frp-panel/conf"
	"github.com/stretchr/testify/assert"
)

// funcChecker 用注入的函数实现 Checker 接口，便于构造各种异常场景。
type funcChecker struct {
	baseChecker
	run func(ctx context.Context, env *Env) Result
}

func (c *funcChecker) Run(ctx context.Context, env *Env) Result {
	return c.run(ctx, env)
}

func newTestEnv() *Env {
	return &Env{
		Cfg:     conf.Config{},
		Version: &conf.VersionInfo{GitVersion: "test"},
		Opts: Options{
			Timeout:        5 * time.Second,
			CheckerTimeout: 50 * time.Millisecond,
			Concurrency:    4,
			FailOn:         LevelMajor,
			Format:         "text",
		},
	}
}

// panic 的 Checker 应被框架捕获并降级为 fail，而不是拖垮整个检查进程。
func TestRunOnePanicProtection(t *testing.T) {
	c := &funcChecker{baseChecker: baseChecker{name: "panic_item", group: "data", severity: LevelMinor, fixHint: "feedback"},
		run: func(ctx context.Context, env *Env) Result {
			panic("boom")
		}}

	res := runOne(context.Background(), newTestEnv(), c)
	assert.Equal(t, StatusFail, res.Status)
	assert.Contains(t, res.Message, "checker panicked: boom")
	assert.Equal(t, "data", res.Group)
	assert.Equal(t, "panic_item", res.Checker)
	assert.Equal(t, "feedback", res.FixHint)
}

// 忽略 ctx 的阻塞 Checker 超时后应被看门狗降级为 fail，且主流程不被拖死。
func TestRunOneTimeout(t *testing.T) {
	c := &funcChecker{baseChecker: baseChecker{name: "hang_item", group: "data", severity: LevelMajor},
		run: func(ctx context.Context, env *Env) Result {
			time.Sleep(300 * time.Millisecond) // 故意不感知 ctx 取消
			return Result{}.Pass("never reached")
		}}

	env := newTestEnv()
	env.Opts.CheckerTimeout = 20 * time.Millisecond
	begin := time.Now()
	res := runOne(context.Background(), env, c)
	assert.Less(t, time.Since(begin), 200*time.Millisecond, "看门狗应在超时后立即返回")
	assert.Equal(t, StatusFail, res.Status)
	assert.Contains(t, res.Message, "timeout")
}

func TestRunOneNormal(t *testing.T) {
	c := &funcChecker{baseChecker: baseChecker{name: "ok_item", group: "env", severity: LevelInfo},
		run: func(ctx context.Context, env *Env) Result {
			return Result{}.Pass("all good")
		}}

	res := runOne(context.Background(), newTestEnv(), c)
	assert.Equal(t, StatusPass, res.Status)
	assert.Equal(t, "env", res.Group)
	assert.Equal(t, "ok_item", res.Checker)
	assert.Equal(t, LevelInfo, res.Severity)
}

func newTestGroup(name string, severity Level, status Status) []Checker {
	return []Checker{
		&funcChecker{baseChecker: baseChecker{name: name, group: "testgroup", severity: severity},
			run: func(ctx context.Context, env *Env) Result {
				return Result{Status: status, Message: "test"}
			}},
	}
}

// --fail-on 门控：达到门槛的 fail 触发退出码；warn 与低于门槛的 fail 不触发。
func TestRunFailGate(t *testing.T) {
	registry["testgroup"] = func(env *Env) ([]Checker, error) {
		return newTestGroup("major_fail", LevelMajor, StatusFail), nil
	}
	defer delete(registry, "testgroup")

	env := newTestEnv()
	env.Opts.FailOn = LevelMajor
	report, err := Run(context.Background(), env, []string{"testgroup"})
	assert.NoError(t, err)
	assert.True(t, report.FailExit)

	env = newTestEnv()
	env.Opts.FailOn = LevelCritical // major 低于 critical 门槛
	report, err = Run(context.Background(), env, []string{"testgroup"})
	assert.NoError(t, err)
	assert.False(t, report.FailExit)

	registry["testgroup"] = func(env *Env) ([]Checker, error) {
		return newTestGroup("minor_warn", LevelMinor, StatusWarn), nil
	}
	env = newTestEnv()
	report, err = Run(context.Background(), env, []string{"testgroup"})
	assert.NoError(t, err)
	assert.False(t, report.FailExit)
}

// Run 输出结构：schema 版本、参数回显（不含凭证）、汇总计数。
func TestRunReportStructure(t *testing.T) {
	registry["testgroup"] = func(env *Env) ([]Checker, error) {
		return newTestGroup("struct_item", LevelMinor, StatusPass), nil
	}
	defer delete(registry, "testgroup")

	env := newTestEnv()
	env.Opts.Node = "node-a"
	env.Opts.Destructive = true
	env.Opts.AdminUser = "admin"
	env.Opts.AdminPassword = "should-not-appear"
	report, err := Run(context.Background(), env, []string{"testgroup"})
	assert.NoError(t, err)

	assert.Equal(t, reportSchemaVersion, report.SchemaVersion)
	assert.Equal(t, "test", report.Version)
	assert.Equal(t, "testgroup", report.Groups[0])
	assert.Equal(t, "node-a", report.Opts.Node)
	assert.True(t, report.Opts.Destructive)
	assert.True(t, report.Opts.HasCredentials)
	assert.Equal(t, map[string]int{"pass": 1}, report.Summary)
	assert.Equal(t, 1, len(report.Results))
}

// 结果排序稳定性：组名升序 → 组内 severity 降级序（critical 最前）→ 名称升序。
func TestSortResults(t *testing.T) {
	rs := []Result{
		{Group: "data", Checker: "b", Severity: LevelMinor},
		{Group: "env", Checker: "a", Severity: LevelInfo},
		{Group: "data", Checker: "a", Severity: LevelMajor},
		{Group: "data", Checker: "c", Severity: LevelCritical},
	}
	sortResults(rs)
	assert.Equal(t, []string{"c", "a", "b", "a"}, []string{
		rs[0].Checker, rs[1].Checker, rs[2].Checker, rs[3].Checker})
}
