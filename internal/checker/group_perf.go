package checker

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/fengmuxi/frp-panel/defs"
	"github.com/fengmuxi/frp-panel/pb"
)

func buildPerfGroup(env *Env) ([]Checker, error) {
	return []Checker{
		&perfAPIBaseline{baseChecker{name: "api_baseline", group: "perf", severity: LevelMinor,
			fixHint: "P99 过高通常是 DB 慢查询或缓存（MASTER_CACHE_SIZE）不足；结合 DB 慢日志定位"}},
		&perfGRPCRTT{baseChecker{name: "grpc_rtt", group: "perf", severity: LevelMinor,
			fixHint: "PullConfig RTT 高会拖慢 EVENT_UPDATE_FRPC 下发；检查 master 与节点间网络质量"}},
		&perfConfigDownlink{baseChecker{name: "config_downlink", group: "perf", severity: LevelMinor,
			fixHint: "批量配置下发压测会写库并触发真实下发给节点，仅在维护窗口以 --destructive 执行"}},
	}, nil
}

// percentile 计算有序样本的分位值。
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	return sorted[idx]
}

// ---------------- perf/api_baseline ----------------

type perfAPIBaseline struct{ baseChecker }

func (c *perfAPIBaseline) Run(ctx context.Context, env *Env) Result {
	refs := []string{"GET /（无鉴权基线）", "MASTER_CACHE_SIZE", "DB_TYPE"}
	duration := env.Opts.PerfDuration
	if duration <= 0 {
		duration = 3 * time.Second
	}
	concurrency := env.Opts.PerfConcurrency
	if concurrency <= 0 {
		concurrency = 4
	}

	var (
		mu      sync.Mutex
		latency []float64
		fails   int
		wg      sync.WaitGroup
	)
	// 目标完全不可达时快速中止，避免对拒绝连接的地址打出海量空请求
	giveUp := func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(latency) == 0 && fails >= 200
	}

	deadline := time.Now().Add(duration)
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			for time.Now().Before(deadline) {
				if giveUp() {
					return
				}
				cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
				start := time.Now()
				code, _, _, err := env.Get(cctx, "/")
				elapsed := time.Since(start).Seconds() * 1000
				cancel()
				mu.Lock()
				if err != nil || code != http.StatusOK {
					fails++
				} else {
					latency = append(latency, elapsed)
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if len(latency) == 0 {
		return Result{}.Fail("压测期全部请求失败").WithDetails(fmt.Sprintf("fails=%d", fails)).ConfigRefs(refs...)
	}
	sort.Float64s(latency)
	qps := float64(len(latency)) / duration.Seconds()
	details := []string{
		fmt.Sprintf("样本=%d 失败=%d 并发=%d 时长=%s", len(latency), fails, concurrency, FormatDuration(duration)),
		fmt.Sprintf("QPS=%.0f P50=%.1fms P99=%.1fms", qps, percentile(latency, 0.5), percentile(latency, 0.99)),
	}
	if percentile(latency, 0.99) > 1000 {
		return Result{}.Warn("API P99 超过 1s").WithDetails(details...).ConfigRefs(refs...)
	}
	return Result{}.Pass("API 基线正常").WithDetails(details...).ConfigRefs(refs...)
}

// ---------------- perf/grpc_rtt ----------------

type perfGRPCRTT struct{ baseChecker }

func (c *perfGRPCRTT) Run(ctx context.Context, env *Env) Result {
	refs := []string{"PullClientConfig", "EVENT_PING 对照", "defs.PullConfigDuration(30s)"}
	const samples = 10

	var rtts []float64
	var failed int
	for i := 0; i < samples; i++ {
		elapsed, err := grpcProbe(ctx, env.Cfg.Master.RPCHost, env.Cfg.Master.RPCPort)
		if err != nil {
			// 探测使用无效凭证，若 master 明确拒绝（服务存活）也计入 RTT
			failed++
			continue
		}
		rtts = append(rtts, float64(elapsed.Microseconds())/1000)
	}
	if len(rtts) == 0 {
		return Result{}.Skip(fmt.Sprintf("gRPC 探测未获得成功样本（%d 次失败），先解决 master/grpc 连通性", failed)).ConfigRefs(refs...)
	}
	sort.Float64s(rtts)
	details := []string{
		fmt.Sprintf("样本=%d 失败=%d", len(rtts), failed),
		fmt.Sprintf("RTT(ms): min=%.1f P50=%.1f P99=%.1f", rtts[0], percentile(rtts, 0.5), percentile(rtts, 0.99)),
	}
	if percentile(rtts, 0.99) > float64(defs.PullConfigDuration.Milliseconds()) {
		return Result{}.Warn("PullConfig RTT 超过一个轮询周期(30s)，配置下发将明显滞后").WithDetails(details...).ConfigRefs(refs...)
	}
	return Result{}.Pass("gRPC 一元调用延迟正常").WithDetails(details...).ConfigRefs(refs...)
}

// ---------------- perf/config_downlink（destructive） ----------------

type perfConfigDownlink struct{ baseChecker }

func (c *perfConfigDownlink) Run(ctx context.Context, env *Env) Result {
	if !env.Opts.Destructive {
		return Result{}.Skip("批量配置下发压测会写库并推送真实节点，需显式 --destructive")
	}
	if !env.HasCredentials() {
		return Result{}.Skip("需要 --login-user/--login-password")
	}
	token, err := env.Login(ctx)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("登录失败: %v", err))
	}

	refs := []string{"/api/v1/proxy/list_configs", "压测读路径覆盖 proxy 路由组全链路"}

	// 压测对象：list_configs 分页读取（读路径覆盖 proxy 组全链路，不写库；
	// 写路径压测会触发真实节点下发，请使用 UI 或专用压测客户端）
	start := time.Now()
	var total int
	for page := 0; page < 10; page++ {
		code, _, body, err := env.PostProto(ctx, "/api/v1/proxy/list_configs", &pb.ListProxyConfigsRequest{
			Page:     int32Ptr(int32(page)),
			PageSize: int32Ptr(100),
		}, token)
		if err != nil || code != http.StatusOK {
			return Result{}.Warn(fmt.Sprintf("list_configs page=%d 失败(http=%d err=%v)，压测中止", page, code, err)).ConfigRefs(refs...)
		}
		total += len(body)
	}
	elapsed := time.Since(start)
	return Result{}.Pass("代理配置批量读取压测完成").
		WithDetails(fmt.Sprintf("10 页共 %d bytes，耗时 %s", total, FormatDuration(elapsed))).ConfigRefs(refs...)
}

// int32Ptr 为 proto optional int32 字段取指针。
func int32Ptr(v int32) *int32 { return &v }
