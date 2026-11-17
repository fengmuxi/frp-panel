import React, { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { ProxyConfig, RespCode } from '@/lib/pb/common'
import { updateProxyRules } from '@/api/proxy'
import { formatBytes } from '@/lib/utils'
import { toast } from 'sonner'
import { $proxyTableRefetchTrigger } from '@/store/refetch-trigger'

export interface ProxyRulesDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  proxy: ProxyConfig
}

// 展示顺序：周一 ~ 周日（0=周日 对应后端 time.Weekday）
const WEEKDAY_ORDER = [1, 2, 3, 4, 5, 6, 0]

const MB = 1024 * 1024
const GB = 1024 * 1024 * 1024

type QuotaUnit = 'MB' | 'GB'

// 隧道自动规则对话框：定时开关（时间段 + 周几多选）与流量限额（日/周/月三选一）
export function ProxyRulesDialog({ open, onOpenChange, proxy }: ProxyRulesDialogProps) {
  const { t } = useTranslation()
  const [saving, setSaving] = useState(false)

  // 初始快照（打开时重置），用于“未修改禁用保存”；quotaType 未启用统一存为 __none__ 以适配 Select
  const initial = useMemo(() => {
    const limit = Number(proxy.quotaLimit ?? 0n)
    const unit: QuotaUnit = limit > 0 && limit % GB !== 0 && limit < GB ? 'MB' : 'GB'
    const value = limit > 0 ? (unit === 'GB' ? limit / GB : limit / MB) : ''
    return {
      scheduleEnabled: proxy.scheduleEnabled ?? false,
      scheduleStart: proxy.scheduleStart || '08:00',
      scheduleEnd: proxy.scheduleEnd || '20:00',
      scheduleDays: [...(proxy.scheduleDays ?? [])].sort((a, b) => a - b),
      quotaType: proxy.quotaType && proxy.quotaType !== '' ? proxy.quotaType : '__none__',
      quotaValue: value === '' ? '' : String(Number(value.toFixed(2))),
      quotaUnit: unit,
      quotaDirection: proxy.quotaDirection || 'both',
    }
  }, [proxy])

  const [scheduleEnabled, setScheduleEnabled] = useState(initial.scheduleEnabled)
  const [scheduleStart, setScheduleStart] = useState(initial.scheduleStart)
  const [scheduleEnd, setScheduleEnd] = useState(initial.scheduleEnd)
  const [scheduleDays, setScheduleDays] = useState<number[]>(initial.scheduleDays)
  const [quotaType, setQuotaType] = useState(initial.quotaType)
  const [quotaValue, setQuotaValue] = useState(String(initial.quotaValue))
  const [quotaUnit, setQuotaUnit] = useState<QuotaUnit>(initial.quotaUnit)
  const [quotaDirection, setQuotaDirection] = useState(initial.quotaDirection)

  useEffect(() => {
    if (open) {
      setScheduleEnabled(initial.scheduleEnabled)
      setScheduleStart(initial.scheduleStart)
      setScheduleEnd(initial.scheduleEnd)
      setScheduleDays(initial.scheduleDays)
      setQuotaType(initial.quotaType)
      setQuotaValue(String(initial.quotaValue))
      setQuotaUnit(initial.quotaUnit)
      setQuotaDirection(initial.quotaDirection)
    }
  }, [open, initial])

  const quotaLimitBytes = useMemo(() => {
    const n = Number(quotaValue)
    if (Number.isNaN(n) || n <= 0) return 0
    return Math.floor(n * (quotaUnit === 'GB' ? GB : MB))
  }, [quotaValue, quotaUnit])

  const quotaUsed = Number(proxy.quotaUsed ?? 0n)
  const quotaActive = quotaType !== '__none__'
  const quotaExceeded =
    quotaActive && quotaLimitBytes > 0 && quotaUsed >= (Number(proxy.quotaLimit ?? 0n) || quotaLimitBytes)

  const daysInvalid = scheduleEnabled && scheduleDays.length === 0
  const quotaInvalid = quotaActive && quotaLimitBytes <= 0

  const unchanged =
    scheduleEnabled === initial.scheduleEnabled &&
    scheduleStart === initial.scheduleStart &&
    scheduleEnd === initial.scheduleEnd &&
    JSON.stringify([...scheduleDays].sort((a, b) => a - b)) === JSON.stringify(initial.scheduleDays) &&
    quotaType === initial.quotaType &&
    (!quotaActive || quotaValue === String(initial.quotaValue)) &&
    (!quotaActive || quotaUnit === initial.quotaUnit) &&
    (!quotaActive || quotaDirection === initial.quotaDirection)

  const toggleDay = (day: number) => {
    setScheduleDays((prev) => (prev.includes(day) ? prev.filter((d) => d !== day) : [...prev, day].sort((a, b) => a - b)))
  }

  const handleSave = async () => {
    if (daysInvalid) {
      toast(t('proxy.rules.days_required'))
      return
    }
    if (quotaInvalid) {
      toast(t('proxy.rules.quota_invalid'))
      return
    }
    setSaving(true)
    try {
      const resp = await updateProxyRules({
        clientId: proxy.clientId,
        serverId: proxy.serverId,
        name: proxy.name,
        scheduleEnabled,
        scheduleStart,
        scheduleEnd,
        scheduleDays,
        quotaType: quotaActive ? quotaType : '',
        quotaLimit: BigInt(quotaActive ? quotaLimitBytes : 0),
        quotaDirection: quotaActive ? quotaDirection : '',
      })
      if (resp.status?.code !== RespCode.SUCCESS) {
        throw new Error(resp.status?.message ?? 'unknown error')
      }
      toast(t('proxy.rules.save_success'))
      $proxyTableRefetchTrigger.set(Math.random())
      onOpenChange(false)
    } catch (e) {
      toast(t('proxy.rules.save_failed'), { description: e instanceof Error ? e.message : String(e) })
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] overflow-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t('proxy.rules.title')}</DialogTitle>
          <DialogDescription>{t('proxy.rules.description')}</DialogDescription>
        </DialogHeader>

        {/* 已配置限额时展示当前窗口用量 */}
        {initial.quotaType !== '' && (
          <div
            className={`rounded-md border px-3 py-2 text-sm ${
              quotaExceeded ? 'border-red-300 bg-red-50 text-red-600 dark:bg-red-950' : 'border-border bg-muted/40'
            }`}
          >
            <span className="font-medium">{t('proxy.rules.quota_used')}:</span>{' '}
            {formatBytes(Number(proxy.quotaUsed ?? 0n))} / {formatBytes(Number(proxy.quotaLimit ?? 0n))}
            {quotaExceeded && <span className="ml-2 font-medium">{t('proxy.rules.exceeded')}</span>}
          </div>
        )}

        {/* 定时开关 */}
        <div className="grid gap-3">
          <div className="flex items-center justify-between">
            <Label htmlFor="rules-schedule-enable" className="font-medium">
              {t('proxy.rules.schedule_section')}
            </Label>
            <Switch id="rules-schedule-enable" checked={scheduleEnabled} onCheckedChange={setScheduleEnabled} />
          </div>

          {scheduleEnabled && (
            <>
              <div className="grid grid-cols-2 gap-3">
                <div className="grid gap-2">
                  <Label htmlFor="rules-start">{t('proxy.rules.schedule_start')}</Label>
                  <Input
                    id="rules-start"
                    type="time"
                    step={60}
                    value={scheduleStart}
                    onChange={(e) => setScheduleStart(e.target.value)}
                  />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="rules-end">{t('proxy.rules.schedule_end')}</Label>
                  <Input
                    id="rules-end"
                    type="time"
                    step={60}
                    value={scheduleEnd}
                    onChange={(e) => setScheduleEnd(e.target.value)}
                  />
                </div>
              </div>

              <div className="grid gap-2">
                <Label>{t('proxy.rules.schedule_days')}</Label>
                <div className="flex flex-wrap gap-3">
                  {WEEKDAY_ORDER.map((day) => (
                    <label key={day} className="flex cursor-pointer items-center gap-1.5 text-sm">
                      <Checkbox
                        checked={scheduleDays.includes(day)}
                        onCheckedChange={() => toggleDay(day)}
                      />
                      {t(`proxy.rules.weekday_${day}`)}
                    </label>
                  ))}
                </div>
                <p className="text-xs text-muted-foreground">{t('proxy.rules.schedule_hint')}</p>
                {daysInvalid && <p className="text-xs text-destructive">{t('proxy.rules.days_required')}</p>}
              </div>
            </>
          )}
        </div>

        {/* 流量限额 */}
        <div className="grid gap-3 border-t pt-3">
          <Label htmlFor="rules-quota-type" className="font-medium">
            {t('proxy.rules.quota_section')}
          </Label>
          <div className="grid grid-cols-2 gap-3">
            <div className="grid gap-2">
              <Label htmlFor="rules-quota-type">{t('proxy.rules.quota_type')}</Label>
              <Select value={quotaType} onValueChange={setQuotaType}>
                <SelectTrigger id="rules-quota-type">
                  <SelectValue placeholder={t('proxy.rules.quota_none')} />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="__none__">{t('proxy.rules.quota_none')}</SelectItem>
                  <SelectItem value="day">{t('proxy.rules.quota_day')}</SelectItem>
                  <SelectItem value="week">{t('proxy.rules.quota_week')}</SelectItem>
                  <SelectItem value="month">{t('proxy.rules.quota_month')}</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="grid gap-2">
              <Label htmlFor="rules-quota-value">{t('proxy.rules.quota_limit')}</Label>
              <div className="flex gap-2">
                <Input
                  id="rules-quota-value"
                  type="number"
                  min={0}
                  disabled={!quotaActive}
                  value={quotaValue}
                  onChange={(e) => setQuotaValue(e.target.value)}
                  className="flex-1"
                />
                <Select
                  value={quotaUnit}
                  onValueChange={(v) => setQuotaUnit(v as QuotaUnit)}
                >
                  <SelectTrigger className="w-24" disabled={!quotaActive}>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="MB">MB</SelectItem>
                    <SelectItem value="GB">GB</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="rules-quota-direction">{t('proxy.rules.quota_direction')}</Label>
            <Select
              value={quotaDirection}
              onValueChange={setQuotaDirection}
              disabled={!quotaActive}
            >
              <SelectTrigger id="rules-quota-direction">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="both">{t('proxy.rules.quota_dir_both')}</SelectItem>
                <SelectItem value="inbound">{t('proxy.rules.quota_dir_in')}</SelectItem>
                <SelectItem value="outbound">{t('proxy.rules.quota_dir_out')}</SelectItem>
              </SelectContent>
            </Select>
          </div>
          {quotaInvalid && <p className="text-xs text-destructive">{t('proxy.rules.quota_invalid')}</p>}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button disabled={unchanged || saving || daysInvalid || quotaInvalid} onClick={handleSave}>
            {saving ? t('common.saving') : t('common.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
