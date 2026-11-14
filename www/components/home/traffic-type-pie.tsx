'use client'

import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { PieChart, Pie, Cell, ResponsiveContainer, Legend, Tooltip } from 'recharts'
import { formatBytes } from '@/lib/utils'
import { ChartContainer, ChartTooltip, ChartTooltipContent } from '@/components/ui/chart'
import { TrafficRow } from '@/components/home/traffic-ranking-table'

const PIE_COLORS = [
  'hsl(var(--chart-1))',
  'hsl(var(--chart-2))',
  'hsl(var(--chart-3))',
  'hsl(var(--chart-4))',
  'hsl(var(--chart-5))',
]

interface TrafficTypePieProps {
  items: TrafficRow[]
}

/** 按协议类型聚合流量分布饼图（覆盖全部隧道） */
export function TrafficTypePie({ items }: TrafficTypePieProps) {
  const { t } = useTranslation()

  const data = useMemo(() => {
    const byType = new Map<string, number>()
    for (const it of items) {
      const key = it.type || 'unknown'
      byType.set(key, (byType.get(key) ?? 0) + it.in + it.out)
    }
    return Array.from(byType.entries())
      .map(([type, value]) => ({ name: type, value }))
      .sort((a, b) => b.value - a.value)
  }, [items])

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('home.typeDist.title')}</CardTitle>
      </CardHeader>
      <CardContent>
        {data.length === 0 ? (
          <div className="flex h-[300px] items-center justify-center text-sm text-muted-foreground">
            {t('home.empty.stats')}
          </div>
        ) : (
          <ChartContainer config={{}} className="h-[300px] w-full font-mono">
            <ResponsiveContainer width="100%" height="100%">
              <PieChart>
                <Pie data={data} dataKey="value" nameKey="name" innerRadius={50} outerRadius={85} paddingAngle={2}>
                  {data.map((_, i) => (
                    <Cell key={i} fill={PIE_COLORS[i % PIE_COLORS.length]} />
                  ))}
                </Pie>
                <Tooltip
                  content={<ChartTooltipContent valueFormatter={(v) => formatBytes(Number(v))} />}
                  wrapperClassName="font-mono"
                />
                <Legend />
              </PieChart>
            </ResponsiveContainer>
          </ChartContainer>
        )}
      </CardContent>
    </Card>
  )
}
