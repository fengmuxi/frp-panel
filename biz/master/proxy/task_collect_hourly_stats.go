package proxy

import (
	"time"

	"github.com/VaalaCat/frp-panel/models"
	"github.com/VaalaCat/frp-panel/services/app"
	"github.com/VaalaCat/frp-panel/services/dao"
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

	nowTs := time.Now().Unix()
	hourTs := nowTs - nowTs%3600 // 当前整点

	snaps := make([]*models.ProxyStatsHourly, 0, len(live))
	for _, l := range live {
		if l == nil {
			continue
		}
		snaps = append(snaps, &models.ProxyStatsHourly{
			ProxyID:    l.ProxyID,
			HourTs:     hourTs,
			TodayIn:    l.TodayTrafficIn,
			TodayOut:   l.TodayTrafficOut,
			HistoryIn:  lo.ToPtr(l.HistoryTrafficIn),
			HistoryOut: lo.ToPtr(l.HistoryTrafficOut),
			Name:       l.Name,
			Type:       l.Type,
			ServerID:   l.ServerID,
			ClientID:   l.ClientID,
			UserID:     l.UserID,
			TenantID:   l.TenantID,
		})
	}

	if err := dao.NewMutation(c).SaveHourlySnapshots(snaps); err != nil {
		logger.Logger(c).WithError(err).Error("collect hourly stats: save snapshots failed")
		return
	}

	// 顺带清理过期快照（失败不影响主流程）
	if err := dao.NewMutation(c).DeleteHourlySnapshotsBefore(nowTs - int64(hourlySnapshotRetention.Seconds())); err != nil {
		logger.Logger(c).WithError(err).Warnf("collect hourly stats: cleanup old snapshots failed")
	}

	logger.Logger(c).Infof("collect hourly stats success, snapshots: %d, hourTs: %d", len(snaps), hourTs)
}
