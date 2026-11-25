"use client"

import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { formatBytes } from '@/lib/utils'
import { getProxyStatsByClientID } from '@/api/stats'
import { getProxyTrafficStats } from '@/api/stats'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'

export interface ProxyTrafficDetailDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  clientID: string
  serverID: string
  proxyName: string
  proxyType?: string
}

// 某日 00:00 的北京时间 unix 秒（统一北京时间，不依赖浏览器时区）
const BJ_MS = 480 * 60_000
function startOfDay(offsetDays = 0): number {
  const b = new Date(Date.now() + BJ_MS)
  const dayUtc = Date.UTC(b.getUTCFullYear(), b.getUTCMonth(), b.getUTCDate() - offsetDays)
  return Math.floor((dayUtc - BJ_MS) / 1000)
}

export const ProxyTrafficDetailDialog: React.FC<ProxyTrafficDetailDialogProps> = ({
  open,
  onOpenChange,
  clientID,
  serverID,
  proxyName,
  proxyType,
}) => {
  const { t } = useTranslation()

	// 概览数据：按 client 拉取后在 proxyInfos 里定位本条隧道。
	// staleTime=0 保证每次打开弹框都重新拉取最新数据；打开期间 30s 轮询保持准实时
	const overviewQuery = useQuery({
		queryKey: ['proxyTrafficOverview', clientID],
		queryFn: () => getProxyStatsByClientID({ clientId: clientID }),
		enabled: open && !!clientID,
		staleTime: 0,
		refetchInterval: open ? 30_000 : false,
	})

  // 区间统计：近 7 天 / 近 30 天，traffic_stats 返回的 items 按 name 匹配
  const rangeQueries = useMemo(
    () => [
      { days: 7, key: 'range7d' },
      { days: 30, key: 'range30d' },
    ],
    [],
  )

  const overview = overviewQuery.data?.proxyInfos?.find(
    (p) => p.name === proxyName && (!p.serverId || p.serverId === serverID),
  )

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {t('traffic.chart.title')} · <span className="font-mono">{proxyName}</span>
          </DialogTitle>
          <DialogDescription>
            {proxyType ? `${proxyType.toUpperCase()} · ` : ''}
            {t('proxy.traffic.detail_hint')}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <Section title={t('traffic.today.total')}>
            <StatPair
              labelIn={t('traffic.chart.inbound')}
              labelOut={t('traffic.chart.outbound')}
              inBytes={Number(overview?.todayTrafficIn ?? 0n)}
              outBytes={Number(overview?.todayTrafficOut ?? 0n)}
              loading={overviewQuery.isLoading}
            />
          </Section>

          <Section title={t('traffic.history.total')}>
            <StatPair
              labelIn={t('traffic.chart.inbound')}
              labelOut={t('traffic.chart.outbound')}
              inBytes={Number(overview?.historyTrafficIn ?? 0n)}
              outBytes={Number(overview?.historyTrafficOut ?? 0n)}
              loading={overviewQuery.isLoading}
            />
          </Section>

          {rangeQueries.map(({ days, key }) => (
            <ProxyRangeStats
              key={key}
              days={days}
              clientID={clientID}
              serverID={serverID}
              proxyName={proxyName}
              open={open}
            />
          ))}
        </div>
      </DialogContent>
    </Dialog>
  )
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="rounded-lg border p-3">
      <div className="text-sm font-medium mb-2">{title}</div>
      {children}
    </div>
  )
}

function StatPair({
  labelIn,
  labelOut,
  inBytes,
  outBytes,
  loading,
}: {
  labelIn: string
  labelOut: string
  inBytes: number
  outBytes: number
  loading?: boolean
}) {
  if (loading) {
    return <div className="text-sm text-muted-foreground">loading…</div>
  }
  return (
    <div className="grid grid-cols-2 gap-2 text-sm">
      <div className="rounded-md bg-muted/40 px-3 py-2">
        <div className="text-xs text-muted-foreground">{labelIn}</div>
        <div className="font-mono text-nowrap">↓ {formatBytes(inBytes)}</div>
      </div>
      <div className="rounded-md bg-muted/40 px-3 py-2">
        <div className="text-xs text-muted-foreground">{labelOut}</div>
        <div className="font-mono text-nowrap">↑ {formatBytes(outBytes)}</div>
      </div>
    </div>
  )
}

// 近 N 天区间统计：traffic_stats 按时间范围返回 items（含每条隧道的区间总量）
function ProxyRangeStats({
  days,
  proxyName,
  clientID,
  serverID,
  open,
}: {
  days: number
  proxyName: string
  clientID: string
  serverID: string
  open: boolean
}) {
  const { t } = useTranslation()

  const { data, isLoading } = useQuery({
    // key 带 clientID/serverID：不同客户端上的同名隧道不得共享缓存
    //（审计 P2-J：此前同名隧道会取到别的隧道的区间流量）
    queryKey: ['proxyTrafficRange', clientID, serverID, proxyName, days],
    queryFn: () =>
      getProxyTrafficStats({
        startTime: BigInt(startOfDay(days - 1)),
        endTime: BigInt(Math.floor(Date.now() / 1000)),
        withSeries: false,
        // 统一北京时间（与上方 startOfDay 的 BJ_MS 口径一致）
        tzOffsetMinutes: 480,
      }),
    enabled: open,
    // 与概览查询一致：每次打开重新拉取 + 打开期间 30s 轮询
    staleTime: 0,
    refetchInterval: open ? 30_000 : false,
  })

  // traffic_stats 的 item 不含 serverId：同名隧道时以第一条近似（缓存 key 已按
  // clientID/serverID 隔离，不会跨客户端串数据）
  const item = data?.items?.find((it) => it.name === proxyName)
  const inBytes = Number(item?.trafficIn ?? 0n)
  const outBytes = Number(item?.trafficOut ?? 0n)

  return (
    <Section title={t('proxy.traffic.last_n_days', { n: days })}>
      <StatPair
        labelIn={t('traffic.chart.inbound')}
        labelOut={t('traffic.chart.outbound')}
        inBytes={inBytes}
        outBytes={outBytes}
        loading={isLoading}
      />
    </Section>
  )
}
