package shared

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/VaalaCat/frp-panel/conf"
	"github.com/VaalaCat/frp-panel/internal/checker"
	"github.com/spf13/cobra"
)

// NewCheckCmd 构造 `frpp check` 子命令：
// 对当前部署（master / server / client 节点）执行全功能自动化排查。
// 默认只读；需要写操作的检查必须显式 --destructive。
func NewCheckCmd(cfg conf.Config) *cobra.Command {
	var (
		format          string
		out             string
		failOn          string
		timeout         time.Duration
		checkerTimeout  time.Duration
		concurrency     int
		destructive     bool
		node            string
		loginUser       string
		loginPassword   string
		perfDuration    time.Duration
		perfConcurrency int
		noColor         bool
		list            bool
	)

	checkCmd := &cobra.Command{
		Use:   "check [group]",
		Short: "run automated health checks (groups: env auth master server client worker wg webui data perf all)",
		Long: `frp-panel 自动化排查工具。

分组:
  env     环境与配置（.env、密钥、DB、端口、OTA 状态、pprof）
  auth    认证与安全（注册开关、登录、JWT、Cookie、frps 认证插件）
  master  控制面（REST 路由组、gRPC 四种传输、节点在线状态、EVENT_PING）
  server  frps 生命周期（配置解析、端口冲突、认证插件宿主、流量统计）
  client  frpc 与隧道（配置解析、proxy 类型覆盖、节点心跳、transport 矩阵）
  worker  workerd（DB 完整性、二进制、代码漂移、ingress 引用）
  wg      WireGuard 组网（地址规划、拓扑完整性、公网 relay 约束、密钥、设备探测[仅 linux]）
  webui   前端（首页、静态资源、日志流、pb 同步、版本一致性）
  data    数据一致性（孤儿记录、软删除审计、统计对账、schema 抽查）
  perf    性能基线（API QPS/P99、gRPC RTT）

示例:
  frpp check all
  frpp check env auth master
  frpp check wg --node node-a
  frpp check all --format json --out report.json --fail-on major`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			groups := checker.GroupNames
			if list {
				for _, line := range checker.AllCheckerNames() {
					fmt.Println(line)
				}
				return nil
			}
			if len(args) > 0 {
				var selected []string
				for _, a := range args {
					if a == "all" {
						selected = append(selected, checker.GroupNames...)
						continue
					}
					selected = append(selected, a)
				}
				groups = selected
			}

			level, ok := checker.ParseLevel(failOn)
			if !ok {
				return fmt.Errorf("invalid --fail-on value %q (valid: info|minor|major|critical)", failOn)
			}

			opts := checker.Options{
				Format:          format,
				Out:             out,
				FailOn:          level,
				Timeout:         timeout,
				CheckerTimeout:  checkerTimeout,
				Concurrency:     concurrency,
				Destructive:     destructive,
				Node:            node,
				AdminUser:       firstNonEmpty(loginUser, os.Getenv("CHECKER_ADMIN_USER")),
				AdminPassword:   firstNonEmpty(loginPassword, os.Getenv("CHECKER_ADMIN_PASSWORD")),
				PerfDuration:    perfDuration,
				PerfConcurrency: perfConcurrency,
				NoColor:         noColor,
				List:            list,
			}

			env := checker.NewEnv(cfg, opts)
			report, err := checker.Run(ctx, env, groups)
			if err != nil {
				return err
			}

			if err := checker.Render(report, opts); err != nil {
				return err
			}

			if report.FailExit {
				os.Exit(1)
			}
			return nil
		},
	}

	checkCmd.Flags().StringVar(&format, "format", "text", "output format: text|json|markdown")
	checkCmd.Flags().StringVar(&out, "out", "", "write report to file (e.g. report.json)")
	checkCmd.Flags().StringVar(&failOn, "fail-on", "major", "exit non-zero when failures at or above this level: info|minor|major|critical")
	checkCmd.Flags().DurationVar(&timeout, "timeout", 2*time.Minute, "global timeout for the whole run")
	checkCmd.Flags().DurationVar(&checkerTimeout, "checker-timeout", 15*time.Second, "per-checker timeout")
	checkCmd.Flags().IntVar(&concurrency, "concurrency", 4, "number of checkers running concurrently")
	checkCmd.Flags().BoolVar(&destructive, "destructive", false, "allow checks with side effects (they clean up after themselves)")
	checkCmd.Flags().StringVar(&node, "node", "", "target a single node by client id / server id / interface name")
	checkCmd.Flags().StringVar(&loginUser, "login-user", "", "admin username for JWT-backed checks (env: CHECKER_ADMIN_USER)")
	checkCmd.Flags().StringVar(&loginPassword, "login-password", "", "admin password for JWT-backed checks (env: CHECKER_ADMIN_PASSWORD)")
	checkCmd.Flags().DurationVar(&perfDuration, "perf-duration", 3*time.Second, "perf baseline duration")
	checkCmd.Flags().IntVar(&perfConcurrency, "perf-concurrency", 4, "perf baseline concurrency")
	checkCmd.Flags().BoolVar(&noColor, "no-color", false, "disable colored output")
	checkCmd.Flags().BoolVar(&list, "list", false, "list all checkers and exit")

	return checkCmd
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
