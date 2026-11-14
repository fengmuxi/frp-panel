package checker

import (
	"context"
	"fmt"
	"net/netip"
	"runtime"
	"time"

	"github.com/VaalaCat/frp-panel/models"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func buildWireGuardGroup(env *Env) ([]Checker, error) {
	return []Checker{
		&wgNetworksValid{baseChecker{name: "networks_valid", group: "wg", severity: LevelMajor,
			fixHint: "network CIDR 必须包含全部节点 LocalAddress 且互不冲突；在面板中调整网络或节点地址"}},
		&wgTopologyIntegrity{baseChecker{name: "topology_integrity", group: "wg", severity: LevelMajor,
			fixHint: "wireguard_links 的两端节点必须属于同一 network，ToEndpoint 必须存在；断链会导致路由规划失败"}},
		&wgRelayConstraint{baseChecker{name: "relay_constraint", group: "wg", severity: LevelMajor,
			fixHint: "组网至少需要一个带公网 IP 的节点承担 relay；为其补充公网 Endpoint 或公网直连能力"}},
		&wgKeyParse{baseChecker{name: "key_parse", group: "wg", severity: LevelCritical,
			fixHint: "PrivateKey 必须是合法的 WireGuard base64 密钥；在面板中重新生成"}},
		&wgListenPorts{baseChecker{name: "listen_ports", group: "wg", severity: LevelMajor,
			fixHint: "listen_port / ws_listen_port 在节点内必须唯一（DB 有唯一索引，此处检查跨节点冲突）"}},
		&wgDeviceProbe{baseChecker{name: "device_probe", group: "wg", severity: LevelMajor,
			fixHint: "设备级探测依赖 wgctrl 内核接口，仅 Linux 可用；其他平台使用 USE_GVISOR_NET 用户态实现，由运行时日志确认握手状态"}},
	}, nil
}

// loadWGTopology 一次性读出组网相关的四张表（只读）。
type wgTopology struct {
	Networks   []models.Network
	WireGuards []models.WireGuard
	Endpoints  []models.Endpoint
	Links      []models.WireGuardLink
}

func loadWGTopology(ctx context.Context, env *Env) (*wgTopology, error) {
	db, err := env.DB(ctx)
	if err != nil {
		return nil, err
	}
	topo := &wgTopology{}
	if err := db.Model(&models.Network{}).Find(&topo.Networks).Error; err != nil {
		return nil, fmt.Errorf("query networks: %w", err)
	}
	if err := db.Model(&models.WireGuard{}).Find(&topo.WireGuards).Error; err != nil {
		return nil, fmt.Errorf("query wireguards: %w", err)
	}
	if err := db.Model(&models.Endpoint{}).Find(&topo.Endpoints).Error; err != nil {
		return nil, fmt.Errorf("query endpoints: %w", err)
	}
	if err := db.Model(&models.WireGuardLink{}).Find(&topo.Links).Error; err != nil {
		return nil, fmt.Errorf("query wireguard_links: %w", err)
	}
	return topo, nil
}

// ---------------- wg/networks_valid ----------------

type wgNetworksValid struct{ baseChecker }

func (c *wgNetworksValid) Run(ctx context.Context, env *Env) Result {
	refs := []string{"networks.cidr", "wireguards.local_address"}
	topo, err := loadWGTopology(ctx, env)
	if err != nil {
		if env.HasDB(ctx) {
			return Result{}.Fail(fmt.Sprintf("读取组网表失败: %v", err))
		}
		return Result{}.Skip(fmt.Sprintf("数据库不可用: %v", err))
	}
	if len(topo.Networks) == 0 && len(topo.WireGuards) == 0 {
		return Result{}.Skip("未配置 WireGuard 网络")
	}

	var problems []string
	for _, n := range topo.Networks {
		prefix, err := netip.ParsePrefix(n.CIDR)
		if err != nil {
			problems = append(problems, fmt.Sprintf("network=%s CIDR 非法: %q", n.Name, n.CIDR))
			continue
		}
		used := map[string]uint{}
		for _, wg := range topo.WireGuards {
			if wg.NetworkID != n.ID {
				continue
			}
			addr, _, err := models.ParseIPOrCIDRWithNetip(wg.LocalAddress)
			if err != nil {
				problems = append(problems, fmt.Sprintf("network=%s node=%s 地址非法: %q", n.Name, wg.Name, wg.LocalAddress))
				continue
			}
			if !prefix.Contains(addr) {
				problems = append(problems, fmt.Sprintf("network=%s node=%s 地址 %s 不在 %s 内", n.Name, wg.Name, addr, prefix))
			}
			if prev, ok := used[addr.String()]; ok {
				problems = append(problems, fmt.Sprintf("network=%s 节点 %d 与 %d 的 VPN 地址冲突: %s", n.Name, prev, wg.ID, addr))
			}
			used[addr.String()] = wg.ID
		}
	}
	if len(problems) > 0 {
		return Result{}.Fail(fmt.Sprintf("%d 处组网地址问题", len(problems))).WithDetails(problems...).ConfigRefs(refs...)
	}
	return Result{}.Pass(fmt.Sprintf("%d 个网络、%d 个节点的地址规划合法", len(topo.Networks), len(topo.WireGuards))).ConfigRefs(refs...)
}

// ---------------- wg/topology_integrity ----------------

type wgTopologyIntegrity struct{ baseChecker }

func (c *wgTopologyIntegrity) Run(ctx context.Context, env *Env) Result {
	refs := []string{"wireguard_links", "endpoints", "GetNetworkTopology(spf)"}
	topo, err := loadWGTopology(ctx, env)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("数据库不可用: %v", err))
	}
	if len(topo.Links) == 0 {
		return Result{}.Skip("wireguard_links 为空（组网未配置链路）")
	}

	wgNet := map[uint]uint{}
	for _, wg := range topo.WireGuards {
		wgNet[wg.ID] = wg.NetworkID
	}
	epExists := map[uint]bool{}
	for _, ep := range topo.Endpoints {
		epExists[ep.ID] = true
	}

	var problems []string
	directed := map[string]int{} // networkID -> link 数（双向成对计数用）
	for _, l := range topo.Links {
		fromNet, fromOK := wgNet[l.FromWireGuardID]
		toNet, toOK := wgNet[l.ToWireGuardID]
		if !fromOK || !toOK {
			problems = append(problems, fmt.Sprintf("link=%d 引用了不存在的 WireGuard 节点（from=%d to=%d）", l.ID, l.FromWireGuardID, l.ToWireGuardID))
			continue
		}
		if fromNet != toNet {
			problems = append(problems, fmt.Sprintf("link=%d 两端属于不同 network（%d vs %d）", l.ID, fromNet, toNet))
		}
		if l.ToEndpointID != 0 && !epExists[l.ToEndpointID] {
			problems = append(problems, fmt.Sprintf("link=%d 引用的 endpoint=%d 不存在", l.ID, l.ToEndpointID))
		}
		directed[fmt.Sprint(fromNet)]++
	}

	var details []string
	for netID, cnt := range directed {
		details = append(details, fmt.Sprintf("network=%s: %d 条有向链路", netID, cnt))
	}
	if len(problems) > 0 {
		return Result{}.Fail(fmt.Sprintf("%d 处链路拓扑问题", len(problems))).WithDetails(append(problems, details...)...).ConfigRefs(refs...)
	}
	return Result{}.Pass("链路拓扑引用完整").WithDetails(details...).ConfigRefs(refs...)
}

// ---------------- wg/relay_constraint ----------------

type wgRelayConstraint struct{ baseChecker }

func (c *wgRelayConstraint) Run(ctx context.Context, env *Env) Result {
	refs := []string{"endpoints.host", "公网 relay 约束"}
	topo, err := loadWGTopology(ctx, env)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("数据库不可用: %v", err))
	}
	if len(topo.Networks) == 0 {
		return Result{}.Skip("未配置 WireGuard 网络")
	}

	epByWG := map[uint][]models.Endpoint{}
	for _, ep := range topo.Endpoints {
		epByWG[ep.WireGuardID] = append(epByWG[ep.WireGuardID], ep)
	}

	var problems, details []string
	for _, n := range topo.Networks {
		hasPublic := false
		var epLines []string
		for _, wg := range topo.WireGuards {
			if wg.NetworkID != n.ID {
				continue
			}
			for _, ep := range epByWG[wg.ID] {
				public := IsPublicIP(ctx, ep.Host)
				epLines = append(epLines, fmt.Sprintf("node=%s endpoint=%s:%d type=%s 公网=%v", wg.Name, ep.Host, ep.Port, ep.Type, public))
				if public {
					hasPublic = true
				}
			}
		}
		details = append(details, epLines...)
		if !hasPublic && len(topo.WireGuards) >= 2 {
			problems = append(problems, fmt.Sprintf("network=%s 无任何公网 Endpoint 节点，跨 NAT 组网将无法打通（需要公网 relay）", n.Name))
		}
	}
	if len(problems) > 0 {
		return Result{}.Fail(fmt.Sprintf("%d 个网络缺少公网 relay 节点", len(problems))).WithDetails(append(problems, details...)...).ConfigRefs(refs...)
	}
	return Result{}.Pass("公网 relay 约束满足（或当前规模无需 relay）").WithDetails(details...).ConfigRefs(refs...)
}

// ---------------- wg/key_parse ----------------

type wgKeyParse struct{ baseChecker }

func (c *wgKeyParse) Run(ctx context.Context, env *Env) Result {
	refs := []string{"wireguards.private_key", "wgtypes.ParseKey"}
	topo, err := loadWGTopology(ctx, env)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("数据库不可用: %v", err))
	}
	if len(topo.WireGuards) == 0 {
		return Result{}.Skip("未配置 WireGuard 节点")
	}

	var bad []string
	for _, wg := range topo.WireGuards {
		if _, err := wgtypes.ParseKey(wg.PrivateKey); err != nil {
			bad = append(bad, fmt.Sprintf("node=%s(id=%d) 私钥非法: %v", wg.Name, wg.ID, err))
		}
	}
	if len(bad) > 0 {
		return Result{}.Fail(fmt.Sprintf("%d/%d 个节点私钥非法", len(bad), len(topo.WireGuards))).WithDetails(bad...).ConfigRefs(refs...)
	}
	return Result{}.Pass(fmt.Sprintf("%d 个节点私钥均合法", len(topo.WireGuards))).ConfigRefs(refs...)
}

// ---------------- wg/listen_ports ----------------

type wgListenPorts struct{ baseChecker }

func (c *wgListenPorts) Run(ctx context.Context, env *Env) Result {
	refs := []string{"wireguards.listen_port", "wireguards.ws_listen_port"}
	topo, err := loadWGTopology(ctx, env)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("数据库不可用: %v", err))
	}
	if len(topo.WireGuards) == 0 {
		return Result{}.Skip("未配置 WireGuard 节点")
	}

	type key struct {
		port uint32
		what string
	}
	seen := map[key]string{}
	var conflicts []string
	for _, wg := range topo.WireGuards {
		for _, p := range []struct {
			port uint32
			what string
		}{{wg.ListenPort, "listen_port"}, {wg.WsListenPort, "ws_listen_port"}} {
			if p.port == 0 {
				continue
			}
			k := key{p.port, p.what}
			if owner, ok := seen[k]; ok {
				conflicts = append(conflicts, fmt.Sprintf("%s=%d 冲突：%s 与 %s", p.what, p.port, owner, wg.Name))
			} else {
				seen[k] = wg.Name
			}
		}
	}
	if len(conflicts) > 0 {
		return Result{}.Fail(fmt.Sprintf("%d 处监听端口冲突", len(conflicts))).WithDetails(conflicts...).ConfigRefs(refs...)
	}
	return Result{}.Pass(fmt.Sprintf("%d 个节点的 UDP/WS 监听端口无冲突", len(topo.WireGuards))).ConfigRefs(refs...)
}

// ---------------- wg/device_probe（跨平台入口） ----------------

type wgDeviceProbe struct{ baseChecker }

func (c *wgDeviceProbe) Run(ctx context.Context, env *Env) Result {
	refs := []string{"wgctrl(linux 内核接口)", "USE_GVISOR_NET"}
	switch runtime.GOOS {
	case "linux":
		return wgDeviceProbeLinux(ctx, env)
	default:
		return Result{}.Skip(fmt.Sprintf("当前平台 %s 不支持 wgctrl 内核探测；组网依赖 APP_USE_GVISOR_NET=%v 的用户态实现，握手状态请通过面板 /api/v1/wg/runtime/get 查看", runtime.GOOS, env.Cfg.App.UseGvisorNet)).
			ConfigRefs(refs...)
	}
}

// formatWGHandshake 输出握手新鲜度描述。
func formatWGHandshake(t time.Time) string {
	if t.IsZero() {
		return "从未握手"
	}
	age := time.Since(t)
	if age < 3*time.Minute {
		return fmt.Sprintf("%s 前（活跃）", age.Truncate(time.Second))
	}
	return fmt.Sprintf("%s 前（疑似离线）", age.Truncate(time.Second))
}
