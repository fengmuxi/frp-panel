//go:build !windows

package workerd

import (
	"context"
	"os/exec"
	"syscall"
	"time"

	"github.com/fengmuxi/frp-panel/services/app"
	"github.com/fengmuxi/frp-panel/utils"
	"github.com/fengmuxi/frp-panel/utils/logger"
	"github.com/sirupsen/logrus"
)

type workerExecManager struct {
	//用于外层循坏的退出
	signMap *utils.SyncMap[string, bool]
	//用于执行cancel函数
	chanMap *utils.SyncMap[string, chan struct{}]
	// 可执行文件路径
	binaryPath string
	// 默认参数
	defaultArgs []string
}

// var ExecManager *execManager

func NewExecManager(binPath string, defaultArgs []string) app.WorkerExecManager {

	if len(defaultArgs) == 0 {
		defaultArgs = []string{"--watch", "--verbose"}
	}

	return &workerExecManager{
		signMap:     new(utils.SyncMap[string, bool]),
		chanMap:     new(utils.SyncMap[string, chan struct{}]),
		binaryPath:  binPath,
		defaultArgs: defaultArgs,
	}
}

func (m *workerExecManager) RunCmd(uid string, cwd string, argv []string) {
	ctx := context.Background()
	logger.Logger(context.Background()).Infof("start to run command, command id: [%s], argv: %s", uid, utils.MarshalForJson(argv))
	// LoadOrStore 原子去重：此前 check-then-store 非原子，并发双启动会产生
	// 两个 run loop + 两个 watcher，ExitCmd 只能唤醒其一——另一个 run loop
	// 永远拿不到取消信号，且 uid 从此被当作 "already running" 无法再启动。
	// 新建的取消信号 channel 由 watcher goroutine 自行从 chanMap 读取
	if _, loaded := m.chanMap.LoadOrStore(uid, make(chan struct{})); loaded {
		logger.Logger(ctx).Infof("command id: [%s] is already running, ignore", uid)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())

	go func(ctx context.Context, uid string, argv []string, m *workerExecManager) {
		defer func(uid string, m *workerExecManager) {
			m.signMap.Delete(uid)
		}(uid, m)

		logger.Logger(ctx).Infof("command id: [%s] is running!", uid)

		// 指数退避：持续崩溃的 worker 此前每 3s 固定重启并刷错误日志
		//（配置非法/二进制缺失/端口被占时永久循环）——3s→6s→…→60s 封顶
		failures := 0
		for {
			args := []string{}

			args = append(args, m.defaultArgs...)
			args = append(args, argv...)

			cmd := exec.CommandContext(ctx, m.binaryPath, args...)
			cmd.Dir = cwd
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: false}
			// 安全审计 P1-12（决策点 F）：workerd 执行租户 JS 代码，unix 下降权运行
			//（专用 frpp-worker 用户）；降权不可用时保留原行为并告警
			applyLowerPrivilege(cmd)
			cmd.Stdout = logger.LoggerWriter("workerd", logrus.InfoLevel)
			cmd.Stderr = logger.LoggerWriter("workerd", logrus.ErrorLevel)
			if err := cmd.Run(); err != nil {
				failures++
				logger.Logger(ctx).WithError(err).Errorf("command id: [%s] run failed (consecutive: %d), binary path: [%s], args: %s", uid, failures, m.binaryPath, utils.MarshalForJson(args))
			} else {
				failures = 0
			}

			if exit, ok := m.signMap.Load(uid); ok && exit {
				return
			}

			backoff := 3 * time.Second
			for i := 1; i < failures && backoff < 60*time.Second; i++ {
				backoff *= 2
			}
			time.Sleep(backoff)
		}
	}(ctx, uid, argv, m)

	go func(cancel context.CancelFunc, uid string, m *workerExecManager) {
		defer func(uid string, m *workerExecManager) {
			m.chanMap.Delete(uid)
		}(uid, m)

		if channel, ok := m.chanMap.Load(uid); ok {
			<-channel
			m.signMap.Store(uid, true)
			cancel()
			return
		} else {
			logger.Logger(ctx).Errorf("command id: [%s] is not running!", uid)
			return
		}
	}(cancel, uid, m)
}

func (m *workerExecManager) ExitCmd(uid string) {
	if channel, ok := m.chanMap.Load(uid); ok {
		channel <- struct{}{}
	}
}

func (m *workerExecManager) ExitAllCmd() {
	for uid := range m.chanMap.ToMap() {
		m.ExitCmd(uid)
	}
}

func (m *workerExecManager) UpdateBinaryPath(path string) {
	m.binaryPath = path
}
