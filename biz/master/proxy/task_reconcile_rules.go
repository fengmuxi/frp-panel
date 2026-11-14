package proxy

import (
	"context"
	"time"

	"github.com/VaalaCat/frp-panel/defs"
	"github.com/VaalaCat/frp-panel/models"
	"github.com/VaalaCat/frp-panel/pb"
	"github.com/VaalaCat/frp-panel/services/app"
	"github.com/VaalaCat/frp-panel/services/dao"
	"github.com/VaalaCat/frp-panel/utils/logger"
)

// ReconcileTunnelRules 隧道自动规则对账任务（Master 每分钟执行一次）：
// 对启用了定时开关/流量限额的隧道，计算期望启停状态并与当前 Stopped 对齐：
//
//   - 期望关 & 当前开 → 自动停用（标记 auto_stopped=true）
//   - 期望开 & 当前关 & auto_stopped=true → 自动恢复（定时窗口开启或统计窗口重置后）
//   - 期望开 & 当前关 & auto_stopped=false → 用户手动暂停，保持不动（手动优先）
//
// 启停复用 StartProxy/StopProxy 完整链路（含配置下发），通过按隧道所属用户合成的
// context 提供用户信息，与页面手动操作行为完全一致。
func ReconcileTunnelRules(appInstance app.Application) error {
	ctx := app.NewContext(context.Background(), appInstance)

	proxyConfigs, err := dao.NewQuery(ctx).AdminListProxyConfigsWithRules()
	if err != nil {
		logger.Logger(ctx).WithError(err).Errorf("reconcile tunnel rules cannot list proxy configs")
		return err
	}
	if len(proxyConfigs) == 0 {
		return nil
	}

	now := time.Now()
	// 同一轮内复用已加载的用户记录
	userCache := map[int]*models.UserEntity{}

	for _, cfg := range proxyConfigs {
		if err := reconcileTunnelRule(ctx, userCache, cfg, now); err != nil {
			logger.Logger(ctx).WithError(err).Warnf("reconcile tunnel rule failed, proxy: [%s/%s], client: [%s]",
				cfg.ServerID, cfg.Name, cfg.ClientID)
		}
	}
	return nil
}

func reconcileTunnelRule(ctx *app.Context, userCache map[int]*models.UserEntity, cfg *models.ProxyConfig, now time.Time) error {
	// 1. 计算期望状态：定时窗口内 且 未超限额
	desiredOn := true
	if cfg.ScheduleEnabled {
		desiredOn = ScheduleWindowOpen(now, cfg.ScheduleStart, cfg.ScheduleEnd, cfg.ScheduleDays)
	}
	if desiredOn && cfg.QuotaType != "" && cfg.QuotaLimit > 0 {
		if CalcProxyQuotaUsed(ctx, cfg.ProxyConfigEntity) >= cfg.QuotaLimit {
			desiredOn = false
		}
	}

	// 2. 与当前状态对齐
	if desiredOn {
		if cfg.Stopped && cfg.AutoStopped {
			return autoStartProxy(ctx, userCache, cfg)
		}
		// 当前开启，或处于手动暂停状态：不动
		return nil
	}
	if !cfg.Stopped {
		return autoStopProxy(ctx, userCache, cfg)
	}
	// 已关闭：无需动作
	return nil
}

// buildProxyOwnerContext 为隧道所属用户合成 context，使 StartProxy/StopProxy 能取到
// 与页面请求一致的用户信息（用户名/令牌/租户等）
func buildProxyOwnerContext(ctx *app.Context, userCache map[int]*models.UserEntity, cfg *models.ProxyConfig) (*app.Context, error) {
	user, ok := userCache[cfg.UserID]
	if !ok {
		var err error
		user, err = dao.NewQuery(ctx).GetUserByUserID(cfg.UserID)
		if err != nil {
			return nil, err
		}
		userCache[cfg.UserID] = user
	}

	// 与 middleware.AuthCtx 相同的 key 与类型，common.GetUserInfo 可直接读取
	ownerCtx := context.WithValue(context.Background(), defs.UserInfoKey, user)
	return app.NewContext(ownerCtx, ctx.GetApp()), nil
}

func autoStopProxy(ctx *app.Context, userCache map[int]*models.UserEntity, cfg *models.ProxyConfig) error {
	ownerCtx, err := buildProxyOwnerContext(ctx, userCache, cfg)
	if err != nil {
		return err
	}

	logger.Logger(ctx).Infof("auto stop tunnel by rules, proxy: [%s/%s], client: [%s], schedule: [%v %s-%s], quota: [%s/%d]",
		cfg.ServerID, cfg.Name, cfg.ClientID, cfg.ScheduleEnabled, cfg.ScheduleStart, cfg.ScheduleEnd, cfg.QuotaType, cfg.QuotaLimit)

	if _, err := StopProxy(ownerCtx, &pb.StopProxyRequest{
		ClientId: &cfg.ClientID,
		ServerId: &cfg.ServerID,
		Name:     &cfg.Name,
	}); err != nil {
		return err
	}

	// StopProxy 会把 auto_stopped 清为 false（手动语义），此处再标记回自动停用
	return dao.NewMutation(ctx).AdminUpdateProxyConfigAutoStopped(cfg.ID, true)
}

func autoStartProxy(ctx *app.Context, userCache map[int]*models.UserEntity, cfg *models.ProxyConfig) error {
	ownerCtx, err := buildProxyOwnerContext(ctx, userCache, cfg)
	if err != nil {
		return err
	}

	logger.Logger(ctx).Infof("auto start tunnel by rules, proxy: [%s/%s], client: [%s]",
		cfg.ServerID, cfg.Name, cfg.ClientID)

	// StartProxy 内部会将 auto_stopped 置回 false
	_, err = StartProxy(ownerCtx, &pb.StartProxyRequest{
		ClientId: &cfg.ClientID,
		ServerId: &cfg.ServerID,
		Name:     &cfg.Name,
	})
	return err
}
