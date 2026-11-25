package watcher

import (
	"context"
	"time"

	"github.com/VaalaCat/frp-panel/utils/logger"
	"github.com/go-co-op/gocron/v2"
)

type Client interface {
	Run()
	Stop()
	AddDurationTask(time.Duration, any, ...any) error
	AddCronTask(string, any, ...any) error
}

type client struct {
	s gocron.Scheduler
}

func NewClient() Client {
	// 全项目统一北京时间：cron 调度不依赖部署机系统时区（裸机/容器默认 UTC 时，
	// "0 0 3 * * *" 的每日快照会漂到北京 11:00）。中国固定 +8 无夏令时。
	// 注意 gocron 会把 location 名字拼成 CRON_TZ=<name> 交给 robfig/cron，
	// 后者按名字 time.LoadLocation 重新加载——自定义名（如 "CST"、"+08:00"）
	// 不在 zoneinfo 里会直接 fatal，必须用真实时区名 Asia/Shanghai；
	// 极少数无 tzdata 的精简环境回退 time.Local（配合 TZ env 仍为北京时间）。
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		logger.Logger(context.Background()).WithError(err).Warn("Asia/Shanghai zoneinfo unavailable, cron falls back to local timezone (set TZ to keep Beijing time)")
		loc = time.Local
	}
	s, err := gocron.NewScheduler(gocron.WithLocation(loc))
	if err != nil {
		logger.Logger(context.Background()).WithError(err).Fatalf("create scheduler error")
	}
	return &client{
		s: s,
	}
}

func (c *client) AddDurationTask(duration time.Duration, function any, parameters ...any) error {
	_, err := c.s.NewJob(
		gocron.DurationJob(duration),
		gocron.NewTask(function, parameters...),
	)
	if err != nil {
		logger.Logger(context.Background()).WithError(err).Fatalf("create task error")
	}
	return err
}

func (c *client) AddCronTask(cron string, function any, parameters ...any) error {
	_, err := c.s.NewJob(
		gocron.CronJob(cron, true),
		gocron.NewTask(function, parameters...),
		// 单实例模式：上一轮任务未结束时禁止下一轮并发进入——
		// ReconcileTunnelRules 等对账任务若因大量 CallClient 超过 60s 周期，
		// 两轮并发会对同一隧道重复启停并重复发送通知
		gocron.WithSingletonMode(gocron.LimitModeReschedule),
	)
	if err != nil {
		logger.Logger(context.Background()).WithError(err).Fatalf("create task error")
	}
	return err
}

func (c *client) Run() {
	ctx := context.Background()
	logger.Logger(ctx).Infof("start to run scheduler")
	c.s.Start()
}

func (c *client) Stop() {
	if err := c.s.Shutdown(); err != nil {
		logger.Logger(context.Background()).WithError(err).Errorf("shutdown scheduler error")
	}
}
