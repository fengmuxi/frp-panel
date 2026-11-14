'use client'

import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Button } from '@/components/ui/button'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { getProxyTrafficStats } from '@/api/stats'
import { Period, TZ_OFFSET, getPeriodRange } from '@/lib/time-range'
import { formatBytes } from '@/lib/utils'

/** 排行行视图模型 */
export interface TrafficRow {
  id: number
  name: string
  type: string
  in: number
  out: number
}

type RankPeriod = Exclude<Period, 'custom'>

const RANK_PERIODS: RankPeriod[] = ['today', 'yesterday', 'week', 'month', 'year']

/** 将接口响应转换为视图模型行（int64 bigint -> number） */
export function toTrafficRows(items: Array<{ proxyId?: number; name?: string; type?: string; trafficIn?: bigint; trafficOut?: bigint }>): TrafficRow[] {
  return items.map((it) => ({
    id: it.proxyId ?? 0,
    name: it.name ?? '',
    type: it.type ?? '',
    in: Number(it.trafficIn ?? 0n),
    out: Number(it.trafficOut ?? 0n),
  }))
}

interface TrafficRankingTableProps {
  /** 与全局时段一致时共享同一 react-query 缓存 */
  defaultPeriod?: RankPeriod
}

export function TrafficRankingTable({ defaultPeriod = 'today' }: TrafficRankingTableProps) {
  const { t } = useTranslation()
  const [period, setPeriod] = useState<RankPeriod>(defaultPeriod)
  const [showAll, setShowAll] = useState(false)

  const range = useMemo(() => getPeriodRange(period), [period])

  const query = useQuery({
    queryKey: ['trafficStats', range.start, range.end, TZ_OFFSET],
    queryFn: () =>
      getProxyTrafficStats({
        startTime: BigInt(range.start),
        endTime: BigInt(range.end),
        withSeries: false,
        tzOffsetMinutes: TZ_OFFSET,
      }),
    staleTime: 30_000,
    // 与首页总览面板一致，前台每 30s 轮询保持排行数据准实时
    refetchInterval: 30_000,
  })

  const rows = useMemo(() => toTrafficRows(query.data?.items ?? []), [query.data])
  const visible = showAll ? rows : rows.slice(0, 10)
  const hasMore = rows.length > 10

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle>{t('home.ranking.title')}</CardTitle>
        <Tabs value={period} onValueChange={(v) => { setPeriod(v as RankPeriod); setShowAll(false) }}>
          <TabsList>
            {RANK_PERIODS.map((p) => (
              <TabsTrigger key={p} value={p}>
                {t(`home.period.${p}`)}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      </CardHeader>
      <CardContent>
        {query.isPending ? (
          <div className="flex h-40 items-center justify-center text-sm text-muted-foreground">
            {t('home.loading')}
          </div>
        ) : rows.length === 0 ? (
          <div className="flex h-40 items-center justify-center text-sm text-muted-foreground">
            {t('home.empty.stats')}
          </div>
        ) : (
          <>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-14">{t('home.ranking.rank')}</TableHead>
                  <TableHead>{t('home.ranking.name')}</TableHead>
                  <TableHead className="w-24">{t('home.ranking.type')}</TableHead>
                  <TableHead className="text-right">{t('home.ranking.trafficIn')}</TableHead>
                  <TableHead className="text-right">{t('home.ranking.trafficOut')}</TableHead>
                  <TableHead className="text-right">{t('home.ranking.trafficTotal')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {visible.map((row, idx) => (
                  <TableRow key={row.id}>
                    <TableCell className="font-mono text-muted-foreground">{idx + 1}</TableCell>
                    <TableCell className="max-w-[240px] truncate font-medium">{row.name || '-'}</TableCell>
                    <TableCell className="font-mono text-sm">{row.type || '-'}</TableCell>
                    <TableCell className="text-right font-mono">{formatBytes(row.in)}</TableCell>
                    <TableCell className="text-right font-mono">{formatBytes(row.out)}</TableCell>
                    <TableCell className="text-right font-mono font-semibold">{formatBytes(row.in + row.out)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            {hasMore && (
              <div className="mt-3 flex justify-center">
                <Button size="sm" variant="outline" onClick={() => setShowAll((s) => !s)}>
                  {showAll
                    ? t('home.top.showTop10')
                    : t('home.top.showAll', { count: rows.length })}
                </Button>
              </div>
            )}
          </>
        )}
      </CardContent>
    </Card>
  )
}
