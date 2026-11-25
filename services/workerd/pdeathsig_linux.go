//go:build linux && !android

package workerd

import "syscall"

// applyPdeathsig 设置父进程死亡信号：frpp 被杀死时子进程 workerd 随之退出。
// Pdeathsig 是 Linux 专属字段（android 的 syscall.SysProcAttr 独立定义、无此字段，
// darwin 同样没有），非 Linux 平台走 pdeathsig_other.go 的空实现。
func applyPdeathsig(a *syscall.SysProcAttr) {
	a.Pdeathsig = syscall.SIGKILL
}
