//go:build windows

package workerd

import "os/exec"

// windows 无 POSIX 权限模型，workerd 维持与 frpp 相同的运行身份（安全审计决策点 F）。

func applyLowerPrivilege(cmd *exec.Cmd) {}

func chownTree(dir string) {}
