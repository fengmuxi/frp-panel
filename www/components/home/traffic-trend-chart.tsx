'use client'

import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  CartesianGrid,
  Legend,
  ResponsiveContainer,
} from 'recharts'
import { formatBytes } from '@/lib/utils'
import { ChartContainer, ChartTooltip, ChartTooltipContent } from '@/components/ui/chart'

export interface TrendPoint {
  ts: number
  in: number
  out: number
}

// TrendGranularity 趋势粒度：由时段决定（今日/昨日→小时，本年→月，其余→日）
export type TrendGranularity = 'hour' | 'day' | 'month'

interface TrafficTrendChartProps {
  series: TrendPoint[]
  granularity?: TrendGranularity
}

export function TrafficTrendChart({ series, granularity = 'day' }: TrafficTrendChartProps) {
  const { t } = useTranslation()

  // X 轴/tooltip 格式由粒度显式驱动：小时 → "HH:mm"，月 → "YYYY/MM"，日 → 日期；
  // 未传粒度时按点间距兜底推断
  const data = useMemo(() => {
    if (series.length === 0) {
      return []
    }
    let effective = granularity
    if (series.length > 1 && granularity === 'day') {
      const span = series[series.length - 1].ts - series[0].ts
      if (span > 0 && span < 24 * 3600) {
        effective = 'hour'
      }
    }
    return series.map((p) => ({
      date:
        effective === 'hour'
          ? new Date(p.ts * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
          : effective === 'month'
            ? new Date(p.ts * 1000).toLocaleDateString([], { year: 'numeric', month: '2-digit' })
            : new Date(p.ts * 1000).toLocaleDateString(),
      in: p.in,
      out: p.out,
    }))
  }, [series, granularity])

  const inLabel = t('home.summary.inbound')
  const outLabel = t('home.summary.outbound')

  // 全零序列显式空态：区分"没有数据"与"渲染异常"，避免出现看似空白的坐标轴
  const allZero = series.length > 0 && series.every((p) => p.in === 0 && p.out === 0)

  // 单点（如"今日"刚过零点、或部署当天）时折线图无可画的线段，
  // recharts 的 Area 会整体退化为空白 —— 此时渲染居中的数值圆点视图，
  // 待数据点多于一个后自动恢复折线图
  if (series.length === 1) {
    const only = series[0]
    const timeLabel =
      granularity === 'hour'
        ? new Date(only.ts * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
        : new Date(only.ts * 1000).toLocaleDateString()
    return (
      <Card>
        <CardHeader>
          <CardTitle>{t(granularity === 'hour' ? 'home.trend.title_hour' : 'home.trend.title')}</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="h-[260px] flex flex-col items-center justify-center gap-5">
            <div className="flex items-center gap-10">
              <div className="flex flex-col items-center gap-1.5">
                <span className="h-5 w-5 rounded-full" style={{ background: 'hsl(var(--chart-1))' }} />
                <span className="font-mono text-sm font-semibold">{formatBytes(only.in)}</span>
                <span className="text-xs text-muted-foreground">{inLabel}</span>
              </div>
              <div className="flex flex-col items-center gap-1.5">
                <span className="h-5 w-5 rounded-full" style={{ background: 'hsl(var(--chart-2))' }} />
                <span className="font-mono text-sm font-semibold">{formatBytes(only.out)}</span>
                <span className="text-xs text-muted-foreground">{outLabel}</span>
              </div>
            </div>
            <span className="text-xs text-muted-foreground font-mono">{timeLabel}</span>
          </div>
        </CardContent>
      </Card>
    )
  }

  if (allZero) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>{t('home.trend.title')}</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="h-[260px] flex items-center justify-center text-sm text-muted-foreground">
            {t('home.empty.stats')}
          </div>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>
          {t(
            granularity === 'hour'
              ? 'home.trend.title_hour'
              : granularity === 'month'
                ? 'home.trend.title_month'
                : 'home.trend.title',
          )}
        </CardTitle>
      </CardHeader>
      <CardContent>
        <ChartContainer
          config={{
            in: { label: inLabel, color: 'hsl(var(--chart-1))' },
            out: { label: outLabel, color: 'hsl(var(--chart-2))' },
          }}
          className="h-[260px] w-full font-mono"
        >
          <ResponsiveContainer width="100%" height="100%">
            <AreaChart data={data} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
              <defs>
                <linearGradient id="fillIn" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="5%" stopColor="hsl(var(--chart-1))" stopOpacity={0.5} />
                  <stop offset="95%" stopColor="hsl(var(--chart-1))" stopOpacity={0.05} />
                </linearGradient>
                <linearGradient id="fillOut" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="5%" stopColor="hsl(var(--chart-2))" stopOpacity={0.5} />
                  <stop offset="95%" stopColor="hsl(var(--chart-2))" stopOpacity={0.05} />
                </linearGradient>
              </defs>
              <CartesianGrid strokeDasharray="3 3" vertical={false} />
              <XAxis tickLine={false} axisLine={false} tickMargin={8} dataKey="date" minTickGap={32} />
              <YAxis tickFormatter={(v) => formatBytes(Number(v))} tickLine={false} axisLine={false} width={70} />
              <ChartTooltip content={<ChartTooltipContent valueFormatter={(v) => formatBytes(Number(v))} />} />
              <Legend />
              <Area
                type="monotone"
                dataKey="in"
                name={inLabel}
                stroke="hsl(var(--chart-1))"
                fill="url(#fillIn)"
                strokeWidth={2}
                // 常显数据点：单点（如"今日"时段只有一个日粒度样本）时也能看到图形
                dot={{ r: 3, strokeWidth: 1 }}
                activeDot={{ r: 5 }}
              />
              <Area
                type="monotone"
                dataKey="out"
                name={outLabel}
                stroke="hsl(var(--chart-2))"
                fill="url(#fillOut)"
                strokeWidth={2}
                dot={{ r: 3, strokeWidth: 1 }}
                activeDot={{ r: 5 }}
              />
            </AreaChart>
          </ResponsiveContainer>
        </ChartContainer>
      </CardContent>
    </Card>
  )
}
