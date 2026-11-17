package shared

import (
	"context"
	"fmt"
	"os"
	"strings"
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
		only            []string
		skip            []string
		notifyMail      []string
		historyDir      string
		fix             bool
	)

	checkCmd := &cobra.Command{
		Use:   "check [group]",
		Short: "run automated health checks (groups: env auth master server client worker wg webui data perf all)",
		Long: `frp-panel 自动化排查工具。

分组:
  env     环境与配置（.env、密钥、DB、端口、OTA 状态、pprof、磁盘水位、文件权限）
  auth    认证与安全（注册开关、登录、JWT、Cookie[含 SameSite]、frps 认证插件）
  master  控制面（REST 路由组、gRPC 四种传输、节点在线状态、节点版本偏差）
  server  frps 生命周期（配置解析、端口冲突、认证插件宿主、流量统计）
  client  frpc 与隧道（配置解析、proxy 类型覆盖、节点心跳、transport 矩阵）
  worker  workerd（DB 完整性、二进制、代码漂移、ingress 引用）
  wg      WireGuard 组网（地址规划、拓扑完整性、公网 relay 约束、密钥、设备探测[仅 linux]）
  webui   前端（首页、静态资源、日志流、pb 同步、版本一致性）
  data    数据一致性（孤儿记录、软删除、统计对账、证书有效期、配额、迁移与备份）
  perf    性能基线（API QPS/P99、gRPC RTT）

示例:
  frpp check all
  frpp check env auth master
  frpp check data --only db_migration           # 只运行单个检查项
  frpp check all --skip perf                    # 跳过某组/某项
  frpp check all --format prometheus --out /var/lib/node_exporter/textfile/frpp.prom
  frpp check all --login-user admin --login-password '***'
  frpp check all --notify-mail ops@example.com  # 失败/告警邮件通知（复用面板 SMTP 配置）
  frpp check all --fix                          # 一键修复（默认仅列出；加 --destructive 实际执行）
  frpp check diff old.json new.json             # 对比两份报告，新增失败退出码非 0
  frpp check report merge a.json b.json --out overview.json   # 多节点聚合
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
				Only:            only,
				Skip:            skip,
				NotifyMail:      notifyMail,
				HistoryDir:      historyDir,
				Fix:             fix,
			}

			env := checker.NewEnv(cfg, opts)
			report, err := checker.Run(ctx, env, groups)
			if err != nil {
				return err
			}

			if err := checker.Render(report, opts); err != nil {
				return err
			}

			// 报告历史：保存本次报告；终端模式下附一行与上次的对比摘要
			if opts.HistoryDir != "" {
				prevPath, histErr := checker.SaveHistoryReport(opts.HistoryDir, report)
				if histErr != nil {
					fmt.Printf("[warn] 保存报告历史失败: %v\n", histErr)
				} else if opts.Out == "" && prevPath != "" {
					if prev, loadErr := checker.LoadReport(prevPath); loadErr == nil {
						fmt.Print(checker.RenderDiffSummary(checker.CompareReports(prev, report)))
					}
				}

				// 邮件告警：有失败/告警且配置了收件人时发送（内容 24h 去重）
				if len(opts.NotifyMail) > 0 && (report.Summary["fail"] > 0 || report.Summary["warn"] > 0) {
					sent, mailErr := checker.NotifyMail(ctx, env, report, opts.NotifyMail, opts.HistoryDir)
					switch {
					case mailErr != nil:
						fmt.Printf("[warn] 邮件告警发送失败: %v\n", mailErr)
					case sent:
						fmt.Printf("[info] 检查摘要已发送至 %s\n", strings.Join(opts.NotifyMail, ", "))
					default:
						fmt.Println("[info] 检查摘要与上次相同（24h 内已发送），跳过邮件")
					}
				}
			}

			// 一键修复：默认仅列出动作，--destructive 才实际执行（需要可写连接）
			if opts.Fix {
				if db, dbErr := openMigrationDB(cfg, false); dbErr != nil {
					fmt.Printf("[warn] 一键修复需要可写数据库连接: %v\n", dbErr)
				} else {
					fmt.Print(checker.RenderFixText(checker.ApplyFixes(ctx, db, env, !destructive)))
					closeMigrationDB(db)
				}
			}

			if report.FailExit {
				os.Exit(1)
			}
			return nil
		},
	}

	checkCmd.Flags().StringVar(&format, "format", "text", "output format: text|json|markdown|prometheus")
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
	checkCmd.Flags().StringSliceVar(&only, "only", nil, "run only matching checkers: group or group/name (e.g. data, data/db_migration)")
	checkCmd.Flags().StringSliceVar(&skip, "skip", nil, "skip matching checkers: group or group/name")
	checkCmd.Flags().StringSliceVar(&notifyMail, "notify-mail", nil, "send failure/warn digest to these emails (reuses panel SMTP settings)")
	checkCmd.Flags().StringVar(&historyDir, "history-dir", "check-history", "report history dir (keeps latest 30; enables last-run diff and notify dedupe)")
	checkCmd.Flags().BoolVar(&fix, "fix", false, "run auto-fixers: lists actions by default, applies with --destructive")

	checkCmd.AddCommand(
		newCheckDiffCmd(),
		newCheckReportCmd(),
	)
	return checkCmd
}

// newCheckDiffCmd 构造 `frpp check diff`：对比两份 JSON 报告；出现新增失败时退出码非 0。
func newCheckDiffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diff <old.json> <new.json>",
		Short: "compare two JSON reports (exits non-zero on new failures)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			oldReport, err := checker.LoadReport(args[0])
			if err != nil {
				return err
			}
			newReport, err := checker.LoadReport(args[1])
			if err != nil {
				return err
			}
			d := checker.CompareReports(oldReport, newReport)
			d.Old, d.New = args[0], args[1]
			fmt.Print(checker.RenderDiffText(d))
			if len(d.NewFailures) > 0 {
				os.Exit(1)
			}
			return nil
		},
	}
}

// newCheckReportCmd 构造 `frpp check report`（聚合子命令组）。
func newCheckReportCmd() *cobra.Command {
	reportCmd := &cobra.Command{
		Use:   "report",
		Short: "report aggregation tools",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	var (
		format string
		out    string
	)
	mergeCmd := &cobra.Command{
		Use:   "merge <report1.json> [report2.json ...]",
		Short: "aggregate reports from multiple nodes into one overview",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var reports []*checker.RunReport
			for _, p := range args {
				r, err := checker.LoadReport(p)
				if err != nil {
					return err
				}
				reports = append(reports, r)
			}
			merged := checker.MergeReports(reports)
			var (
				text string
				err  error
			)
			switch format {
			case "markdown", "md":
				text, err = checker.RenderMergedMarkdown(merged)
			case "json", "":
				text, err = checker.RenderMergedJSON(merged)
			default:
				return fmt.Errorf("unknown format %q (valid: json|markdown)", format)
			}
			if err != nil {
				return err
			}
			fmt.Print(text)
			if out != "" {
				if err := os.WriteFile(out, []byte(text), 0o600); err != nil {
					return fmt.Errorf("write merged report %s: %w", out, err)
				}
				fmt.Printf("\nmerged report written to %s\n", out)
			}
			return nil
		},
	}
	mergeCmd.Flags().StringVar(&format, "format", "json", "output format: json|markdown")
	mergeCmd.Flags().StringVar(&out, "out", "", "write merged report to file")
	reportCmd.AddCommand(mergeCmd)
	return reportCmd
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
