package proxy

import (
	"sort"
	"time"

	"github.com/VaalaCat/frp-panel/models"
	"github.com/VaalaCat/frp-panel/pb"
	"github.com/samber/lo"
)

// trafficCum 某个日边界时刻的历史累计流量
type trafficCum struct {
	in  int64
	out int64
}

// startOfDayUnix 返回 ts 所在"日"的 00:00 unix 秒（按 loc 时区切日）
func startOfDayUnix(ts int64, loc *time.Location) int64 {
	t := time.Unix(ts, 0).In(loc)
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc).Unix()
}

const daySeconds = int64(24 * 3600)

const hourSeconds = int64(3600)

// CalcHourlySeries 用小时快照差分生成"今日/昨日"时段的小时粒度趋势。
//
// 快照（proxy_stats_hourly）记录整点时刻的当日累计（today）与全期基线（history）。
// 差分按分段口径进行，避免把部署前的历史流量误算进首小时：
//   - 首个快照点：值 = 当日 00:00 → 该整点的累计（today 口径）；
//   - 同一天内的相邻快照：today 口径差分（history 当天恒定，等价于全期差分）；
//   - 跨零点（昨日 23:00 → 今日 00:00）：today 口径在零点重置无法差分，
//     改用全期基线差分（history + today），闭合昨日最后一小时；
//   - 快照缺口（漏采）：同日合并到前一个采样点（总量不丢），跨天缺口跳过；
//   - 末端为今天且落后于当前小时：用 live.today 补一个实时点。
//
// 缺 HistoryIn/Out 的旧快照无法参与跨零点差分，会被丢弃（对应小时无曲线，渐进可用）。
// 返回点按时间升序，ts 为该小时区间的起点整点；没有任何可用快照时返回 nil
// （调用方回退为日粒度单点）。
func CalcHourlySeries(
	live []*models.ProxyStatsEntity,
	snaps []*models.ProxyStatsHourly,
	fromDay, toDay int64,
	now time.Time,
) []*pb.TrafficSeriesPoint {
	type cum struct{ in, out int64 }

	// 两套口径分别聚合（多隧道同整点求和）：
	// today —— 当日 00:00 起的累计；all —— 全期累计（history + today）
	sumToday := map[int64]cum{}
	sumAll := map[int64]cum{}
	for _, s := range snaps {
		if s == nil || s.HourTs < fromDay || s.HourTs > toDay+daySeconds {
			continue // 取 [fromDay, toDay] 内的整点快照，允许次日 00:00 快照闭合末小时
		}
		agg := sumToday[s.HourTs]
		agg.in += s.TodayIn
		agg.out += s.TodayOut
		sumToday[s.HourTs] = agg
		if s.HistoryIn != nil && s.HistoryOut != nil {
			all := sumAll[s.HourTs]
			all.in += *s.HistoryIn + s.TodayIn
			all.out += *s.HistoryOut + s.TodayOut
			sumAll[s.HourTs] = all
		}
	}
	if len(sumToday) == 0 {
		return nil
	}

	// 升序整点边界
	hours := make([]int64, 0, len(sumToday))
	for h := range sumToday {
		hours = append(hours, h)
	}
	sort.Slice(hours, func(i, j int) bool { return hours[i] < hours[j] })

	curHour := now.Unix() - now.Unix()%hourSeconds

	// 分段口径差分：ts 取区间起点，值 = 该小时区间（[ts, 下一边界)）的增量
	var series []*pb.TrafficSeriesPoint
	appendPoint := func(ts int64, in, out int64) {
		if in < 0 {
			in = 0
		}
		if out < 0 {
			out = 0
		}
		series = append(series, &pb.TrafficSeriesPoint{
			Ts:         lo.ToPtr(ts),
			TrafficIn:  lo.ToPtr(in),
			TrafficOut: lo.ToPtr(out),
		})
	}

	var lastSnap int64
	var lastSnapSet bool
	for _, h := range hours {
		var in, out int64
		switch {
		case !lastSnapSet:
			// 首个快照：当日 00:00 → 该整点的累计（today 口径，不含部署前历史）
			in, out = sumToday[h].in, sumToday[h].out
		case h == lastSnap+daySeconds:
			// 跨零点：today 口径已重置，用全期基线差分闭合昨日最后一小时
			if _, ok := sumAll[h]; ok {
				in = sumAll[h].in - sumAll[lastSnap].in
				out = sumAll[h].out - sumAll[lastSnap].out
			} else {
				// 次日 00:00 快照缺全期基线（旧数据），该小时无法计算，跳过
				lastSnap = h
				lastSnapSet = true
				continue
			}
		case h == lastSnap+hourSeconds || h-lastSnap < daySeconds:
			// 同日相邻或同日漏采缺口：today 口径差分（缺口合并到前一点）
			in = sumToday[h].in - sumToday[lastSnap].in
			out = sumToday[h].out - sumToday[lastSnap].out
		default:
			// 跨天缺口超过一天：无法计算，跳过该点
			lastSnap = h
			lastSnapSet = true
			continue
		}
		appendPoint(h, in, out)
		lastSnap = h
		lastSnapSet = true
	}

	// 末端为今天且落后于当前小时：用 live.today 补当前未完成小时的实时点
	if lastSnapSet && toDay == todayDayOf(now) && curHour > lastSnap {
		curIn := liveTodayOf(live, false) - sumToday[lastSnap].in
		curOut := liveTodayOf(live, true) - sumToday[lastSnap].out
		appendPoint(curHour, curIn, curOut)
	}
	return series
}

// liveTodayOf 汇总 live 的当日累计（out=false 取入站，true 取出站）。
func liveTodayOf(live []*models.ProxyStatsEntity, out bool) int64 {
	var sum int64
	for _, l := range live {
		if l == nil {
			continue
		}
		if out {
			sum += l.TodayTrafficOut
		} else {
			sum += l.TodayTrafficIn
		}
	}
	return sum
}

// todayDayOf 返回 now 所在日的 00:00 unix 秒（now 自身时区）。
func todayDayOf(now time.Time) int64 {
	y, m, d := now.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, now.Location()).Unix()
}

// CalcProxyTrafficStats 按时间区间聚合各隧道流量（快照差分，仅供展示、非精确对账）。
//
// 数据语义：
//   - history_proxy_stats 每日 cron 写入一条"截至该时刻的累计值"快照，D 日 03:00 的快照近似 D 日 00:00 的累计值
//   - proxy_stats.today_* 为当日实时增量；history_* 在跨零点首次同步时翻转，故等于"今日 00:00 累计值"
//
// 算法（day 均按前端时区 tz = tzOffsetMinutes 切分，now 用于确定"今天"边界）：
//   - 区间 [D1, D2]，若 D2 含今天：流量 = (cumTodayStart - cum(D1)) + live.today
//   - 否则：流量 = cum(D2+1日) - cum(D1)，其中 cum(D) 取 D 日（含）之前最近一条快照
//   - D2 为昨天时，D2+1 即今天，直接用 cumTodayStart（live.history 或今日快照），无需等待当日 3 点快照
//   - 无任何快照时 cum 回退为 0（部署/隧道创建前的历史流量不在统计内，属可接受近似）
//
// 返回 items 按 (in+out) 降序；series 仅在 withSeries 时返回，按天升序。
func CalcProxyTrafficStats(
	live []*models.ProxyStatsEntity,
	snaps []*models.HistoryProxyStats,
	start, end int64,
	tzOffsetMinutes int,
	withSeries bool,
	now time.Time,
) (items []*pb.ProxyTrafficItem, series []*pb.TrafficSeriesPoint, totalIn, totalOut int64) {
	loc := time.FixedZone("local", tzOffsetMinutes*60)

	todayDay := startOfDayUnix(now.Unix(), loc)
	d1 := startOfDayUnix(start, loc)
	d2 := startOfDayUnix(end, loc)
	if d2 > todayDay {
		d2 = todayDay
	}
	if d1 > d2 {
		d1 = d2
	}

	// 实时统计索引
	liveByPid := make(map[int]*models.ProxyStatsEntity, len(live))
	for _, l := range live {
		if l != nil {
			liveByPid[l.ProxyID] = l
		}
	}

	// 快照索引：pid -> day(00:00 unix) -> 快照（同日多条取最新），并记录排序后的 day 列表
	type snapInfo struct {
		ts        int64
		in, out   int64
		name, typ string
	}
	snapByPid := make(map[int]map[int64]*snapInfo, len(live))
	snapDays := make(map[int][]int64, len(live))
	for _, s := range snaps {
		if s == nil {
			continue
		}
		day := startOfDayUnix(s.CreatedAt.Unix(), loc)
		m := snapByPid[s.ProxyID]
		if m == nil {
			m = make(map[int64]*snapInfo)
			snapByPid[s.ProxyID] = m
		}
		if old, ok := m[day]; !ok || s.CreatedAt.Unix() > old.ts {
			m[day] = &snapInfo{ts: s.CreatedAt.Unix(), in: s.TrafficIn, out: s.TrafficOut, name: s.Name, typ: s.Type}
		}
	}
	for pid, m := range snapByPid {
		days := make([]int64, 0, len(m))
		for day := range m {
			days = append(days, day)
		}
		sort.Slice(days, func(i, j int) bool { return days[i] < days[j] })
		snapDays[pid] = days
	}

	// cumAtDay: day < todayDay 时，取 day（含）之前最近一条快照的累计值；无则为 0
	cumAtDay := func(pid int, day int64) trafficCum {
		m := snapByPid[pid]
		if m == nil {
			return trafficCum{}
		}
		days := snapDays[pid]
		idx := sort.Search(len(days), func(i int) bool { return days[i] > day }) - 1
		if idx < 0 {
			return trafficCum{}
		}
		s := m[days[idx]]
		return trafficCum{in: s.in, out: s.out}
	}

	// cumTodayStart: 今天 00:00 的累计值。live.history 跨零点已翻转即该值；
	// 无 live（已删除隧道）时用今日快照，再无则用最后一条快照，全无则为 0
	cumTodayStart := func(pid int) trafficCum {
		if l, ok := liveByPid[pid]; ok {
			return trafficCum{in: l.HistoryTrafficIn, out: l.HistoryTrafficOut}
		}
		m := snapByPid[pid]
		if m == nil {
			return trafficCum{}
		}
		if s, ok := m[todayDay]; ok {
			return trafficCum{in: s.in, out: s.out}
		}
		days := snapDays[pid]
		if len(days) > 0 {
			s := m[days[len(days)-1]]
			return trafficCum{in: s.in, out: s.out}
		}
		return trafficCum{}
	}

	// 并集：有实时统计或有快照的全部隧道
	pidSet := make(map[int]struct{}, len(liveByPid)+len(snapByPid))
	for pid := range liveByPid {
		pidSet[pid] = struct{}{}
	}
	for pid := range snapByPid {
		pidSet[pid] = struct{}{}
	}

	for pid := range pidSet {
		var base, boundary trafficCum
		if d1 >= todayDay {
			base = cumTodayStart(pid)
		} else {
			base = cumAtDay(pid, d1)
		}

		addToday := false
		if d2 >= todayDay {
			boundary = cumTodayStart(pid)
			addToday = true
		} else if d2+daySeconds >= todayDay {
			// d2 为昨天：其结束边界即今天 00:00
			boundary = cumTodayStart(pid)
		} else {
			boundary = cumAtDay(pid, d2+daySeconds)
		}

		var todayIn, todayOut int64
		if addToday {
			if l, ok := liveByPid[pid]; ok {
				todayIn, todayOut = l.TodayTrafficIn, l.TodayTrafficOut
			}
		}

		in := boundary.in - base.in + todayIn
		out := boundary.out - base.out + todayOut
		if in < 0 {
			in = 0
		}
		if out < 0 {
			out = 0
		}

		// name/type：实时记录优先（反映最新），已删除隧道回退快照值
		var name, typ string
		if l, ok := liveByPid[pid]; ok {
			name, typ = l.Name, l.Type
		} else if m := snapByPid[pid]; m != nil && len(snapDays[pid]) > 0 {
			if s, ok := m[snapDays[pid][len(snapDays[pid])-1]]; ok {
				name, typ = s.name, s.typ
			}
		}

		items = append(items, &pb.ProxyTrafficItem{
			ProxyId:    lo.ToPtr(int32(pid)),
			Name:       lo.ToPtr(name),
			Type:       lo.ToPtr(typ),
			TrafficIn:  lo.ToPtr(in),
			TrafficOut: lo.ToPtr(out),
		})
		totalIn += in
		totalOut += out
	}

	sort.Slice(items, func(i, j int) bool {
		ti := items[i].GetTrafficIn() + items[i].GetTrafficOut()
		tj := items[j].GetTrafficIn() + items[j].GetTrafficOut()
		if ti != tj {
			return ti > tj
		}
		return items[i].GetName() < items[j].GetName()
	})

	// 日粒度趋势序列
	if withSeries {
		for day := d1; day <= d2; day += daySeconds {
			var dayIn, dayOut int64
			if day >= todayDay {
				// 今天：实时当日增量（对全部隧道累加）
				for _, l := range liveByPid {
					dayIn += l.TodayTrafficIn
					dayOut += l.TodayTrafficOut
				}
			} else {
				base := trafficCum{}
				for pid := range pidSet {
					b := cumAtDay(pid, day)
					var next trafficCum
					if day+daySeconds >= todayDay {
						next = cumTodayStart(pid)
					} else {
						next = cumAtDay(pid, day+daySeconds)
					}
					di := next.in - b.in
					do := next.out - b.out
					if di < 0 {
						di = 0
					}
					if do < 0 {
						do = 0
					}
					base.in += di
					base.out += do
				}
				dayIn, dayOut = base.in, base.out
			}
			series = append(series, &pb.TrafficSeriesPoint{
				Ts:         lo.ToPtr(day),
				TrafficIn:  lo.ToPtr(dayIn),
				TrafficOut: lo.ToPtr(dayOut),
			})
		}
	}

	return items, series, totalIn, totalOut
}
