package proxy

import (
	"time"

	"github.com/VaalaCat/frp-panel/models"
	"github.com/VaalaCat/frp-panel/services/app"
	"github.com/VaalaCat/frp-panel/services/dao"
	"github.com/VaalaCat/frp-panel/utils"
	"github.com/VaalaCat/frp-panel/utils/logger"
	"github.com/samber/lo"
)

// hourlySnapshotRetention 清理阈值：小时快照只服务"今天"的小时序列，保留 2 天足够
const hourlySnapshotRetention = 48 * time.Hour

// CollectHourlyStats 每小时整点（以及 master 启动时）把当前各隧道的
// 当日累计流量（proxy_stats.today_*）快照到 proxy_stats_hourly 表。
//
// 相邻整点快照差分即该小时增量，用于首页"今日"时段的小时粒度趋势；
// frps 内存统计与 proxy_stats 都是覆盖写、无历史轨迹，故必须在此落快照。
// 幂等：同一 (proxy_id, hour_ts) 重复写入会覆盖。
func CollectHourlyStats(c *app.Context) {
	live, err := dao.NewQuery(c).AdminGetAllProxyStats(c.GetApp().GetDBManager().GetDefaultDB())
	if err != nil {
		logger.Logger(c).WithError(err).Error("collect hourly stats: cannot list proxy stats")
		return
	}

	now := time.Now()
	hourTs := now.Unix() - now.Unix()%3600 // 当前整点

	snaps := make([]*models.ProxyStatsHourly, 0, len(live))
	for _, l := range live {
		if l == nil {
			continue
		}
		snaps = append(snaps, buildHourlySnapshot(l, hourTs, now))
	}

	if err := dao.NewMutation(c).SaveHourlySnapshots(snaps); err != nil {
		logger.Logger(c).WithError(err).Error("collect hourly stats: save snapshots failed")
		return
	}

	// 顺带清理过期快照与增量流水（失败不影响主流程）
	if err := dao.NewMutation(c).DeleteHourlySnapshotsBefore(now.Unix() - int64(hourlySnapshotRetention.Seconds())); err != nil {
		logger.Logger(c).WithError(err).Warnf("collect hourly stats: cleanup old snapshots failed")
	}
	if err := dao.NewMutation(c).AdminDeleteProxyStatTicksBefore(now.Unix() - int64(hourlySnapshotRetention.Seconds())); err != nil {
		logger.Logger(c).WithError(err).Warnf("collect hourly stats: cleanup old stat ticks failed")
	}

	logger.Logger(c).Infof("collect hourly stats success, snapshots: %d, hourTs: %d", len(snaps), hourTs)
}

// buildHourlySnapshot 由一条实时统计构造整点快照。
//
// 关键点：行内 today_* 的自然日归属以 UpdatedAt 为准。跨天后的首次推送会把
// 昨日 today 翻转入 history，但 00:00 整点的快照定时任务可能与该推送存在
// 分钟级竞态——若照写，昨日全天流量会被记成"今日 00:00 快照"，趋势图首点
// 出现巨幅尖峰、后续小时差分全被压成 0（表现为趋势时好时坏）。
// 因此行未跨天翻转时（UpdatedAt 不在今天），统一按翻转已发生的口径合成：
// today=0、history=history+today，与"今日 00:00 的全期基线"语义严格一致，
// 跨零点闭合差分（昨日 24:00 全期累计 = 次日 history）不受影响。
func buildHourlySnapshot(l *models.ProxyStatsEntity, hourTs int64, now time.Time) *models.ProxyStatsHourly {
	todayIn, todayOut := l.TodayTrafficIn, l.TodayTrafficOut
	historyIn, historyOut := l.HistoryTrafficIn, l.HistoryTrafficOut
	if !utils.IsSameDay(now, l.UpdatedAt) {
		todayIn, todayOut = 0, 0
		historyIn += l.TodayTrafficIn
		historyOut += l.TodayTrafficOut
	}
	return &models.ProxyStatsHourly{
		ProxyID:    l.ProxyID,
		HourTs:     hourTs,
		TodayIn:    todayIn,
		TodayOut:   todayOut,
		HistoryIn:  lo.ToPtr(historyIn),
		HistoryOut: lo.ToPtr(historyOut),
		Name:       l.Name,
		Type:       l.Type,
		ServerID:   l.ServerID,
		ClientID:   l.ClientID,
		UserID:     l.UserID,
		TenantID:   l.TenantID,
	}
}
