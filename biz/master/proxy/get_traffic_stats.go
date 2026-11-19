package proxy

import (
	"context"
	"fmt"
	"time"

	"github.com/VaalaCat/frp-panel/common"
	"github.com/VaalaCat/frp-panel/pb"
	"github.com/VaalaCat/frp-panel/services/app"
	"github.com/VaalaCat/frp-panel/services/dao"
	"github.com/VaalaCat/frp-panel/utils/logger"
	"github.com/samber/lo"
)

const (
	// maxTrafficRangeDays 单次查询最大区间，覆盖"本年"并防止恶意大区间拉爆内存
	maxTrafficRangeDays = 400
	// maxTrafficItems 兜底截断，防止极端隧道数导致 payload 过大
	maxTrafficItems = 1000
	// snapshotQueryBufferDays 快照查询缓冲：起点向前多查数天，容忍 cron 漏天时回退到更早快照
	snapshotQueryBufferDays = 7
)

// GetProxyTrafficStats 按时间区间聚合隧道流量（供首页总览/趋势/排行使用，仅供展示）
func GetProxyTrafficStats(c *app.Context, req *pb.GetProxyTrafficStatsRequest) (*pb.GetProxyTrafficStatsResponse, error) {
	logger.Logger(c).Infof("get proxy traffic stats, req: [%+v]", req)

	var (
		userInfo = common.GetUserInfo(c)
		now      = time.Now()
		tzOffset = int(req.GetTzOffsetMinutes())
	)
	if tzOffset < -14*60 || tzOffset > 14*60 {
		tzOffset = 0
	}

	start, end := req.GetStartTime(), req.GetEndTime()
	if start <= 0 || end <= 0 {
		return nil, fmt.Errorf("request invalid: start_time and end_time required")
	}
	if start > end {
		return nil, fmt.Errorf("request invalid: start_time is after end_time")
	}
	if end > now.Unix() {
		end = now.Unix()
	}
	if end-start > int64(maxTrafficRangeDays)*daySeconds {
		return nil, fmt.Errorf("request invalid: range exceeds %d days", maxTrafficRangeDays)
	}

	// 快照查询窗口：起点向前留缓冲（容忍漏天回退），终点向后覆盖次日凌晨 3 点快照
	loc := time.FixedZone("local", tzOffset*60)
	from := time.Unix(startOfDayUnix(start, loc)-(snapshotQueryBufferDays*daySeconds), 0)
	to := time.Unix(startOfDayUnix(end, loc)+(snapshotQueryBufferDays*daySeconds), 0)

	snaps, err := dao.NewQuery(c).GetHistoryStatsInRange(userInfo, from, to)
	if err != nil {
		logger.Logger(context.Background()).WithError(err).Error("cannot get history stats in range")
		return nil, err
	}

	live, err := dao.NewQuery(c).GetProxyStatsByUser(userInfo)
	if err != nil {
		logger.Logger(context.Background()).WithError(err).Error("cannot get live proxy stats")
		return nil, err
	}

	items, series, totalIn, totalOut := CalcProxyTrafficStats(
		live, snaps, start, end, tzOffset, req.GetWithSeries(), now,
	)

	// 分钟粒度时段（今日/昨日）：优先用推送增量流水按 5 分钟桶聚合（准实时，
	// 随 30s 上报滚动更新）；无 tick 数据（部署初期/刚清库）时回退小时快照差分，
	// 再无则保持日粒度单点。昨日区间需截断到昨日 24:00，避免混入今日增量。
	todayDay := startOfDayUnix(now.Unix(), loc)
	startDay := startOfDayUnix(start, loc)
	endDay := startOfDayUnix(end, loc)
	// 分钟粒度时段（今日/昨日）：start、end 的切日都必须落在 {今天, 昨天} 内。
	// 不能要求 endDay == todayDay —— 纯昨日区间 endDay 是昨天，会被误排除，
	// 导致昨日永远回退为日粒度单点（下方 endDay != todayDay 的 tickTo 截断即为昨日准备）
	isHourRange := startDay <= endDay && endDay <= todayDay && startDay >= todayDay-daySeconds
	if req.GetWithSeries() && isHourRange {
		tickTo := now.Unix()
		if endDay != todayDay {
			tickTo = endDay + daySeconds - 1
		}
		buckets, err := dao.NewQuery(c).AdminGetProxyStatTickBucketsInRange(userInfo, startDay, tickTo, tickSeriesBucketSeconds)
		if err != nil {
			logger.Logger(context.Background()).WithError(err).Warn("cannot get proxy stat ticks, fallback to hourly snapshots")
		} else if tickSeries := CalcTickSeries(buckets, startDay, tickTo); len(tickSeries) > 0 {
			series = tickSeries
		} else if hourly, err := dao.NewQuery(c).GetHourlySnapshotsByUser(userInfo, startDay, now.Unix()); err != nil {
			logger.Logger(context.Background()).WithError(err).Warn("cannot get hourly snapshots, fallback to daily series")
		} else if hourSeries := CalcHourlySeries(live, hourly, startDay, endDay, now); len(hourSeries) > 0 {
			series = hourSeries
		}
	}

	if len(items) > maxTrafficItems {
		items = items[:maxTrafficItems]
	}

	return &pb.GetProxyTrafficStatsResponse{
		Status:   &pb.Status{Code: pb.RespCode_RESP_CODE_SUCCESS, Message: "ok"},
		Items:    items,
		Series:   series,
		TotalIn:  lo.ToPtr(totalIn),
		TotalOut: lo.ToPtr(totalOut),
	}, nil
}
