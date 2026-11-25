// 首页流量统计的时段计算：全项目统一按北京时间（UTC+8）切日，
// 不依赖浏览器/部署机的本地时区（中国固定 +8、无夏令时）
export type Period = 'today' | 'yesterday' | 'week' | 'month' | 'year' | 'custom'

/** 北京时间偏移分钟（固定 +480），下发给后端做统一的切日口径 */
export const TZ_OFFSET = 480

/** 北京偏移毫秒：时刻 +8h 后按 UTC 读取即可得到北京的日历分量 */
const BJ_MS = TZ_OFFSET * 60_000

export interface TimeRange {
  /** 起始 unix 秒（北京该日 00:00） */
  start: number
  /** 结束 unix 秒（北京该日 23:59:59） */
  end: number
}

const DAY_SECONDS = 24 * 3600

/** 某时刻所在"北京日"的 00:00（unix 毫秒） */
function startOfDay(d: Date): Date {
  const b = new Date(d.getTime() + BJ_MS)
  return new Date(Date.UTC(b.getUTCFullYear(), b.getUTCMonth(), b.getUTCDate()) - BJ_MS)
}

/** 某时刻所在"北京日"的 23:59:59（unix 毫秒） */
function endOfDay(d: Date): Date {
  return new Date(startOfDay(d).getTime() + DAY_SECONDS * 1000 - 1000)
}

function fmtInput(d: Date): string {
  const b = new Date(d.getTime() + BJ_MS)
  const m = `${b.getUTCMonth() + 1}`.padStart(2, '0')
  const day = `${b.getUTCDate()}`.padStart(2, '0')
  return `${b.getUTCFullYear()}-${m}-${day}`
}

/** Date -> yyyy-MM-dd（用于日期输入框，按北京日历分量） */
export const toDateInput = (d: Date): string => fmtInput(d)

/** yyyy-MM-dd -> 北京该日 00:00 的 unix 秒 */
const parseStart = (s: string): number => {
  const [y, m, d] = s.split('-').map(Number)
  return Math.floor((Date.UTC(y, m - 1, d) - BJ_MS) / 1000)
}

/** yyyy-MM-dd -> 北京该日 23:59:59 的 unix 秒 */
const parseEnd = (s: string): number => parseStart(s) + DAY_SECONDS - 1

/**
 * 计算预设时段的起止（unix 秒，按北京时间）
 * - today: 今天
 * - yesterday: 昨天
 * - week: 近七天（含今天往前 7 个自然日）
 * - month: 本月 1 日至今
 * - year: 本年 1 月 1 日至今
 */
export function getPeriodRange(period: Exclude<Period, 'custom'>): TimeRange {
  const todayMs = startOfDay(new Date()).getTime()
  switch (period) {
    case 'today':
      return { start: Math.floor(todayMs / 1000), end: Math.floor((todayMs + DAY_SECONDS * 1000 - 1000) / 1000) }
    case 'yesterday': {
      const y = new Date(todayMs - DAY_SECONDS * 1000)
      return { start: Math.floor(y.getTime() / 1000), end: Math.floor((y.getTime() + DAY_SECONDS * 1000 - 1000) / 1000) }
    }
    case 'week': {
      // 近七天：含今天往前 7 个自然日（周几不再影响区间）
      const start7 = new Date(todayMs - 6 * DAY_SECONDS * 1000)
      return { start: Math.floor(start7.getTime() / 1000), end: Math.floor((todayMs + DAY_SECONDS * 1000 - 1000) / 1000) }
    }
    case 'month': {
      const b = new Date(todayMs + BJ_MS)
      const first = new Date(Date.UTC(b.getUTCFullYear(), b.getUTCMonth(), 1) - BJ_MS)
      return { start: Math.floor(first.getTime() / 1000), end: Math.floor((todayMs + DAY_SECONDS * 1000 - 1000) / 1000) }
    }
    case 'year': {
      const b = new Date(todayMs + BJ_MS)
      const first = new Date(Date.UTC(b.getUTCFullYear(), 0, 1) - BJ_MS)
      return { start: Math.floor(first.getTime() / 1000), end: Math.floor((todayMs + DAY_SECONDS * 1000 - 1000) / 1000) }
    }
  }
}

/** 自定义起止日期（yyyy-MM-dd，含首尾整天，按北京时间） */
export function getCustomRange(startDate: string, endDate: string): TimeRange {
  return { start: parseStart(startDate), end: parseEnd(endDate) }
}

/** 时段默认对应的自定义日期输入值（默认今天，北京时间） */
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
