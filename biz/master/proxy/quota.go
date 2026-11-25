package proxy

import (
	"time"

	"github.com/fengmuxi/frp-panel/defs"
	"github.com/fengmuxi/frp-panel/models"
	"github.com/fengmuxi/frp-panel/services/app"
	"github.com/fengmuxi/frp-panel/services/dao"
	"github.com/fengmuxi/frp-panel/utils/logger"
)

// CalcProxyQuotaUsed 计算隧道在当前统计窗口内的已用流量（字节）。
// 统计方向由 cfg.QuotaDirection 决定：入站/出站/双向合计（空与 both 均为双向，兼容旧数据）。
// 未启用限额或无统计数据时返回 0。
//
// 口径与精度：
//   - 日窗口：直接取 frps 上报的当日计数（TodayTrafficIn/Out，精确）；
//   - 周/月窗口：当前累计（History+Today）按方向减去“窗口起点之后最早一次每日历史快照”的
//     累计值；若窗口开始后尚无快照（例如周一凌晨），回退为减去当前 History，
//     即仅统计窗口起点以来的当日流量（等价于精确值）。
//   - 统计本身沿用面板既有的近似采集口径（见 models.HistoryProxyStats 注释）。
func CalcProxyQuotaUsed(ctx *app.Context, cfg *models.ProxyConfigEntity) int64 {
	if cfg.QuotaType == "" || cfg.QuotaLimit <= 0 {
		return 0
	}

	stats, err := dao.NewQuery(ctx).AdminGetProxyStatsByKey(cfg.UserID, cfg.TenantID, cfg.ServerID, cfg.Name)
	if err != nil {
		logger.Logger(ctx).WithError(err).Warnf("cannot get proxy stats for quota, proxy: [%s]", cfg.Name)
		return 0
	}
	if stats == nil {
		// 尚无统计数据，用量为 0
		return 0
	}

	// quotaDirectionSum 按限额方向对入/出流量求和
	directionSum := func(in, out int64) int64 {
		switch cfg.QuotaDirection {
		case defs.QuotaDirectionInbound:
			return in
		case defs.QuotaDirectionOutbound:
			return out
		default:
			return in + out
		}
	}

	switch cfg.QuotaType {
	case defs.QuotaTypeDay:
		return directionSum(stats.TodayTrafficIn, stats.TodayTrafficOut)
	case defs.QuotaTypeWeek, defs.QuotaTypeMonth:
		totalIn := stats.HistoryTrafficIn + stats.TodayTrafficIn
		totalOut := stats.HistoryTrafficOut + stats.TodayTrafficOut
		windowStart := QuotaWindowStart(time.Now(), cfg.QuotaType)

		baselineIn, baselineOut, found, err := dao.NewQuery(ctx).AdminGetQuotaHistoryBaseline(stats.ProxyID, windowStart)
		if err != nil {
			// 快照查询失败时宁可少算也不误停（fail-open），仅记录日志
			logger.Logger(ctx).WithError(err).Warnf("cannot get quota baseline, proxy: [%s]", cfg.Name)
			return 0
		}
		if !found {
			// 窗口开始后尚无快照：以当前累计 History 为基线，即只统计窗口起点以来的流量
			baselineIn = stats.HistoryTrafficIn
			baselineOut = stats.HistoryTrafficOut
		}

		usedIn := totalIn - baselineIn
		if usedIn < 0 {
			usedIn = 0
		}
		usedOut := totalOut - baselineOut
		if usedOut < 0 {
			usedOut = 0
		}
		return directionSum(usedIn, usedOut)
	default:
		return 0
	}
}
