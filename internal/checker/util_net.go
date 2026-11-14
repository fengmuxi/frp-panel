package checker

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"time"
)

// CanDial 探测 TCP host:port 是否可建立连接（只读探测，立即关闭）。
func CanDial(ctx context.Context, host string, port int) (time.Duration, error) {
	if port <= 0 || port > 65535 {
		return 0, fmt.Errorf("invalid port %d", port)
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	start := time.Now()
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	elapsed := time.Since(start)
	if err != nil {
		return elapsed, fmt.Errorf("dial %s: %w", addr, err)
	}
	_ = conn.Close()
	return elapsed, nil
}

// IsPublicIP 判断是否公网地址（用于 wg relay 约束校验）：
// 排除回环、私网（RFC1918）、链路本地、CGNAT(100.64/10) 与未指定地址。
// host 允许传入域名 —— 域名先解析，任一地址为公网即视为公网。
func IsPublicIP(ctx context.Context, host string) bool {
	if host == "" {
		return false
	}
	addrs := []netip.Addr{}
	if a, err := netip.ParseAddr(host); err == nil {
		addrs = append(addrs, a.Unmap())
	} else {
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil || len(ips) == 0 {
			return false
		}
		for _, ip := range ips {
			if a, ok := netip.AddrFromSlice(ip.IP); ok {
				addrs = append(addrs, a.Unmap())
			}
		}
	}
	for _, a := range addrs {
		if a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() || a.IsUnspecified() {
			continue
		}
		// CGNAT 100.64.0.0/10
		if a.Is4() && a.As4()[0] == 100 && a.As4()[1] >= 64 && a.As4()[1] < 128 {
			continue
		}
		return true
	}
	return false
}

// FormatDuration 以人类可读格式输出耗时。
func FormatDuration(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return d.String()
	case d < time.Second:
		return fmt.Sprintf("%.1fms", float64(d)/float64(time.Millisecond))
	default:
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
}
