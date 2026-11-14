import { useQuery } from '@tanstack/react-query'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { TbEngine, TbServer2 } from 'react-icons/tb'
import { useEffect } from 'react'
import { $platformInfo } from '@/store/user'
import { getPlatformInfo } from '@/api/platform'
import { useTranslation } from 'react-i18next'

// 单个节点的统计卡：总数 + 已配置/未配置分解 + 配置率进度条
function NodeStatCard({
  title,
  icon,
  total,
  configured,
  unconfigured,
  configuredLabel,
  unconfiguredLabel,
  unit,
}: {
  title: string
  icon: React.ReactNode
  total: number
  configured: number
  unconfigured: number
  configuredLabel: string
  unconfiguredLabel: string
  unit: string
}) {
  const ratio = total > 0 ? Math.min(100, Math.round((configured / total) * 100)) : 0

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between">
          <h3 className="tracking-tight text-sm font-medium">{title}</h3>
          {icon}
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="flex items-baseline gap-2">
          <span className="text-3xl font-bold leading-none">{total}</span>
          <span className="text-sm text-muted-foreground">{unit}</span>
        </div>

        <div className="flex items-center gap-4 text-xs text-nowrap">
          <span className="flex items-center gap-1.5">
            <span className="h-2 w-2 rounded-full bg-primary" />
            {configuredLabel}
            <span className="font-semibold text-foreground">{configured}</span>
          </span>
          <span className="flex items-center gap-1.5 text-muted-foreground">
            <span className="h-2 w-2 rounded-full bg-muted-foreground/40" />
            {unconfiguredLabel}
            <span className="font-semibold">{unconfigured}</span>
          </span>
          <span className="ml-auto font-mono text-muted-foreground">{ratio}%</span>
        </div>

        {/* 配置率进度条（手绘 div，避免引入额外组件） */}
        <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
          <div className="h-full rounded-full bg-primary transition-all" style={{ width: `${ratio}%` }} />
        </div>
      </CardContent>
    </Card>
  )
}

export default function PlatformInfo() {
  const { t } = useTranslation();
  const platformInfo = useQuery({
    queryKey: ['platformInfo'],
    queryFn: getPlatformInfo,
  })
  useEffect(() => {
    $platformInfo.set(platformInfo.data)
  }, [platformInfo])

  const data = platformInfo.data

  return (
    <div className="space-y-2">
      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        <NodeStatCard
          title={t('platform.servers')}
          icon={<TbServer2 className="mt-1" />}
          total={data?.totalServerCount ?? 0}
          configured={data?.configuredServerCount ?? 0}
          unconfigured={data?.unconfiguredServerCount ?? 0}
          configuredLabel={t('platform.configured')}
          unconfiguredLabel={t('platform.unconfigured')}
          unit={t('platform.unit')}
        />
        <NodeStatCard
          title={t('platform.clients')}
          icon={<TbEngine className="mt-1" />}
          total={data?.totalClientCount ?? 0}
          configured={data?.configuredClientCount ?? 0}
          unconfigured={data?.unconfiguredClientCount ?? 0}
          configuredLabel={t('platform.configured')}
          unconfiguredLabel={t('platform.unconfigured')}
          unit={t('platform.unit')}
        />
      </div>
      <p className="text-xs text-muted-foreground px-1">{t('platform.menuHint')}</p>
    </div>
  )
}
