package dao

// proxy_stats 的今日流量补偿计算。
//
// 背景与口径：
//   - frps 内存中的当日计数（frp mem DateCounter，按 frps 本地自然日切分）经节点
//     每 30s PushProxyInfo 上报，master 侧覆盖写入 proxy_stats.today_*；
//   - frps 进程重启会清零内存计数，直接覆盖写会导致"今日"丢失重启前的部分；
//   - 本文件维护增量口径：master 记住上次收到的 frps 内存值（ReportedTodayIn/Out），
//     用 (reported - lastReported) 作为增量累加到今日累计上；
//     检测到上报值回退（frps.today < lastReported）即判定 frps 重启，
//     此时重启前的流量已在今日累计中，把当前内存值整体累加即可无缝衔接。
//
// 各分支的语义见 computeProxyStatsDailyUpdate 的注释。

// proxyStatsDailyUpdate 是对一条 proxy_stats 行今日/历史字段的一次增量计算结果。
type proxyStatsDailyUpdate struct {
	TodayIn     int64 // 新的今日入站累计
	TodayOut    int64 // 新的今日出站累计
	HistoryIn   int64 // 新的历史入站累计
	HistoryOut  int64 // 新的历史出站累计
	ReportedIn  int64 // 本次上报的 frps 内存值（写回 ReportedTodayIn 供下次回退检测）
	ReportedOut int64
}

// computeProxyStatsDailyUpdate 计算一次 PushProxyInfo 上报后的今日/历史字段。
//
// 参数：
//   - oldTodayIn/Out, oldHistoryIn/Out：DB 中现存值
//   - oldReportedIn/Out：DB 中记录的上次 frps 内存值；nil 表示补偿字段上线后尚未记录
//   - reportedIn/Out：本次上报的 frps 内存当日计数
//   - firstSync：frps 进程启动后的首次同步（等价于内存计数刚初始化）
//   - isSameDay：上次写入是否发生在同一天（utils.IsSameDay(old.UpdatedAt, now)）
//
// 分支语义：
//  1. 跨天：昨日今日累计滚入历史（与既有行为一致），今日直接采用本次上报值
//     —— frps 的内存计数本身按自然日切分，跨天后即"今天 00:00 以来"；
//  2. 补偿字段缺失（升级首拍）：旧口径的 today 就是上次上报的内存覆盖值，
//     直接采用最新上报值即天然连续，误差仅为一个上报周期内的增量；
//  3. 回退/firstSync（frps 重启）：今日累计保持并累加当前内存值；
//  4. 正常推进：按内存计数增量累加。
//
// 兜底：任何分支出现负值均按 0 处理（防御计数异常，宁可少算不重复算）。
func computeProxyStatsDailyUpdate(
	oldTodayIn, oldTodayOut int64,
	oldHistoryIn, oldHistoryOut int64,
	oldReportedIn, oldReportedOut *int64,
	reportedIn, reportedOut int64,
	firstSync, isSameDay bool,
) proxyStatsDailyUpdate {
	historyIn, historyOut := oldHistoryIn, oldHistoryOut
	if !isSameDay {
		// 跨天：昨日今日累计滚入历史
		historyIn += oldTodayIn
		historyOut += oldTodayOut
	}

	var todayIn, todayOut int64
	switch {
	case !isSameDay:
		// 新的一天：frps 内存计数即"今天 00:00 以来"
		todayIn, todayOut = reportedIn, reportedOut
	case oldReportedIn == nil || oldReportedOut == nil:
		// 补偿字段上线后的首次上报：旧口径 today 等于上次上报的内存值，
		// 最新上报值天然连续
		todayIn, todayOut = reportedIn, reportedOut
	case firstSync || reportedIn < *oldReportedIn || reportedOut < *oldReportedOut:
		// frps 重启/计数回退：重启前的今日流量已累计在 old.today 中，
		// 当前内存值从 0 重新起算，整体累加即可衔接
		todayIn = oldTodayIn + reportedIn
		todayOut = oldTodayOut + reportedOut
	default:
		// 正常推进：按内存计数增量累加
		todayIn = oldTodayIn + reportedIn - *oldReportedIn
		todayOut = oldTodayOut + reportedOut - *oldReportedOut
	}

	if todayIn < 0 {
		todayIn = 0
	}
	if todayOut < 0 {
		todayOut = 0
	}

	return proxyStatsDailyUpdate{
		TodayIn:     todayIn,
		TodayOut:    todayOut,
		HistoryIn:   historyIn,
		HistoryOut:  historyOut,
		ReportedIn:  reportedIn,
		ReportedOut: reportedOut,
	}
}
