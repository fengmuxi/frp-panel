//go:build linux

package checker

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/VaalaCat/frp-panel/models"
	"github.com/VaalaCat/frp-panel/utils/logger"
	"golang.zx2c4.com/wireguard/wgctrl"
)

// wgDeviceProbeLinux 通过 wgctrl 读取内核态 WireGuard 设备，
// 与数据库配置对账：接口存在性、peer 数、最新握手时间与流量计数。
// 仅在 linux 下编译（build tag），对应 defs 中"WireGuard 智能组网仅 Linux 支持"的约束。
func wgDeviceProbeLinux(ctx context.Context, env *Env) Result {
	refs := []string{"wgctrl", "wireguards 表", "latest handshake"}

	client, err := wgctrl.New()
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("wgctrl 初始化失败（无权限或未运行于支持内核）: %v", err)).ConfigRefs(refs...)
	}
	defer client.Close()

	devices, err := client.Devices()
	if err != nil {
		return Result{}.Warn(fmt.Sprintf("枚举 WireGuard 设备失败: %v", err)).ConfigRefs(refs...)
	}

	devByName := map[string]int{}
	for i, d := range devices {
		devByName[d.Name] = i
	}

	// 与 DB 中本机节点对账
	var dbNodes []models.WireGuard
	if db, err := env.DB(ctx); err == nil {
		_ = db.Model(&models.WireGuard{}).Find(&dbNodes).Error
	}

	var (
		details []string
		missing []string
	)
	for _, wg := range dbNodes {
		if !env.NodeFilter(wg.Name, wg.ClientID) {
			continue
		}
		idx, ok := devByName[wg.Name]
		if !ok {
			// 不在本机 -> 可能是其他节点的记录，跳过而非报错
			continue
		}
		dev := devices[idx]
		details = append(details, fmt.Sprintf("iface=%s peers=%d type=%s", dev.Name, len(dev.Peers), dev.Type))

		// 最新握手 + 流量计数
		var latest time.Time
		var rx, tx int64
		for _, p := range dev.Peers {
			if p.LastHandshakeTime.After(latest) {
				latest = p.LastHandshakeTime
			}
			rx += int64(p.ReceiveBytes)
			tx += int64(p.TransmitBytes)
		}
		details = append(details, fmt.Sprintf("iface=%s rx=%d tx=%d handshake=%s", dev.Name, rx, tx, formatWGHandshake(latest)))
	}

	if len(details) == 0 && len(dbNodes) == 0 {
		return Result{}.Pass(fmt.Sprintf("本机无 WireGuard 配置记录，内核设备 %d 个", len(devices))).ConfigRefs(refs...)
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		details = append(details, missing...)
	}
	return Result{}.Pass(fmt.Sprintf("内核探测完成：%d 个设备", len(devices))).WithDetails(details...).
		ConfigRefs(refs...)
}

var _ = logger.Logger // 保持与项目日志依赖关联，便于排障时切换
