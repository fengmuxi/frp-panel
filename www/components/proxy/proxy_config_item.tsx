import { ColumnDef, Row, Table } from '@tanstack/react-table'
import React from 'react'
import { useTranslation } from 'react-i18next'
import { VisitPreview } from '../base/visit-preview'
import { useQuery } from '@tanstack/react-query'
import { getServer } from '@/api/server'
import { getClient } from '@/api/client'
import { ProxyType, TypedProxyConfig } from '@/types/proxy'
import { ProxyConfigActions } from './proxy_config_actions'
import { ProxyConfig } from '@/lib/pb/common'
import { getProxyConfig } from '@/api/proxy'
import { getProxyStatsByClientID } from '@/api/stats'
import { Badge } from '../ui/badge'
import { useStore } from '@nanostores/react'
import { $proxyTableRefetchTrigger } from '@/store/refetch-trigger'
import { formatBytes } from '@/lib/utils'
import { parseJSONSafe } from '@/lib/safe-parse'

export type ProxyConfigTableSchema = {
  serverID: string
  clientID: string
  name: string
  type: ProxyType
  localIP?: string
  localPort?: number
  visitPreview: string
  config?: string
  originalProxyConfig: ProxyConfig
  stopped: boolean
}

export const columns: ColumnDef<ProxyConfigTableSchema>[] = [
  {
    accessorKey: 'name',
    header: function Header() {
      const { t } = useTranslation()
      return t('proxy.item.proxy_name')
    },
    cell: ({ row }) => {
      return <div className="font-mono text-nowrap">{row.original.name}</div>
    },
  },
  {
    accessorKey: 'type',
    header: function Header() {
      const { t } = useTranslation()
      return t('proxy.item.proxy_type')
    },
    cell: ({ row }) => {
      return <div className="font-mono text-nowrap">{row.original.type}</div>
    },
  },
  {
    accessorKey: 'clientID',
    header: function Header() {
      const { t } = useTranslation()
      return t('proxy.item.client_name')
    },
    cell: ({ row }) => {
      return <ProxyClientName row={row} />
    },
  },
  {
    accessorKey: 'serverID',
    header: function Header() {
      const { t } = useTranslation()
      return t('proxy.item.server_name')
    },
    cell: ({ row }) => {
      return <ProxyServerName row={row} />
    },
  },
  {
    id: 'status',
    header: function Header() {
      const { t } = useTranslation()
      return t('proxy.item.status')
    },
    cell: ProxyStatus,
  },
  {
    id: 'rules',
    header: function Header() {
      const { t } = useTranslation()
      return t('proxy.rules.column')
    },
    cell: ProxyRulesCell,
  },
  {
    id: 'todayTraffic',
    header: function Header() {
      const { t } = useTranslation()
      return t('traffic.today.total')
    },
    cell: ProxyTodayTrafficCell,
  },
  {
    accessorKey: 'visitPreview',
    header: function Header() {
      const { t } = useTranslation()
      return t('proxy.item.visit_preview')
    },
    cell: ({ row }) => {
      return <VisitPreviewField row={row} />
    },
  },
  {
    id: 'action',
    cell: ({ row }) => {
      return (
        <ProxyConfigActions
          row={row}
          serverID={row.original.serverID}
          clientID={row.original.clientID}
          name={row.original.name}
        />
      )
    },
  },
]

// 所属客户端名称：优先显示 comment，查不到时回退显示原 ID
function ProxyClientName({ row }: { row: Row<ProxyConfigTableSchema> }) {
  const clientId = row.original.originalProxyConfig.originClientId || row.original.clientID
  const { data } = useQuery({
    queryKey: ['getClientName', clientId],
    queryFn: () => getClient({ clientId }),
    enabled: !!clientId,
    staleTime: 30_000,
  })
  return <div className="text-nowrap">{data?.client?.comment || clientId}</div>
}

// 所属服务端名称：优先显示 comment，查不到时回退显示原 ID
function ProxyServerName({ row }: { row: Row<ProxyConfigTableSchema> }) {
  const { data } = useQuery({
    queryKey: ['getServerName', row.original.serverID],
    queryFn: () => getServer({ serverId: row.original.serverID }),
    enabled: !!row.original.serverID,
    staleTime: 30_000,
  })
  return <div className="text-nowrap">{data?.server?.comment || row.original.serverID}</div>
}

function VisitPreviewField({ row }: { row: Row<ProxyConfigTableSchema> }) {
  const { data: server } = useQuery({
    queryKey: ['getServer', row.original.serverID],
    queryFn: () => {
      return getServer({ serverId: row.original.serverID })
    },
  })

  const typedProxyConfig = parseJSONSafe<TypedProxyConfig>(row.original.config, {} as TypedProxyConfig)

  return <VisitPreview server={server?.server || { frpsUrls: [] }} typedProxyConfig={typedProxyConfig} />
}

function ProxyStatus({ row }: { row: Row<ProxyConfigTableSchema> }) {
  const { t } = useTranslation()
  const refetchTrigger = useStore($proxyTableRefetchTrigger)
  const { data } = useQuery({
    queryKey: ['getProxyConfig', row.original.clientID, row.original.serverID, row.original.name, refetchTrigger],
    queryFn: () => {
      return getProxyConfig({
        clientId: row.original.clientID,
        serverId: row.original.serverID,
        name: row.original.name,
      })
    },
    refetchInterval: 10000,
  })

  function getStatusColor(status: string): string {
    switch (status) {
      case 'new':
        return 'text-blue-500'
      case 'wait start':
        return 'text-yellow-400'
      case 'start error':
        return 'text-red-500'
      case 'running':
        return 'text-green-500'
      case 'check failed':
        return 'text-orange-500'
      case 'error':
        return 'text-red-600'
      default:
        return 'text-gray-500'
    }
  }

  return (
    <div className="flex items-center gap-2 flex-row text-nowrap">
      <Badge
        variant={'secondary'}
        className={`p-2 border rounded font-mono w-fit ${getStatusColor(data?.workingStatus?.status || 'unknown')} text-nowrap rounded-full h-6`}
      >
        {data?.workingStatus?.status || 'loading'}
      </Badge>
      {row.original.originalProxyConfig.autoStopped && (
        <Badge variant={'destructive'} className="text-nowrap rounded-full h-6 px-2">
          {t('proxy.rules.auto_stopped')}
        </Badge>
      )}
    </div>
  )
}

// 今日流量列：按 client 拉取统计（React Query 按缓存去重，同 client 多行只发一次请求），
// 在返回的 ProxyInfo 里按 name+serverId 定位本行隧道。
// 刷新信号进入 queryKey：点击"搜索"等触发表格刷新时本列同步重新拉取。
function ProxyTodayTrafficCell({ row }: { row: Row<ProxyConfigTableSchema> }) {
  const refetchTrigger = useStore($proxyTableRefetchTrigger)
  const { data } = useQuery({
    queryKey: ['getProxyStatsByClientID', row.original.clientID, refetchTrigger],
    queryFn: () => getProxyStatsByClientID({ clientId: row.original.clientID }),
    enabled: !!row.original.clientID,
    staleTime: 30_000,
    refetchInterval: 30_000,
  })

  const info = data?.proxyInfos?.find(
    (p) => p.name === row.original.name && (!p.serverId || p.serverId === row.original.serverID),
  )
  if (!info) {
    return <span className="text-muted-foreground text-sm text-nowrap">—</span>
  }

  const todayIn = Number(info.todayTrafficIn ?? 0n)
  const todayOut = Number(info.todayTrafficOut ?? 0n)
  if (todayIn === 0 && todayOut === 0) {
    return <span className="text-muted-foreground text-sm text-nowrap">—</span>
  }

  return (
    <div className="text-xs space-y-0.5 text-nowrap font-mono">
      <div>↓ {formatBytes(todayIn)}</div>
      <div className="text-muted-foreground">↑ {formatBytes(todayOut)}</div>
    </div>
  )
}

// 规则/限额列：展示定时窗口与流量限额（含当前窗口已用量）
function ProxyRulesCell({ row }: { row: Row<ProxyConfigTableSchema> }) {
  const { t } = useTranslation()
  const cfg = row.original.originalProxyConfig

  const scheduleOn = cfg.scheduleEnabled ?? false
  const quotaType = cfg.quotaType || ''
  const quotaOn = quotaType !== ''
  const quotaLimit = Number(cfg.quotaLimit ?? 0n)
  const quotaUsed = Number(cfg.quotaUsed ?? 0n)
  const exceeded = quotaOn && quotaLimit > 0 && quotaUsed >= quotaLimit
  // 限额统计方向：空/both 为双向（旧数据兼容口径），不额外展示
  const quotaDirection = cfg.quotaDirection || ''
  const quotaDirLabel =
    quotaDirection === 'inbound' || quotaDirection === 'outbound'
      ? t(`proxy.rules.quota_dir_${quotaDirection === 'inbound' ? 'in' : 'out'}`)
      : ''

  if (!scheduleOn && !quotaOn) {
    return <span className="text-muted-foreground text-sm">—</span>
  }

  const days = [...(cfg.scheduleDays ?? [])].sort((a, b) => a - b)

  return (
    <div className="text-xs space-y-1 text-nowrap">
      {scheduleOn && (
        <div className="font-mono">
          🕐 {cfg.scheduleStart || '--:--'} - {cfg.scheduleEnd || '--:--'}
          {days.length > 0 && (
            <span className="text-muted-foreground"> ({days.map((d) => t(`proxy.rules.weekday_${d}`)).join('、')})</span>
          )}
        </div>
      )}
      {quotaOn && (
        <div className={`font-mono ${exceeded ? 'font-medium text-red-500' : ''}`}>
          {t(`proxy.rules.quota_${quotaType}`)}
          {quotaDirLabel && <span className="text-muted-foreground">（{quotaDirLabel}）</span>}{' '}
          {formatBytes(quotaUsed)} / {formatBytes(quotaLimit)}
          {exceeded && <span className="ml-1">· {t('proxy.rules.exceeded_short')}</span>}
        </div>
      )}
    </div>
  )
}
