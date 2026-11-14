package proxy

import (
	"testing"
	"time"

	"github.com/VaalaCat/frp-panel/models"
)

var (
	testLoc = time.FixedZone("test", 8*3600) // 东八区，tzOffsetMinutes = +480
	// 固定"今天"：2026-11-13 10:00 (UTC+8)
	testNow = time.Date(2026, 11, 13, 10, 0, 0, 0, testLoc)
)

func dayAt(d int) time.Time { // 2026-11-d 的 00:00 (UTC+8)
	return time.Date(2026, 11, d, 0, 0, 0, 0, testLoc)
}

func snap(proxyID int, ts time.Time, in, out int64) *models.HistoryProxyStats {
	s := &models.HistoryProxyStats{
		ProxyID:    proxyID,
		Name:       "p",
		TrafficIn:  in,
		TrafficOut: out,
	}
	s.CreatedAt = ts
	return s
}

// 用例1：今日区间 = live.today
func TestCalcTodayEqualsLiveToday(t *testing.T) {
	live := []*models.ProxyStatsEntity{
		{ProxyID: 1, Name: "tunnel-a", Type: "tcp", TodayTrafficIn: 100, TodayTrafficOut: 50, HistoryTrafficIn: 1000, HistoryTrafficOut: 500},
	}
	items, _, tin, tout := CalcProxyTrafficStats(live, nil, testNow.Unix(), testNow.Unix(), 480, false, testNow)
	if len(items) != 1 {
		t.Fatalf("expect 1 item, got %d", len(items))
	}
	if items[0].GetTrafficIn() != 100 || items[0].GetTrafficOut() != 50 {
		t.Fatalf("today traffic expect 100/50, got %d/%d", items[0].GetTrafficIn(), items[0].GetTrafficOut())
	}
	if tin != 100 || tout != 50 {
		t.Fatalf("total expect 100/50, got %d/%d", tin, tout)
	}
}

// 用例2：昨日 = 快照(昨日03:00) 差分到 live.history（今天00:00累计），无需今日快照
func TestCalcYesterdaySnapshotDiff(t *testing.T) {
	live := []*models.ProxyStatsEntity{
		{ProxyID: 1, Name: "tunnel-a", Type: "tcp", TodayTrafficIn: 7, TodayTrafficOut: 3, HistoryTrafficIn: 300, HistoryTrafficOut: 200},
	}
	snaps := []*models.HistoryProxyStats{
		snap(1, dayAt(11).Add(3*time.Hour), 100, 40),  // 11日 03:00：11日00:00累计
		snap(1, dayAt(12).Add(3*time.Hour), 240, 140), // 12日 03:00：12日00:00累计（昨日00:00）
	}
	// 昨日 = 12日
	start, end := dayAt(12).Unix(), dayAt(12).Add(23*time.Hour+59*time.Minute).Unix()
	items, _, tin, tout := CalcProxyTrafficStats(live, snaps, start, end, 480, false, testNow)
	if len(items) != 1 {
		t.Fatalf("expect 1 item, got %d", len(items))
	}
	// boundary = cumTodayStart = live.history(300/200)，base = 快照12日(240/140) → 60/60
	if items[0].GetTrafficIn() != 60 || items[0].GetTrafficOut() != 60 {
		t.Fatalf("yesterday traffic expect 60/60, got %d/%d", items[0].GetTrafficIn(), items[0].GetTrafficOut())
	}
	if tin != 60 || tout != 60 {
		t.Fatalf("total expect 60/60, got %d/%d", tin, tout)
	}
}

// 用例3：隧道创建于区间中段（无 D1 日快照）→ base 回退到更早快照或 0，不为负
func TestCalcNewTunnelInMiddleOfRange(t *testing.T) {
	snaps := []*models.HistoryProxyStats{
		// 仅 12 日起有快照（隧道 12 日才出现），查询 10~13 日
		snap(2, dayAt(12).Add(3*time.Hour), 50, 30),
		snap(3, dayAt(13).Add(3*time.Hour), 80, 60),
	}
	start, end := dayAt(10).Unix(), dayAt(13).Add(23*time.Hour).Unix()
	items, _, tin, _ := CalcProxyTrafficStats(nil, snaps, start, end, 480, false, testNow)
	byPid := map[int]int64{}
	for _, it := range items {
		byPid[int(it.GetProxyId())] = it.GetTrafficIn()
	}
	// pid2：base=0（10日无快照），d2=13=今天 → boundary=cumTodayStart=无live无今日快照→回退最后快照(12日=50) → in=50
	if byPid[2] != 50 {
		t.Fatalf("pid2 expect 50, got %d", byPid[2])
	}
	if tin <= 0 {
		t.Fatalf("total should be positive, got %d", tin)
	}
	for _, it := range items {
		if it.GetTrafficIn() < 0 || it.GetTrafficOut() < 0 {
			t.Fatalf("traffic should never be negative")
		}
	}
}

// 用例4：无任何数据 → 空 items；今日区间序列返回单个 0 值点（图表有内容）
func TestCalcEmpty(t *testing.T) {
	items, series, tin, tout := CalcProxyTrafficStats(nil, nil, testNow.Unix(), testNow.Unix(), 480, true, testNow)
	if len(items) != 0 || tin != 0 || tout != 0 {
		t.Fatalf("expect empty items, got items=%d tin=%d", len(items), tin)
	}
	if len(series) != 1 || series[0].GetTrafficIn() != 0 || series[0].GetTrafficOut() != 0 {
		t.Fatalf("expect single zero point, got %d", len(series))
	}
}

// 用例5：cron 漏天（D1 日无快照）→ cum 回退到 D1 之前最近一条快照，总量守恒
func TestCalcMissingSnapshotFallback(t *testing.T) {
	snaps := []*models.HistoryProxyStats{
		snap(1, dayAt(9).Add(3*time.Hour), 10, 10),    // 9日快照（D1=11 日缺失，回退到 9 日）
		snap(1, dayAt(10).Add(3*time.Hour), 20, 20),   // 10日快照
		snap(1, dayAt(12).Add(3*time.Hour), 60, 60),   // 12日快照
		snap(1, dayAt(13).Add(3*time.Hour), 100, 100), // 13日(今天)03:00 快照
	}
	start, end := dayAt(11).Unix(), dayAt(11).Add(23*time.Hour).Unix() // 查询 11 日
	items, _, tin, _ := CalcProxyTrafficStats(nil, snaps, start, end, 480, false, testNow)
	if len(items) != 1 {
		t.Fatalf("expect 1 item, got %d", len(items))
	}
	// base = ≤11日最近快照 = 10日(20)，boundary = cum(12日) = 60 → 11 日流量 = 40
	if items[0].GetTrafficIn() != 40 {
		t.Fatalf("fallback traffic expect 40, got %d", items[0].GetTrafficIn())
	}
	_ = tin
}

// 用例6：日序列 + Top 排序（多隧道按 in+out 降序）
func TestCalcSeriesAndSort(t *testing.T) {
	live := []*models.ProxyStatsEntity{
		{ProxyID: 1, Name: "big", Type: "tcp", TodayTrafficIn: 10, HistoryTrafficIn: 500},
		{ProxyID: 2, Name: "small", Type: "udp", TodayTrafficIn: 1, HistoryTrafficIn: 100},
	}
	start := dayAt(12).Unix()
	end := testNow.Unix()
	items, series, _, _ := CalcProxyTrafficStats(live, nil, start, end, 480, true, testNow)
	if len(items) != 2 {
		t.Fatalf("expect 2 items, got %d", len(items))
	}
	if items[0].GetName() != "big" {
		t.Fatalf("expect big first, got %s", items[0].GetName())
	}
	// 12日：base=cum(12日)=无快照0，boundary=cumTodayStart=live.history → big=500；13日(今天)=today=10
	if len(series) != 2 {
		t.Fatalf("expect 2 series points, got %d", len(series))
	}
	if series[0].GetTrafficIn() != 600 { // big 500 + small 100
		t.Fatalf("12日 series expect 600, got %d", series[0].GetTrafficIn())
	}
	if series[1].GetTrafficIn() != 11 { // 今天 10+1
		t.Fatalf("today series expect 11, got %d", series[1].GetTrafficIn())
	}
}

// hourlySnap 构造旧格式快照（无全期基线，无法参与跨天差分）
func hourlySnap(proxyID int, hourTs int64, todayIn, todayOut int64) *models.ProxyStatsHourly {
	return &models.ProxyStatsHourly{
		ProxyID:  proxyID,
		HourTs:   hourTs,
		TodayIn:  todayIn,
		TodayOut: todayOut,
		Name:     "p",
		Type:     "tcp",
	}
}

// hourlySnapWithHistory 构造带全期基线的小时快照（today=当日累计，history=今日00:00 全期累计）
func hourlySnapWithHistory(proxyID int, hourTs int64, historyIn, historyOut, todayIn, todayOut int64) *models.ProxyStatsHourly {
	return &models.ProxyStatsHourly{
		ProxyID:    proxyID,
		HourTs:     hourTs,
		TodayIn:    todayIn,
		TodayOut:   todayOut,
		HistoryIn:  &historyIn,
		HistoryOut: &historyOut,
		Name:       "p",
		Type:       "tcp",
	}
}

// TestCalcHourlySeries 覆盖"今日/昨日"小时粒度趋势的差分逻辑（全期累计口径）。
func TestCalcHourlySeries(t *testing.T) {
	todayDay := time.Date(2026, 11, 13, 0, 0, 0, 0, testLoc).Unix()
	hour := func(h int) int64 { return todayDay + int64(h)*3600 }

	// live：history=5000（今日 00:00 翻转后的全期基线），today=200/300
	live := []*models.ProxyStatsEntity{
		{HistoryTrafficIn: 5000, HistoryTrafficOut: 4000, TodayTrafficIn: 200, TodayTrafficOut: 300},
	}

	t.Run("无快照_返回空_由调用方回退日粒度单点", func(t *testing.T) {
		got := CalcHourlySeries(live, nil, todayDay, todayDay, testNow)
		if len(got) != 0 {
			t.Fatalf("expect empty series, got %d", len(got))
		}
	})

	t.Run("相邻快照差分_末端live补实时点", func(t *testing.T) {
		// 快照：08:00 today=100（当日 00:00 起累计）、09:00 today=150；live.today=200
		// 期望：08:00 首点 = 当日 00:00→08:00 累计 = 100（不含部署前历史）；
		//       09:00 点 = 150-100 = 50（[08,09)）；实时点(10:00) = 200-150 = 50（[09,now)）
		snaps := []*models.ProxyStatsHourly{
			hourlySnapWithHistory(1, hour(8), 5000, 4000, 100, 100),
			hourlySnapWithHistory(1, hour(9), 5000, 4000, 150, 150),
		}
		got := CalcHourlySeries(live, snaps, todayDay, todayDay, testNow)
		if len(got) != 3 {
			t.Fatalf("expect 3 points, got %d: %+v", len(got), got)
		}
		if got[0].GetTs() != hour(8) || got[0].GetTrafficIn() != 100 {
			t.Fatalf("point0 unexpected: %v", got[0])
		}
		if got[1].GetTs() != hour(9) || got[1].GetTrafficIn() != 50 {
			t.Fatalf("point1 unexpected: %v", got[1])
		}
		if got[2].GetTs() != hour(10) || got[2].GetTrafficIn() != 50 || got[2].GetTrafficOut() != 150 {
			t.Fatalf("point2 unexpected: %v", got[2])
		}
	})

	t.Run("快照整点恰为当前小时_补实时点", func(t *testing.T) {
		// 快照：09:00 today=150、10:00 today=200（恰为当前小时整点）；live.today=200
		// 期望：09:00 首点 = 150；10:00 点 = 200-150 = 50；当前小时已含在 10:00 快照内，不补点
		snaps := []*models.ProxyStatsHourly{
			hourlySnapWithHistory(1, hour(9), 5000, 4000, 150, 150),
			hourlySnapWithHistory(1, hour(10), 5000, 4000, 200, 300),
		}
		got := CalcHourlySeries(live, snaps, todayDay, todayDay, testNow)
		if len(got) != 2 {
			t.Fatalf("expect 2 points, got %d: %+v", len(got), got)
		}
		if got[0].GetTs() != hour(9) || got[0].GetTrafficIn() != 150 {
			t.Fatalf("point0 unexpected: %v", got[0])
		}
		if got[1].GetTs() != hour(10) || got[1].GetTrafficIn() != 50 {
			t.Fatalf("point1 unexpected: %v", got[1])
		}
	})

	t.Run("漏采小时_区间合并到前一个采样点_不丢总量", func(t *testing.T) {
		// 快照：08:00 today=100、10:00 today=200（09:00 漏采）
		// 期望：08:00 首点 = 100（00:00→08:00）；10:00 点 = 200-100 = 100（[08,10) 合并）
		snaps := []*models.ProxyStatsHourly{
			hourlySnapWithHistory(1, hour(8), 5000, 4000, 100, 100),
			hourlySnapWithHistory(1, hour(10), 5000, 4000, 200, 300),
		}
		got := CalcHourlySeries(live, snaps, todayDay, todayDay, testNow)
		if len(got) != 2 {
			t.Fatalf("expect 2 points, got %d: %+v", len(got), got)
		}
		if got[0].GetTs() != hour(8) || got[0].GetTrafficIn() != 100 || got[0].GetTrafficOut() != 100 {
			t.Fatalf("point0 unexpected: %v", got[0])
		}
		if got[1].GetTs() != hour(10) || got[1].GetTrafficIn() != 100 || got[1].GetTrafficOut() != 200 {
			t.Fatalf("point1 unexpected: %v", got[1])
		}
	})

	t.Run("多隧道同整点求和", func(t *testing.T) {
		// 两条隧道同整点 today 求和 = 150+50 = 200... 实为 150 与 50 分属不同 proxy：
		// sumToday(09:00) = 150+50 = 200？—— 不对，分别为 150 与 50 → 求和 200
		// live.today = 250 → 09:00 首点 = 200；实时点 = 250-200 = 50
		snaps := []*models.ProxyStatsHourly{
			hourlySnapWithHistory(1, hour(9), 5000, 4000, 150, 150),
			hourlySnapWithHistory(2, hour(9), 5000, 4000, 50, 50),
		}
		live := []*models.ProxyStatsEntity{
			{HistoryTrafficIn: 10000, HistoryTrafficOut: 8000, TodayTrafficIn: 250, TodayTrafficOut: 250},
		}
		got := CalcHourlySeries(live, snaps, todayDay, todayDay, testNow)
		if len(got) != 2 {
			t.Fatalf("expect 2 points, got %d: %+v", len(got), got)
		}
		if got[0].GetTs() != hour(9) || got[0].GetTrafficIn() != 200 {
			t.Fatalf("sum point unexpected: %v", got[0])
		}
		if got[1].GetTs() != hour(10) || got[1].GetTrafficIn() != 50 {
			t.Fatalf("live point unexpected: %v", got[1])
		}
	})
}

// TestCalcHourlySeriesCrossDay 覆盖昨日→今日跨零点的小时序列。
func TestCalcHourlySeriesCrossDay(t *testing.T) {
	todayDay := time.Date(2026, 11, 13, 0, 0, 0, 0, testLoc).Unix()
	yesterdayDay := todayDay - 86400
	hourOf := func(day int64, h int) int64 { return day + int64(h)*3600 }

	t.Run("昨日完整小时序列_跨零点由次日00:00快照闭合", func(t *testing.T) {
		// 昨天每小时增量 10：h 点快照 today=10h（当日 00:00 起累计），history 恒定 9770
		// 今天 00:00 快照：history=9770+230=10000（翻转后），today=0
		// 期望 25 个点：
		//   昨日 h0 首点 = today(0) = 0（00:00 整点，尚无流量）
		//   昨日 h1..h23 = today 差分各 10（[00,01)...[22,23)）
		//   今日 h0 跨零点 = 全期(今日00:00)-全期(昨日23:00) = 0（[23,24) 无流量）
		snaps := []*models.ProxyStatsHourly{}
		for h := 0; h <= 23; h++ {
			snaps = append(snaps, hourlySnapWithHistory(1, hourOf(yesterdayDay, h), 9770, 9770, int64(h)*10, int64(h)*10))
		}
		snaps = append(snaps, hourlySnapWithHistory(1, hourOf(todayDay, 0), 10000, 10000, 0, 0))

		live := []*models.ProxyStatsEntity{{TodayTrafficIn: 5, TodayTrafficOut: 5}}
		got := CalcHourlySeries(live, snaps, yesterdayDay, yesterdayDay, testNow)

		if len(got) != 25 {
			t.Fatalf("expect 25 points, got %d: %+v", len(got), got)
		}
		if got[0].GetTs() != hourOf(yesterdayDay, 0) || got[0].GetTrafficIn() != 0 {
			t.Fatalf("first hour unexpected: %v", got[0])
		}
		if got[1].GetTs() != hourOf(yesterdayDay, 1) || got[1].GetTrafficIn() != 10 {
			t.Fatalf("second hour unexpected: %v", got[1])
		}
		// 跨零点点：ts 为今日 00:00，表示昨日 [23:00,24:00) 的增量 0
		last := got[24]
		if last.GetTs() != hourOf(todayDay, 0) || last.GetTrafficIn() != 0 {
			t.Fatalf("cross-midnight point unexpected: %v", last)
		}
		var sum int64
		for _, p := range got {
			sum += p.GetTrafficIn()
		}
		// 昨天全天 230
		if sum != 230 {
			t.Fatalf("sum should be 230, got %d", sum)
		}
	})

	t.Run("旧快照无全期基线_作为首点保留今日区间正常", func(t *testing.T) {
		snaps := []*models.ProxyStatsHourly{
			// 昨日 23:00 的旧快照（HistoryIn=nil）：虽无法参与跨零点差分，
			// 但作为首点仍能给出"昨日 00:00→23:00"的合并累计
			hourlySnap(1, hourOf(yesterdayDay, 23), 230, 230),
			// 今天 00:00 的新快照（带全期基线）
			hourlySnapWithHistory(1, hourOf(todayDay, 0), 10000, 10000, 0, 0),
		}
		// live.today=5
		live := []*models.ProxyStatsEntity{{HistoryTrafficIn: 10000, HistoryTrafficOut: 10000, TodayTrafficIn: 5, TodayTrafficOut: 5}}
		got := CalcHourlySeries(live, snaps, yesterdayDay, todayDay, testNow)
		// 期望 3 点：昨日 23:00 首点（昨日全天合并 230）、
		//          今日 00:00 跨零点（全期差分 0）、实时点（now, 5）
		if len(got) != 3 {
			t.Fatalf("expect 3 points, got %+v", got)
		}
		if got[0].GetTs() != hourOf(yesterdayDay, 23) || got[0].GetTrafficIn() != 230 {
			t.Fatalf("point0 unexpected: %v", got[0])
		}
		if got[1].GetTs() != hourOf(todayDay, 0) || got[1].GetTrafficIn() != 0 {
			t.Fatalf("point1 unexpected: %v", got[1])
		}
		if got[2].GetTs() != testNow.Unix() || got[2].GetTrafficIn() != 5 {
			t.Fatalf("point2 unexpected: %v", got[2])
		}
	})

	t.Run("昨日到今日_跨零点连续", func(t *testing.T) {
		// 区间 [昨日, 今日]：
		//   昨日 22:00 首点 = today(22) = 220（00:00→22:00 累计）
		//   昨日 23:00 = 230-220 = 10（[22,23)）
		//   今日 00:00 = 跨零点全期差分 = (10000)-(9770+230) = 0（[23,24)）
		//   今日 01:00 = today(1)-today(0) = 20（[00,01)）
		//   实时点 = live.today(30)-today(1)(20) = 10（[01,now)）
		snaps := []*models.ProxyStatsHourly{
			hourlySnapWithHistory(1, hourOf(yesterdayDay, 22), 9770, 9770, 220, 220),
			hourlySnapWithHistory(1, hourOf(yesterdayDay, 23), 9770, 9770, 230, 230),
			hourlySnapWithHistory(1, hourOf(todayDay, 0), 10000, 10000, 0, 0),
			hourlySnapWithHistory(1, hourOf(todayDay, 1), 10000, 10000, 20, 20),
		}
		live := []*models.ProxyStatsEntity{
			{HistoryTrafficIn: 10000, HistoryTrafficOut: 10000, TodayTrafficIn: 30, TodayTrafficOut: 30},
		}
		got := CalcHourlySeries(live, snaps, yesterdayDay, todayDay, testNow)
		if len(got) != 5 {
			t.Fatalf("expect 5 points, got %d: %+v", len(got), got)
		}
		if got[0].GetTs() != hourOf(yesterdayDay, 22) || got[0].GetTrafficIn() != 220 {
			t.Fatalf("point0 unexpected: %v", got[0])
		}
		if got[1].GetTrafficIn() != 10 || got[2].GetTrafficIn() != 0 || got[3].GetTrafficIn() != 20 || got[4].GetTrafficIn() != 10 {
			t.Fatalf("unexpected values: %+v", got)
		}
	})
}
