// 首页流量统计的时段计算（按浏览器本地时区切日）
export type Period = 'today' | 'yesterday' | 'week' | 'month' | 'year' | 'custom'

/** 前端时区偏移分钟数（东八区 = +480），与后端按此切日对齐 */
export const TZ_OFFSET = -new Date().getTimezoneOffset()

export interface TimeRange {
  /** 起始 unix 秒（该日 00:00） */
  start: number
  /** 结束 unix 秒（该日 23:59:59） */
  end: number
}

const DAY_SECONDS = 24 * 3600

function startOfDay(d: Date): Date {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate())
}

function endOfDay(d: Date): Date {
  const s = startOfDay(d)
  return new Date(s.getTime() + DAY_SECONDS * 1000 - 1000)
}

function fmtInput(d: Date): string {
  const m = `${d.getMonth() + 1}`.padStart(2, '0')
  const day = `${d.getDate()}`.padStart(2, '0')
  return `${d.getFullYear()}-${m}-${day}`
}

/** Date -> yyyy-MM-dd（用于日期输入框） */
export const toDateInput = (d: Date) => fmtInput(d)

/** yyyy-MM-dd -> 当日 23:59:59 的 unix 秒 */
const parseEnd = (s: string): number => {
  const [y, m, d] = s.split('-').map(Number)
  return Math.floor(endOfDay(new Date(y, m - 1, d)).getTime() / 1000)
}

/** yyyy-MM-dd -> 当日 00:00 的 unix 秒 */
const parseStart = (s: string): number => {
  const [y, m, d] = s.split('-').map(Number)
  return Math.floor(startOfDay(new Date(y, m - 1, d)).getTime() / 1000)
}

/**
 * 计算预设时段的起止（unix 秒）
 * - today: 今天
 * - yesterday: 昨天
 * - week: 本周一至今
 * - month: 本月 1 日至今
 * - year: 本年 1 月 1 日至今
 */
export function getPeriodRange(period: Exclude<Period, 'custom'>): TimeRange {
  const now = new Date()
  const today = startOfDay(now)
  switch (period) {
    case 'today':
      return { start: Math.floor(today.getTime() / 1000), end: Math.floor(endOfDay(now).getTime() / 1000) }
    case 'yesterday': {
      const y = new Date(today.getTime() - DAY_SECONDS * 1000)
      return { start: Math.floor(y.getTime() / 1000), end: Math.floor(endOfDay(y).getTime() / 1000) }
    }
    case 'week': {
      const monday = new Date(today.getTime() - (now.getDay() === 0 ? 6 : now.getDay() - 1) * DAY_SECONDS * 1000)
      return { start: Math.floor(monday.getTime() / 1000), end: Math.floor(endOfDay(now).getTime() / 1000) }
    }
    case 'month': {
      const first = new Date(now.getFullYear(), now.getMonth(), 1)
      return { start: Math.floor(first.getTime() / 1000), end: Math.floor(endOfDay(now).getTime() / 1000) }
    }
    case 'year': {
      const first = new Date(now.getFullYear(), 0, 1)
      return { start: Math.floor(first.getTime() / 1000), end: Math.floor(endOfDay(now).getTime() / 1000) }
    }
  }
}

/** 自定义起止日期（yyyy-MM-dd，含首尾整天） */
export function getCustomRange(startDate: string, endDate: string): TimeRange {
  return { start: parseStart(startDate), end: parseEnd(endDate) }
}

/** 时段默认对应的自定义日期输入值（默认今天） */
export function defaultCustomDates(): { startDate: string; endDate: string } {
  const today = fmtInput(new Date())
  return { startDate: today, endDate: today }
}

/** 校验区间：start <= end 且不超过 400 天 */
export function validateRange(range: TimeRange): 'invalid' | 'tooLong' | null {
  if (range.start > range.end) return 'invalid'
  if (range.end - range.start > 400 * DAY_SECONDS) return 'tooLong'
  return null
}
