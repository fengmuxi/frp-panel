package checker

import (
	"errors"
	"fmt"
)

// GroupNames 按子命令顺序列出所有检查分组。
var GroupNames = []string{"env", "auth", "master", "server", "client", "worker", "wg", "webui", "data", "perf"}

// groupBuilder 返回某分组的全部 Checker 实例。
// 每次调用都新建实例，避免共享可变状态。
type groupBuilder func(env *Env) ([]Checker, error)

var registry = map[string]groupBuilder{
	"env":    buildEnvGroup,
	"auth":   buildAuthGroup,
	"master": buildMasterGroup,
	"server": buildServerGroup,
	"client": buildClientGroup,
	"worker": buildWorkerGroup,
	"wg":     buildWireGuardGroup,
	"webui":  buildWebUIGroup,
	"data":   buildDataGroup,
	"perf":   buildPerfGroup,
}

// CollectCheckers 汇总指定分组的 Checker。
// 分组不存在或为空时返回错误，避免用户敲错命令却得到"全绿"报告。
func CollectCheckers(env *Env, groups []string) ([]Checker, map[string]string, error) {
	var (
		all     []Checker
		skipped = map[string]string{}
	)
	seen := map[string]bool{}
	for _, name := range groups {
		if seen[name] {
			continue
		}
		seen[name] = true
		build, ok := registry[name]
		if !ok {
			return nil, nil, fmt.Errorf("unknown check group %q, valid: %v", name, GroupNames)
		}
		cs, err := build(env)
		if err != nil {
			return nil, nil, fmt.Errorf("build group %s: %w", name, err)
		}
		all = append(all, cs...)
	}
	if len(all) == 0 {
		return nil, nil, errors.New("no checker collected")
	}
	return all, skipped, nil
}

// AllCheckerNames 返回 (group, name) 清单，供 --list 输出。
func AllCheckerNames() []string {
	env := &Env{}
	var names []string
	for _, g := range GroupNames {
		build, ok := registry[g]
		if !ok {
			continue
		}
		cs, err := build(env)
		if err != nil {
			continue
		}
		for _, c := range cs {
			names = append(names, fmt.Sprintf("%-8s %-32s %-8s %s", g, c.Name(), c.Severity(), c.FixHint()))
		}
	}
	return names
}
