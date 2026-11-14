package proxy

import (
	"testing"
	"time"

	"github.com/VaalaCat/frp-panel/defs"
	"github.com/stretchr/testify/assert"
)

// 2026-01-05 是周一，2026-01-06 是周二，2026-01-11 是周日（用于构造确定的周几场景）
func mustDate(t *testing.T, month time.Month, day, hour, min int) time.Time {
	t.Helper()
	return time.Date(2026, time.January, day, hour, min, 0, 0, time.Local)
}

func TestScheduleWindowOpen(t *testing.T) {
	// 同一天窗口：周一 08:00-20:00，仅周一生效
	days := []int{int(time.Monday)}

	assert.True(t, ScheduleWindowOpen(mustDate(t, time.January, 5, 8, 0), "08:00", "20:00", days), "开启时刻应含")
	assert.True(t, ScheduleWindowOpen(mustDate(t, time.January, 5, 10, 30), "08:00", "20:00", days))
	assert.True(t, ScheduleWindowOpen(mustDate(t, time.January, 5, 19, 59), "08:00", "20:00", days), "关闭前一分钟应开启")
	assert.False(t, ScheduleWindowOpen(mustDate(t, time.January, 5, 7, 59), "08:00", "20:00", days), "开启前一分钟应关闭")
	assert.False(t, ScheduleWindowOpen(mustDate(t, time.January, 5, 20, 0), "08:00", "20:00", days), "关闭时刻应含（右开区间）")
	assert.False(t, ScheduleWindowOpen(mustDate(t, time.January, 6, 10, 0), "08:00", "20:00", days), "周二不在生效周几内")

	// 跨天窗口：22:00-06:00，仅周一生效（跨天段归属开始日）
	crossDays := []int{int(time.Monday)}
	assert.True(t, ScheduleWindowOpen(mustDate(t, time.January, 5, 23, 0), "22:00", "06:00", crossDays), "周一夜里应在窗口内")
	assert.True(t, ScheduleWindowOpen(mustDate(t, time.January, 6, 5, 59), "22:00", "06:00", crossDays), "周二凌晨属于周一开始的窗口")
	assert.False(t, ScheduleWindowOpen(mustDate(t, time.January, 6, 6, 0), "22:00", "06:00", crossDays), "跨天窗口右边界应关闭")
	assert.False(t, ScheduleWindowOpen(mustDate(t, time.January, 6, 23, 0), "22:00", "06:00", crossDays), "周二夜里不在窗口内（仅周一）")
	assert.False(t, ScheduleWindowOpen(mustDate(t, time.January, 5, 5, 0), "22:00", "06:00", crossDays), "周一凌晨承接的是周日，周日不在生效列表")

	// 多选周几：周一、周三、周五
	multiDays := []int{int(time.Monday), int(time.Wednesday), int(time.Friday)}
	assert.True(t, ScheduleWindowOpen(mustDate(t, time.January, 5, 12, 0), "08:00", "20:00", multiDays))
	assert.False(t, ScheduleWindowOpen(mustDate(t, time.January, 6, 12, 0), "08:00", "20:00", multiDays))

	// 失效安全：start == end / 未选周几 / 时间非法 → 视为不关闭
	assert.True(t, ScheduleWindowOpen(mustDate(t, time.January, 5, 12, 0), "08:00", "08:00", days))
	assert.True(t, ScheduleWindowOpen(mustDate(t, time.January, 5, 12, 0), "08:00", "20:00", nil))
	assert.True(t, ScheduleWindowOpen(mustDate(t, time.January, 5, 12, 0), "bad", "20:00", days))
}

func TestQuotaWindowStart(t *testing.T) {
	mondayNoon := mustDate(t, time.January, 5, 12, 30)

	// 日窗口：今天 00:00
	dayStart := QuotaWindowStart(mondayNoon, defs.QuotaTypeDay)
	assert.Equal(t, 0, dayStart.Hour())
	assert.Equal(t, 5, dayStart.Day())

	// 周窗口：本周一 00:00（周一当天 → 今天；周二 → 本周一；周日 → 本周一）
	weekStart := QuotaWindowStart(mondayNoon, defs.QuotaTypeWeek)
	assert.Equal(t, 5, weekStart.Day())
	weekStartTue := QuotaWindowStart(mustDate(t, time.January, 6, 1, 0), defs.QuotaTypeWeek)
	assert.Equal(t, 5, weekStartTue.Day())
	weekStartSun := QuotaWindowStart(mustDate(t, time.January, 11, 23, 0), defs.QuotaTypeWeek)
	assert.Equal(t, 5, weekStartSun.Day())

	// 月窗口：本月 1 日 00:00
	monthStart := QuotaWindowStart(mondayNoon, defs.QuotaTypeMonth)
	assert.Equal(t, 1, monthStart.Day())
	assert.Equal(t, time.January, monthStart.Month())
}

func TestParseHHMM(t *testing.T) {
	minutes, err := ParseHHMM("08:30")
	assert.NoError(t, err)
	assert.Equal(t, 8*60+30, minutes)

	_, err = ParseHHMM("25:00")
	assert.Error(t, err)
	_, err = ParseHHMM("0830")
	assert.Error(t, err)
}

func TestNormalizeScheduleDays(t *testing.T) {
	got := NormalizeScheduleDays([]int{1, 6, 1, -1, 7, 0})
	assert.ElementsMatch(t, []int{0, 1, 6}, got)
	assert.Empty(t, NormalizeScheduleDays(nil))
}
