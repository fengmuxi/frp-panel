// Package checker 实现 `frpp check` 自动化排查框架。
//
// 设计原则：
//   - 旁路探测：check 以独立进程运行，不侵入 master/client/server 进程内存，
//     进程内状态（如节点注册表）通过 REST API（/api/v1/platform/clientsstatus）与数据库间接推断。
//   - 默认只读：所有 Checker 默认不产生副作用；需要写操作的用例必须
//     env.Opts.Destructive 为 true 才会执行，且结束后自动清理。
//   - 所有端口、路径、环境变量均从 conf.Config / defs 常量读取，不硬编码。
package checker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// jsonUnmarshal 局部别名，避免与 MarshalJSON 方法歧义。
func jsonUnmarshal(data []byte, v interface{}) error { return json.Unmarshal(data, v) }

// Level 表示检查项的严重级别，用于 --fail-on 退出码判定与报告排序。
type Level int

const (
	// LevelInfo 信息级，不影响系统运行，仅记录事实。
	LevelInfo Level = iota
	// LevelMinor 轻微问题：配置不优雅、废弃项、可优化点。
	LevelMinor
	// LevelMajor 重要问题：功能受损、存在风险，需要尽快处理。
	LevelMajor
	// LevelCritical 致命问题：服务不可用或存在严重安全隐患。
	LevelCritical
)

func (l Level) String() string {
	switch l {
	case LevelCritical:
		return "critical"
	case LevelMajor:
		return "major"
	case LevelMinor:
		return "minor"
	case LevelInfo:
		return "info"
	}
	return "unknown"
}

// MarshalJSON 使 JSON 报告中的 severity 输出为可读字符串。
func (l Level) MarshalJSON() ([]byte, error) {
	return []byte(`"` + l.String() + `"`), nil
}

// UnmarshalJSON 支持从字符串解析级别。
func (l *Level) UnmarshalJSON(data []byte) error {
	var s string
	if err := jsonUnmarshal(data, &s); err != nil {
		return err
	}
	parsed, ok := ParseLevel(s)
	if !ok {
		return fmt.Errorf("unknown level %q", s)
	}
	*l = parsed
	return nil
}

// ParseLevel 解析 --fail-on 参数值。
func ParseLevel(s string) (Level, bool) {
	switch s {
	case "info":
		return LevelInfo, true
	case "minor":
		return LevelMinor, true
	case "major":
		return LevelMajor, true
	case "critical":
		return LevelCritical, true
	}
	return LevelInfo, false
}

// Status 表示单次检查的运行结论。
type Status string

const (
	// StatusPass 检查通过。
	StatusPass Status = "pass"
	// StatusFail 检查未通过（配合 Severity 决定退出码）。
	StatusFail Status = "fail"
	// StatusWarn 无法确证失败，但存在需要人工关注的风险点。
	StatusWarn Status = "warn"
	// StatusSkip 因前提不满足（平台不支持 / 缺少凭证 / 未开启 destructive）而跳过。
	StatusSkip Status = "skip"
)

// Result 是单个 Checker 的执行结果。
type Result struct {
	Checker       string        `json:"checker"`
	Group         string        `json:"group"`
	Severity      Level         `json:"severity"`
	Status        Status        `json:"status"`
	Duration      time.Duration `json:"duration"`
	Message       string        `json:"message"`
	Details       []string      `json:"details,omitempty"`
	FixHint       string        `json:"fix_hint,omitempty"`
	ConfigRefList []string      `json:"config_refs,omitempty"` // 相关配置路径 / 环境变量 / 路由
}

// ConfigRefs 附加相关配置路径 / 环境变量 / 路由引用。
func (r Result) ConfigRefs(refs ...string) Result {
	r.ConfigRefList = append(r.ConfigRefList, refs...)
	return r
}

// Fail 构造一个失败结果。
func (r Result) Fail(msg string) Result {
	r.Status = StatusFail
	r.Message = msg
	return r
}

// Pass 构造一个通过结果。
func (r Result) Pass(msg string) Result {
	r.Status = StatusPass
	r.Message = msg
	return r
}

// Warn 构造一个警告结果。
func (r Result) Warn(msg string) Result {
	r.Status = StatusWarn
	r.Message = msg
	return r
}

// Skip 构造一个跳过结果。
func (r Result) Skip(msg string) Result {
	r.Status = StatusSkip
	r.Message = msg
	return r
}

// WithDetails 附加明细行。
func (r Result) WithDetails(lines ...string) Result {
	r.Details = append(r.Details, lines...)
	return r
}

// Checker 是每个检查项必须实现的接口。
//
// 与任务约定一致：Name/Severity/Run/FixHint 四个核心方法；
// 额外补充 Group() 用于子命令分组（env/auth/master/...），
// 由各组的构造函数统一注入，Checker 实现者无需关心。
type Checker interface {
	Name() string
	Group() string
	Severity() Level
	Run(ctx context.Context, env *Env) Result
	FixHint() string
}

// baseChecker 提供公共字段，各组 Checker 通过嵌入复用。
type baseChecker struct {
	name     string
	group    string
	severity Level
	fixHint  string
}

func (b *baseChecker) Name() string    { return b.name }
func (b *baseChecker) Group() string   { return b.group }
func (b *baseChecker) Severity() Level { return b.severity }
func (b *baseChecker) FixHint() string { return b.fixHint }
func (b *baseChecker) String() string  { return b.group + "/" + b.name }
