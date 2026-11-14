//go:build !linux

package checker

import (
	"context"
	"fmt"
	"runtime"
)

// wgDeviceProbeLinux 在非 linux 平台不可用 wgctrl 内核探测，
// 由 group_wg.go 的 wgDeviceProbe 检查器先行按 runtime.GOOS 分流后不会调用本函数；
// 保留实现仅为满足跨平台编译完整性。
func wgDeviceProbeLinux(ctx context.Context, env *Env) Result {
	return Result{}.Skip(fmt.Sprintf("wgctrl 内核探测仅支持 linux，当前平台 %s", runtime.GOOS))
}
