'use client'

import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Period, defaultCustomDates, toDateInput } from '@/lib/time-range'

const PRESET_PERIODS: Exclude<Period, 'custom'>[] = ['today', 'yesterday', 'week', 'month', 'year']

interface PeriodTabsProps {
  value: Period
  onChange: (p: Period) => void
  /** 自定义时段"应用"，参数为 yyyy-MM-dd 起止 */
  onApplyCustom: (startDate: string, endDate: string) => void
}

export function PeriodTabs({ value, onChange, onApplyCustom }: PeriodTabsProps) {
  const { t } = useTranslation()
  const [dates, setDates] = useState(defaultCustomDates())

  const apply = () => onApplyCustom(dates.startDate, dates.endDate)

  return (
    <div className="flex flex-wrap items-center gap-2">
      <Tabs value={value} onValueChange={(v) => onChange(v as Period)}>
        <TabsList>
          {PRESET_PERIODS.map((p) => (
            <TabsTrigger key={p} value={p}>
              {t(`home.period.${p}`)}
            </TabsTrigger>
          ))}
          <TabsTrigger value="custom">{t('home.period.custom')}</TabsTrigger>
        </TabsList>
      </Tabs>
      {value === 'custom' && (
        <div className="flex flex-wrap items-center gap-2">
          <Input
            type="date"
            className="w-[150px]"
            value={dates.startDate}
            max={dates.endDate}
            onChange={(e) => setDates((d) => ({ ...d, startDate: e.target.value }))}
          />
          <span className="text-sm text-muted-foreground">~</span>
          <Input
            type="date"
            className="w-[150px]"
            value={dates.endDate}
            min={dates.startDate}
            onChange={(e) => setDates((d) => ({ ...d, endDate: e.target.value }))}
          />
          <Button size="sm" variant="outline" onClick={apply}>
            {t('home.period.apply')}
          </Button>
        </div>
      )}
    </div>
  )
}
