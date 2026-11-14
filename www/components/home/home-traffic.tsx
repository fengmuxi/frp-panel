'use client'

import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import { getProxyTrafficStats } from '@/api/stats'
import { Period, TZ_OFFSET, getCustomRange, getPeriodRange, validateRange } from '@/lib/time-range'
import { PeriodTabs } from '@/components/home/period-tabs'
import { TrafficSummaryCard } from '@/components/home/traffic-summary-card'
import { TrafficTrendChart, TrendGranularity, TrendPoint } from '@/components/home/traffic-trend-chart'
import { TopTrafficChart } from '@/components/home/top-traffic-chart'
import { TrafficTypePie } from '@/components/home/traffic-type-pie'
import { TrafficRankingTable, toTrafficRows } from '@/components/home/traffic-ranking-table'
import { Skeleton } from '@/components/ui/skeleton'
import { Card, CardContent } from '@/components/ui/card'
import { toast } from 'sonner'

/** 首页流量面板：全局时段驱动总览/趋势/Top10/类型分布，排行表自带五时段 */
export function HomeTraffic() {
  const { t } = useTranslation()
  const [period, setPeriod] = useState<Period>('today')
  const [customRange, setCustomRange] = useState(() => getCustomRange(
    new Date().toISOString().slice(0, 10),
    new Date().toISOString().slice(0, 10),
  ))

  const range = useMemo(
    () => (period === 'custom' ? customRange : getPeriodRange(period as Exclude<Period, 'custom'>)),
    [period, customRange],
  )

  const query = useQuery({
    queryKey: ['trafficStats', range.start, range.end, TZ_OFFSET],
    queryFn: () =>
      getProxyTrafficStats({
        startTime: BigInt(range.start),
        endTime: BigInt(range.end),
        withSeries: true,
        tzOffsetMinutes: TZ_OFFSET,
      }),
    staleTime: 30_000,
    // 今日数据来自节点每 30s 的 PushProxyInfo 上报，前台每 30s 轮询保持准实时
    refetchInterval: 30_000,
    retry: 1,
  })

  const onApplyCustom = (startDate: string, endDate: string) => {
    const r = getCustomRange(startDate, endDate)
    const invalid = validateRange(r)
    if (invalid) {
      toast(t(invalid === 'tooLong' ? 'home.range.tooLong' : 'home.range.invalid'))
      return
    }
    setCustomRange(r)
  }

  const rows = useMemo(() => toTrafficRows(query.data?.items ?? []), [query.data])
  const series: TrendPoint[] = useMemo(() => {
    const raw: TrendPoint[] = (query.data?.series ?? []).map((p) => ({
      ts: Number(p.ts ?? 0n),
      in: Number(p.trafficIn ?? 0n),
      out: Number(p.trafficOut ?? 0n),
    }))
    // 本年时段：日粒度点按月聚合（ts 归到每月 1 日），避免 300+ 个点的密集折线
    if (period === 'year') {
      const byMonth = new Map<string, TrendPoint>()
      for (const p of raw) {
        const d = new Date(p.ts * 1000)
        const key = `${d.getFullYear()}-${d.getMonth()}`
        const agg = byMonth.get(key)
        if (agg) {
          agg.in += p.in
          agg.out += p.out
        } else {
          byMonth.set(key, { ...p })
        }
      }
      return [...byMonth.values()].sort((a, b) => a.ts - b.ts)
    }
    return raw
  }, [query.data, period])
  const totalIn = Number(query.data?.totalIn ?? 0n)
  const totalOut = Number(query.data?.totalOut ?? 0n)

  // 趋势图粒度：今日/昨日 → 小时，本年 → 月，其余（本周/本月/自定义）→ 日
  const granularity: TrendGranularity =
    period === 'today' || period === 'yesterday'
      ? 'hour'
      : period === 'year'
        ? 'month'
        : 'day'

  const noData = !query.isPending && rows.length === 0

  return (
    <div className="space-y-4">
      <PeriodTabs value={period} onChange={setPeriod} onApplyCustom={onApplyCustom} />

      {query.isPending ? (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
          <Skeleton className="h-[220px]" />
          <Skeleton className="h-[220px] lg:col-span-2" />
        </div>
      ) : query.isError ? (
        <Card>
          <CardContent className="flex h-40 items-center justify-center text-sm text-muted-foreground">
            {t('home.errors.loadFailed')}
          </CardContent>
        </Card>
      ) : noData ? (
        <Card>
          <CardContent className="flex h-24 items-center justify-center text-sm text-muted-foreground">
            {t('home.empty.stats')}
          </CardContent>
        </Card>
      ) : null}

      {!query.isError && !query.isPending && !noData && (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
          <TrafficSummaryCard totalIn={totalIn} totalOut={totalOut} />
          <div className="lg:col-span-2">
            <TrafficTrendChart series={series} granularity={granularity} />
          </div>
        </div>
      )}

      {!query.isError && !query.isPending && !noData && (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
          <div className="lg:col-span-2">
            <TopTrafficChart items={rows} />
          </div>
          <TrafficTypePie items={rows} />
        </div>
      )}

      <TrafficRankingTable />
    </div>
  )
}
