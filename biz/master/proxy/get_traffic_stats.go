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

	// 小时粒度时段（今日/昨日）：用小时快照差分替换单点日粒度序列，
	// 让趋势图呈现逐时走势。区间完全落在 [昨天00:00, 今天] 内才启用；
	// 无可用快照（部署当天首个整点前 / 旧数据）时保持原单点回退。
	todayDay := startOfDayUnix(now.Unix(), loc)
	startDay := startOfDayUnix(start, loc)
	endDay := startOfDayUnix(end, loc)
	isHourRange := endDay == todayDay && (startDay == todayDay || startDay == todayDay-daySeconds)
	if req.GetWithSeries() && isHourRange {
		hourly, err := dao.NewQuery(c).GetHourlySnapshotsByUser(userInfo, startDay, now.Unix())
		if err != nil {
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
