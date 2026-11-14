'use client'

import { useTranslation } from 'react-i18next'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { PieChart, Pie, Cell, ResponsiveContainer, Tooltip } from 'recharts'
import { formatBytes } from '@/lib/utils'
import { ChartContainer, ChartTooltip, ChartTooltipContent } from '@/components/ui/chart'

interface TrafficSummaryCardProps {
  totalIn: number
  totalOut: number
}

export function TrafficSummaryCard({ totalIn, totalOut }: TrafficSummaryCardProps) {
  const { t } = useTranslation()
  const total = totalIn + totalOut
  const inRatio = total > 0 ? Math.round((totalIn / total) * 100) : 0
  const outRatio = total > 0 ? 100 - inRatio : 0

  const ratioData = [
    { name: t('home.summary.inbound'), value: totalIn },
    { name: t('home.summary.outbound'), value: totalOut },
  ]

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('home.summary.title')}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="grid grid-cols-3 gap-2 text-center">
          <div>
            <div className="text-xs text-muted-foreground">{t('home.summary.inbound')}</div>
            <div className="text-lg font-bold font-mono">{formatBytes(totalIn)}</div>
          </div>
          <div>
            <div className="text-xs text-muted-foreground">{t('home.summary.outbound')}</div>
            <div className="text-lg font-bold font-mono">{formatBytes(totalOut)}</div>
          </div>
          <div>
            <div className="text-xs text-muted-foreground">{t('home.summary.total')}</div>
            <div className="text-lg font-bold font-mono">{formatBytes(total)}</div>
          </div>
        </div>
        <div className="flex items-center gap-4">
          <ChartContainer config={{}} className="h-[120px] w-[120px] font-mono">
            <ResponsiveContainer width="100%" height="100%">
              <PieChart>
                <Pie data={ratioData} dataKey="value" nameKey="name" innerRadius={30} outerRadius={50} paddingAngle={2}>
                  <Cell fill="hsl(var(--chart-1))" />
                  <Cell fill="hsl(var(--chart-2))" />
                </Pie>
                <ChartTooltip content={<ChartTooltipContent valueFormatter={(v) => formatBytes(Number(v))} />} />
              </PieChart>
            </ResponsiveContainer>
          </ChartContainer>
          <div className="flex flex-col gap-1 text-sm">
            <div className="flex items-center gap-2">
              <span className="inline-block h-2.5 w-2.5 rounded-full" style={{ background: 'hsl(var(--chart-1))' }} />
              {t('home.summary.inRatio')}: <span className="font-mono font-semibold">{inRatio}%</span>
            </div>
            <div className="flex items-center gap-2">
              <span className="inline-block h-2.5 w-2.5 rounded-full" style={{ background: 'hsl(var(--chart-2))' }} />
              {t('home.summary.outRatio')}: <span className="font-mono font-semibold">{outRatio}%</span>
            </div>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
