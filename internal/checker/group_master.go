package checker

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/fengmuxi/frp-panel/conf"
	"github.com/fengmuxi/frp-panel/defs"
	"github.com/fengmuxi/frp-panel/models"
	"github.com/fengmuxi/frp-panel/pb"
	"github.com/fengmuxi/frp-panel/utils/logger"
	"github.com/fengmuxi/frp-panel/utils/wsgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
)

func buildMasterGroup(env *Env) ([]Checker, error) {
	return []Checker{
		&masterGRPCTLS{baseChecker{name: "grpc_tls", group: "master", severity: LevelCritical,
			fixHint: "检查 MASTER_RPC_PORT 是否监听、TLS 证书链（conf.GetCertTemplate 自签 CA）与 MASTER_RPC_HOST 一致性"}},
		&masterGRPCInsecure{baseChecker{name: "grpc_insecure", group: "master", severity: LevelMinor,
			fixHint: "insecure 模式仅用于本机/反代内网；若线上暴露明文 gRPC 端口请启用 CLIENT_TLS_RPC"}},
		&masterGRPCWS{baseChecker{name: "grpc_ws", group: "master", severity: LevelMajor,
			fixHint: "ws 传输依赖 API 端口的 /wsgrpc 路由；检查反向代理是否放行该路径与升级头"}},
		&masterGRPCWSS{baseChecker{name: "grpc_wss", group: "master", severity: LevelMinor,
			fixHint: "wss 需要 API 端口前置 HTTPS 反代或 TLS；检查证书有效性"}},
		&masterWSGRPCEndpoint{baseChecker{name: "wsgrpc_route", group: "master", severity: LevelMajor,
			fixHint: "runMaster 中注册于 MasterRouter(GET /wsgrpc)；404 说明路由未挂载或请求打到了别的服务"}},
		&masterNodeStatus{baseChecker{name: "node_status", group: "master", severity: LevelMajor,
			fixHint: "提供 --login-user/--login-password 后可检查节点在线状态与 EVENT_PING 延迟；掉线节点请查看节点侧日志与网络"}},
		&masterNodeVersionSkew{baseChecker{name: "node_version_skew", group: "master", severity: LevelMinor,
			fixHint: "版本偏差的节点仍在跑旧二进制：对节点执行 frpp upgrade 或重新部署，OTA 灰度期间属正常"}},
		&masterJSONCompat{baseChecker{name: "json_compat", group: "master", severity: LevelMinor,
			fixHint: "项目 REST 层支持 protobuf 与 JSON 双协议（common.GetProtoRequest/OKResp）；JSON 失败影响部分老前端调用"}},
	}, nil
}

// grpcProbe 用一元 RPC PullClientConfig 探测 gRPC 服务：
// 使用无效凭证 —— 无论返回什么业务码，只要拿到响应就说明服务存活且鉴权链路在工作。
// 返回往返耗时。
func grpcProbe(ctx context.Context, host string, port int, dialOpts ...grpc.DialOption) (time.Duration, error) {
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := grpc.NewClient(addr, dialOpts...)
	if err != nil {
		return 0, fmt.Errorf("grpc.NewClient(%s): %w", addr, err)
	}
	defer conn.Close()

	cli := pb.NewMasterClient(conn)
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	start := time.Now()
	resp, err := cli.PullClientConfig(cctx, &pb.PullClientConfigReq{
		Base: &pb.ClientBase{ClientId: "frpp-check-probe", ClientSecret: "invalid-secret"},
	})
	elapsed := time.Since(start)
	if err != nil {
		// UNAUTHENTICATED/PERMISSION_DENIED 类错误同样证明服务存活
		return elapsed, fmt.Errorf("PullClientConfig: %w", err)
	}
	if resp == nil {
		return elapsed, fmt.Errorf("empty response from master")
	}
	return elapsed, nil
}

// ---------------- master/grpc_tls ----------------

type masterGRPCTLS struct{ baseChecker }

func (c *masterGRPCTLS) Run(ctx context.Context, env *Env) Result {
	refs := []string{"MASTER_RPC_HOST", "MASTER_RPC_PORT", "CLIENT_TLS_RPC"}
	// master 使用 conf.GetCertTemplate 自签证书，节点默认不校验（CLIENT_TLS_INSECURE_SKIP_VERIFY=true）
	tlsCred := grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		InsecureSkipVerify: env.Cfg.Client.TLSInsecureSkipVerify,
	}))
	elapsed, err := grpcProbe(ctx, env.Cfg.Master.RPCHost, env.Cfg.Master.RPCPort, tlsCred)
	if err != nil {
		return Result{}.Fail(fmt.Sprintf("gRPC over TLS 探测失败: %v", err)).ConfigRefs(refs...)
	}
	return Result{}.Pass(fmt.Sprintf("gRPC over TLS 可用（%s）", FormatDuration(elapsed))).ConfigRefs(refs...)
}

// ---------------- master/grpc_insecure ----------------

type masterGRPCInsecure struct{ baseChecker }

func (c *masterGRPCInsecure) Run(ctx context.Context, env *Env) Result {
	refs := []string{"MASTER_RPC_PORT", "CLIENT_TLS_RPC"}
	elapsed, err := grpcProbe(ctx, env.Cfg.Master.RPCHost, env.Cfg.Master.RPCPort,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return Result{}.Warn(fmt.Sprintf("明文 gRPC 探测失败: %v（若生产为纯 TLS 部署属正常）", err)).ConfigRefs(refs...)
	}
	return Result{}.Warn(fmt.Sprintf("明文 gRPC 可达（%s）：确认该端口未被公网直接暴露", FormatDuration(elapsed))).ConfigRefs(refs...)
}

// ---------------- master/grpc_ws / grpc_wss ----------------

type masterGRPCWS struct{ baseChecker }

func (c *masterGRPCWS) Run(ctx context.Context, env *Env) Result {
	return probeWSTransport(ctx, env, conf.WS)
}

type masterGRPCWSS struct{ baseChecker }

func (c *masterGRPCWSS) Run(ctx context.Context, env *Env) Result {
	return probeWSTransport(ctx, env, conf.WSS)
}

// probeWSTransport 复用项目 utils/wsgrpc 的拨号器（与节点侧完全同一条代码路径），
// 对 ws:// 或 wss:// <RPC conn host>/wsgrpc 发起 gRPC 探测。
func probeWSTransport(ctx context.Context, env *Env, scheme conf.Scheme) Result {
	refs := []string{"CLIENT_RPC_URL", "MASTER_API_HOST", "MASTER_API_PORT", "/wsgrpc"}
	elapsed, err := probeWSRaw(ctx, env, string(scheme))
	if err != nil {
		return Result{}.Fail(fmt.Sprintf("%s 传输探测失败: %v", scheme, err)).ConfigRefs(refs...)
	}
	return Result{}.Pass(fmt.Sprintf("%s 传输可用（%s）", scheme, FormatDuration(elapsed))).ConfigRefs(refs...)
}

// probeWSRaw 是 ws/wss 探测的原始实现，供 master 组与 client 组共用。
func probeWSRaw(ctx context.Context, env *Env, scheme string) (time.Duration, error) {
	wsURL := fmt.Sprintf("%s://%s/wsgrpc", scheme, conf.GetRPCConnInfo(env.Cfg).Host)
	dialer := wsgrpc.WebsocketDialer(wsURL, nil, env.Cfg.Client.TLSInsecureSkipVerify, logger.Logger(context.Background()))
	// gRPC 地址用可解析的占位 host:port，实际拨号完全由 wsgrpc dialer 接管
	return grpcProbe(ctx, defs.LocalHost, 0,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(dialer),
	)
}

// ---------------- master/wsgrpc_route ----------------

type masterWSGRPCEndpoint struct{ baseChecker }

func (c *masterWSGRPCEndpoint) Run(ctx context.Context, env *Env) Result {
	refs := []string{"GET /wsgrpc（无升级头）"}
	code, _, _, err := env.Get(ctx, "/wsgrpc")
	if err != nil {
		return Result{}.Fail(fmt.Sprintf("/wsgrpc 不可达: %v", err)).ConfigRefs(refs...)
	}
	switch code {
	case http.StatusBadRequest, http.StatusUpgradeRequired, http.StatusNotImplemented:
		return Result{}.Pass("/wsgrpc 路由存在（拒绝非 WebSocket 请求）").WithDetails(fmt.Sprintf("http=%d", code)).ConfigRefs(refs...)
	case http.StatusNotFound:
		return Result{}.Fail("/wsgrpc 返回 404：路由未挂载，ws/wss 传输将全部失败").ConfigRefs(refs...)
	default:
		return Result{}.Warn(fmt.Sprintf("/wsgrpc 返回 %d，需人工确认", code)).ConfigRefs(refs...)
	}
}

// ---------------- master/node_status ----------------

type masterNodeStatus struct{ baseChecker }

func (c *masterNodeStatus) Run(ctx context.Context, env *Env) Result {
	refs := []string{"/api/v1/platform/clientsstatus", "EVENT_PING", "defs.OfflineCheckDelay"}
	if !env.HasCredentials() {
		return Result{}.Skip("未提供登录凭证，无法查询节点在线状态与 PING 延迟").ConfigRefs(refs...)
	}
	token, err := env.Login(ctx)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("登录失败: %v", err)).ConfigRefs(refs...)
	}

	code, _, body, err := env.PostProto(ctx, "/api/v1/platform/clientsstatus",
		&pb.GetClientsStatusRequest{ClientType: pb.ClientType_CLIENT_TYPE_FRPC}, token)
	if err != nil {
		return Result{}.Fail(fmt.Sprintf("clientsstatus 请求失败: %v", err)).ConfigRefs(refs...)
	}
	if code != http.StatusOK {
		return Result{}.Fail(fmt.Sprintf("clientsstatus 返回 http=%d", code)).ConfigRefs(refs...)
	}
	resp := &pb.GetClientsStatusResponse{}
	if err := proto.Unmarshal(body, resp); err != nil {
		return Result{}.Fail(fmt.Sprintf("clientsstatus 响应解析失败: %v", err)).ConfigRefs(refs...)
	}
	if resp.GetStatus().GetCode() != pb.RespCode_RESP_CODE_SUCCESS {
		return Result{}.Fail(fmt.Sprintf("clientsstatus 业务失败: %s", resp.GetStatus().GetMessage())).ConfigRefs(refs...)
	}

	var (
		online, offline, errored int
		pings                    []int32
		offlineIDs               []string
	)
	for id, st := range resp.GetClients() {
		if !env.NodeFilter(id) {
			continue
		}
		switch st.GetStatus() {
		case pb.ClientStatus_STATUS_ONLINE:
			online++
			if st.GetPing() > 0 {
				pings = append(pings, st.GetPing())
			}
		case pb.ClientStatus_STATUS_OFFLINE:
			offline++
			offlineIDs = append(offlineIDs, id)
		case pb.ClientStatus_STATUS_ERROR:
			errored++
			offlineIDs = append(offlineIDs, id+"(error)")
		}
	}

	details := []string{fmt.Sprintf("在线=%d 离线=%d 错误=%d", online, offline, errored)}
	if len(pings) > 0 {
		sort.Slice(pings, func(i, j int) bool { return pings[i] < pings[j] })
		var sum int64
		for _, p := range pings {
			sum += int64(p)
		}
		p99 := pings[len(pings)-1]
		if len(pings) >= 20 {
			p99 = pings[len(pings)*99/100]
		}
		details = append(details, fmt.Sprintf("EVENT_PING(ms): min=%d avg=%.0f p99=%d (n=%d)",
			pings[0], float64(sum)/float64(len(pings)), p99, len(pings)))
	}
	if len(offlineIDs) > 0 {
		return Result{}.Warn(fmt.Sprintf("%d 个节点离线/异常: %s", len(offlineIDs), joinLimit(offlineIDs, 8))).
			WithDetails(details...).ConfigRefs(refs...)
	}

	// DB 对账：库中 client 总数 vs 运行时状态数量
	var dbTotal int64
	if db, err := env.DB(ctx); err == nil {
		_ = db.Model(&models.Client{}).Where("deleted_at IS NULL").Count(&dbTotal).Error
	}
	if dbTotal > 0 && int(dbTotal) > online+offline+errored {
		details = append(details, fmt.Sprintf("DB clients=%d > 运行时状态=%d：部分节点从未连接或注册表已摘除", dbTotal, online+offline+errored))
	}
	return Result{}.Pass("节点状态查询正常").WithDetails(details...).ConfigRefs(refs...)
}

// ---------------- master/node_version_skew ----------------

type masterNodeVersionSkew struct{ baseChecker }

// fetchClientStatus 查询某类节点的运行时状态（在线状态 + EVENT_PING 附带的版本信息）。
func fetchClientStatus(ctx context.Context, env *Env, token string, clientType pb.ClientType) (*pb.GetClientsStatusResponse, error) {
	code, _, body, err := env.PostProto(ctx, "/api/v1/platform/clientsstatus",
		&pb.GetClientsStatusRequest{ClientType: clientType}, token)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("http=%d", code)
	}
	resp := &pb.GetClientsStatusResponse{}
	if err := proto.Unmarshal(body, resp); err != nil {
		return nil, fmt.Errorf("响应解析失败: %w", err)
	}
	if resp.GetStatus().GetCode() != pb.RespCode_RESP_CODE_SUCCESS {
		return nil, fmt.Errorf("业务失败: %s", resp.GetStatus().GetMessage())
	}
	return resp, nil
}

func (c *masterNodeVersionSkew) Run(ctx context.Context, env *Env) Result {
	refs := []string{"/api/v1/platform/clientsstatus", "EVENT_PING ClientVersion", "frpp upgrade"}
	if !env.HasCredentials() {
		return Result{}.Skip("未提供登录凭证，无法对账节点版本").ConfigRefs(refs...)
	}
	token, err := env.Login(ctx)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("登录失败: %v", err)).ConfigRefs(refs...)
	}

	masterVersion := env.Version.GitVersion
	var (
		skewed, unknown []string
		online          int
	)
	for _, clientType := range []pb.ClientType{pb.ClientType_CLIENT_TYPE_FRPC, pb.ClientType_CLIENT_TYPE_FRPS} {
		resp, err := fetchClientStatus(ctx, env, token, clientType)
		if err != nil {
			return Result{}.Fail(fmt.Sprintf("clientsstatus(%s) %v", clientType, err)).ConfigRefs(refs...)
		}
		for id, st := range resp.GetClients() {
			if !env.NodeFilter(id) {
				continue
			}
			if st.GetStatus() != pb.ClientStatus_STATUS_ONLINE {
				continue // 离线节点由 master/node_status 报告
			}
			online++
			v := st.GetVersion().GetGitVersion()
			switch {
			case v == "":
				unknown = append(unknown, id+"（未上报版本，节点二进制过旧）")
			case v != masterVersion:
				skewed = append(skewed, fmt.Sprintf("%s: %s != master %s", id, v, masterVersion))
			}
		}
	}

	details := []string{fmt.Sprintf("master=%s, 在线节点=%d, 版本一致=%d, 待升级=%d, 未上报=%d",
		masterVersion, online, online-len(skewed)-len(unknown), len(skewed), len(unknown))}
	if len(skewed) > 0 {
		details = append(details, skewed...)
	}
	if len(unknown) > 0 {
		details = append(details, unknown...)
	}
	if len(skewed) > 0 {
		return Result{}.Warn(fmt.Sprintf("%d 个在线节点版本与 master 不一致", len(skewed))).WithDetails(details...).ConfigRefs(refs...)
	}
	if len(unknown) > 0 {
		return Result{}.Warn(fmt.Sprintf("%d 个在线节点未上报版本（二进制过旧）", len(unknown))).WithDetails(details...).ConfigRefs(refs...)
	}
	return Result{}.Pass("在线节点版本与 master 一致").WithDetails(details...).ConfigRefs(refs...)
}

// ---------------- master/json_compat ----------------

type masterJSONCompat struct{ baseChecker }

func (c *masterJSONCompat) Run(ctx context.Context, env *Env) Result {
	refs := []string{"common.GetProtoRequest（application/json 分支）"}
	if !env.HasCredentials() {
		return Result{}.Skip("未提供登录凭证，跳过 JSON 协议兼容检查").ConfigRefs(refs...)
	}
	token, err := env.Login(ctx)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("登录失败: %v", err)).ConfigRefs(refs...)
	}

	code, _, _, err := env.PostJSON(ctx, "/api/v1/user/get", "{}", token)
	if err != nil {
		return Result{}.Fail(fmt.Sprintf("JSON 协议请求失败: %v", err)).ConfigRefs(refs...)
	}
	if code != http.StatusOK {
		return Result{}.Warn(fmt.Sprintf("JSON 协议返回 http=%d（protobuf 协议可用即可）", code)).ConfigRefs(refs...)
	}
	return Result{}.Pass("JSON 协议兼容正常").ConfigRefs(refs...)
}

// joinLimit 拼接列表，最多展示 maxN 个。
func joinLimit(items []string, maxN int) string {
	if len(items) <= maxN {
		return strings.Join(items, ", ")
	}
	return strings.Join(items[:maxN], ", ") + fmt.Sprintf(" ...(+%d)", len(items)-maxN)
}
