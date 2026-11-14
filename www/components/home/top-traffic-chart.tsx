'use client'

import { useTranslation } from 'react-i18next'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, Legend, ResponsiveContainer } from 'recharts'
import { formatBytes } from '@/lib/utils'
import { ChartContainer, ChartTooltip, ChartTooltipContent } from '@/components/ui/chart'
import { TrafficRow } from '@/components/home/traffic-ranking-table'

interface TopTrafficChartProps {
  items: TrafficRow[]
}

/** 隧道流量 Top10 横向条形图（数据需已按流量降序） */
export function TopTrafficChart({ items }: TopTrafficChartProps) {
  const { t } = useTranslation()

  const top = items.slice(0, 10).map((it) => ({
    name: it.name,
    in: it.in,
    out: it.out,
  }))

  const inLabel = t('home.summary.inbound')
  const outLabel = t('home.summary.outbound')

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('home.top.title')}</CardTitle>
      </CardHeader>
      <CardContent>
        <ChartContainer
          config={{
            in: { label: inLabel, color: 'hsl(var(--chart-1))' },
            out: { label: outLabel, color: 'hsl(var(--chart-2))' },
          }}
          className="h-[300px] w-full font-mono"
        >
          <ResponsiveContainer width="100%" height="100%">
            <BarChart data={top} layout="vertical" margin={{ top: 4, right: 16, left: 8, bottom: 4 }}>
              <CartesianGrid strokeDasharray="3 3" horizontal={false} />
              <XAxis type="number" tickFormatter={(v) => formatBytes(Number(v))} tickLine={false} axisLine={false} />
              <YAxis
                type="category"
                dataKey="name"
                width={120}
                tickLine={false}
                axisLine={false}
                tick={{ fontSize: 12 }}
              />
              <ChartTooltip content={<ChartTooltipContent valueFormatter={(v) => formatBytes(Number(v))} />} />
              <Legend />
              <Bar dataKey="in" name={inLabel} stackId="a" fill="hsl(var(--chart-1))" radius={[0, 0, 0, 0]} />
              <Bar dataKey="out" name={outLabel} stackId="a" fill="hsl(var(--chart-2))" radius={[4, 4, 0, 0]} />
            </BarChart>
          </ResponsiveContainer>
        </ChartContainer>
      </CardContent>
    </Card>
  )
}
