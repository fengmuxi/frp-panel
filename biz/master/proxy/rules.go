package proxy

import (
	"fmt"
	"time"

	"github.com/VaalaCat/frp-panel/defs"
)

// ParseHHMM 解析 "HH:MM" 格式时间为自 00:00 起的分钟数（分钟精度）
func ParseHHMM(s string) (int, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, fmt.Errorf("invalid time [%s], expect HH:MM", s)
	}
	return t.Hour()*60 + t.Minute(), nil
}

// ScheduleWindowOpen 判断当前时刻是否处于定时开启窗口内（应开启隧道）。
//
// 规则约定（均为“失效安全”方向，配置异常时不会误关隧道）：
//   - 分钟粒度：窗口为 [start, end)，即开启时刻含、关闭时刻不含；
//   - 支持跨天：start > end 表示跨午夜（如 22:00-06:00），跨天段归属开始日期所在的周几；
//   - 周几多选：days 为生效周几（0=周日 ~ 6=周六），当天不在列表中则窗口不生效；
//   - days 为空或时间非法解析失败时视为规则不生效（保持开启）；start == end 视为全天开启。
func ScheduleWindowOpen(now time.Time, start, end string, days []int) bool {
	s, err := ParseHHMM(start)
	if err != nil {
		return true
	}
	e, err := ParseHHMM(end)
	if err != nil {
		return true
	}
	if s == e {
		// 相同时间视为全天开启，避免误停
		return true
	}
	if len(days) == 0 {
		// 未选择周几视为规则不生效
		return true
	}

	dayAllowed := func(d time.Weekday) bool {
		for _, day := range days {
			if day == int(d) {
				return true
			}
		}
		return false
	}

	minutes := now.Hour()*60 + now.Minute()

	if s < e {
		// 同一天内的窗口
		if !dayAllowed(now.Weekday()) {
			return false
		}
		return minutes >= s && minutes < e
	}

	// 跨天窗口：[s, 24:00) 属于当天，[00:00, e) 属于前一天开始的窗口
	if minutes >= s {
		return dayAllowed(now.Weekday())
	}
	if minutes < e {
		return dayAllowed(now.AddDate(0, 0, -1).Weekday())
	}
	return false
}

// QuotaWindowStart 返回流量限额统计窗口的起点（本地时区）：
// day=今天 00:00，week=本周一 00:00，month=本月 1 日 00:00；未知类型按 day 处理。
func QuotaWindowStart(now time.Time, quotaType string) time.Time {
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch quotaType {
	case defs.QuotaTypeWeek:
		// Go 的 Weekday: 0=周日；换算成“距本周一的天数”
		offset := (int(now.Weekday()) + 6) % 7
		return day.AddDate(0, 0, -offset)
	case defs.QuotaTypeMonth:
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	default: // defs.QuotaTypeDay
		return day
	}
}

// QuotaTypeValid 校验限额类型取值（空串表示未启用）
func QuotaTypeValid(quotaType string) bool {
	switch quotaType {
	case "", defs.QuotaTypeDay, defs.QuotaTypeWeek, defs.QuotaTypeMonth:
		return true
	}
	return false
}

// QuotaDirectionValid 校验限额统计方向取值（空串表示双向合计，兼容旧数据）
func QuotaDirectionValid(direction string) bool {
	switch direction {
	case "", defs.QuotaDirectionBoth, defs.QuotaDirectionInbound, defs.QuotaDirectionOutbound:
		return true
	}
	return false
}

// NormalizeScheduleDays 规范化周几列表：过滤 0-6 以外的值并去重
func NormalizeScheduleDays(days []int) []int {
	seen := make(map[int]bool, len(days))
	result := make([]int, 0, len(days))
	for _, d := range days {
		if d < 0 || d > 6 || seen[d] {
			continue
		}
		seen[d] = true
		result = append(result, d)
	}
	return result
}
