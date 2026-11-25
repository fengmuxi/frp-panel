//go:build !linux || android

package workerd

import "syscall"

// applyPdeathsig 非 Linux 平台无 Pdeathsig 字段，no-op。
// android 虽基于 Linux 内核，但其 syscall.SysProcAttr 独立定义且不含该字段，
// 交叉编译（GOOS=android）必须走此空实现，见 pdeathsig_linux.go。
func applyPdeathsig(a *syscall.SysProcAttr) {
}
