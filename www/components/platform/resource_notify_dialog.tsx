import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
	NotifyPrefs,
	ResourceNotifyPrefs,
	getNotifyPrefs,
	getResourceNotifyPrefs,
	updateResourceNotifyPrefs,
} from '@/api/notify'
import { OFFLINE_DELAY_OPTIONS } from '@/components/platform/notify-prefs'

import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from '@/components/ui/select'

type Mode = NonNullable<ResourceNotifyPrefs['mode']>
type ResourceType = 'server' | 'client' | 'proxy'

const MODES: Mode[] = ['off', 'inherit', 'override']

interface ResourceNotifyDialogProps {
	open: boolean
	onOpenChange: (open: boolean) => void
	resourceType: ResourceType
	// server/client 传业务主键；proxy 传 proxy_config 行主键
	resourceId: string
	// 弹窗标题里展示的资源显示名
	resourceLabel?: string
	onSuccess?: () => void
}

/**
 * 资源级通知设置弹窗（三段式）：
 *   不提醒      —— 该资源任何提醒一律不发（最高优先级）
 *   全局配置    —— 跟随当前用户的全局通知开关（只读预览）
 *   独立配置    —— 按本资源自己的一套开关与防抖时长执行
 * 优先级：独立设置 > 全局设置。
 */
export function ResourceNotifyDialog({
	open,
	onOpenChange,
	resourceType,
	resourceId,
	resourceLabel,
	onSuccess,
}: ResourceNotifyDialogProps) {
	const { t } = useTranslation()
	const [loading, setLoading] = useState(true)
	const [saving, setSaving] = useState(false)

	const [mode, setMode] = useState<Mode>('inherit')
	// 独立配置字段
	const [enableOffline, setEnableOffline] = useState(false)
	const [enableOnline, setEnableOnline] = useState(false)
	const [offlineDelay, setOfflineDelay] = useState(0)
	const [enableQuota, setEnableQuota] = useState(false)
	const [enableSchedule, setEnableSchedule] = useState(false)
	// 全局配置只读预览
	const [globalPrefs, setGlobalPrefs] = useState<NotifyPrefs | null>(null)

	const isProxy = resourceType === 'proxy'

	useEffect(() => {
		if (!open) {
			return
		}
		let canceled = false
		setLoading(true)
		;(async () => {
			try {
				const [rp, gp] = await Promise.all([
					getResourceNotifyPrefs({ type: resourceType, resource_id: resourceId }),
					getNotifyPrefs(),
				])
				if (canceled) {
					return
				}
				setMode(rp.mode ?? 'inherit')
				setEnableOffline(!!rp.enable_offline)
				setEnableOnline(!!rp.enable_online)
				setOfflineDelay(rp.offline_delay ?? 0)
				setEnableQuota(!!rp.enable_quota_exceeded)
				setEnableSchedule(!!rp.enable_schedule_toggle)
				setGlobalPrefs(gp)
			} catch (err) {
				toast.error(String(err))
			} finally {
				if (!canceled) {
					setLoading(false)
				}
			}
		})()
		return () => {
			canceled = true
		}
	}, [open, resourceType, resourceId])

	const onSave = async () => {
		setSaving(true)
		try {
			await updateResourceNotifyPrefs({
				type: resourceType,
				resource_id: resourceId,
				mode,
				enable_offline: enableOffline,
				enable_online: enableOnline,
				offline_delay: offlineDelay,
				enable_quota_exceeded: enableQuota,
				enable_schedule_toggle: enableSchedule,
			})
			toast.success(t('settings.notifyResource.saveSuccess'))
			onOpenChange(false)
			onSuccess?.()
		} catch (err) {
			toast.error(String(err))
		} finally {
			setSaving(false)
		}
	}

	// 全局只读预览的开关清单（按资源类型显示相关项）
	const globalRows: { label: string; on: boolean }[] = globalPrefs
		? isProxy
			? [
					{ label: t('settings.smtp.form.proxyOfflineLabel'), on: globalPrefs.enable_proxy_offline },
					{ label: t('settings.smtp.form.proxyOnlineLabel'), on: globalPrefs.enable_proxy_online },
					{ label: t('settings.smtp.form.quotaExceededLabel'), on: globalPrefs.enable_quota_exceeded },
					{ label: t('settings.smtp.form.scheduleToggleLabel'), on: globalPrefs.enable_schedule_toggle },
			  ]
			: resourceType === 'server'
			  ? [
						{ label: t('settings.smtp.form.serverOfflineLabel'), on: globalPrefs.enable_server_offline },
						{ label: t('settings.smtp.form.serverOnlineLabel'), on: globalPrefs.enable_server_online },
			    ]
			  : [
						{ label: t('settings.smtp.form.clientOfflineLabel'), on: globalPrefs.enable_client_offline },
						{ label: t('settings.smtp.form.clientOnlineLabel'), on: globalPrefs.enable_client_online },
			    ]
		: []

	// 该资源类型在"独立配置"模式下可设置的开关清单
	const overrideRows = isProxy
		? [
				{ key: 'offline', label: t('settings.smtp.form.proxyOfflineLabel'), desc: t('settings.smtp.form.proxyOfflineDescription'), checked: enableOffline, toggle: setEnableOffline },
				{ key: 'online', label: t('settings.smtp.form.proxyOnlineLabel'), desc: t('settings.smtp.form.proxyOnlineDescription'), checked: enableOnline, toggle: setEnableOnline },
				{ key: 'quota', label: t('settings.smtp.form.quotaExceededLabel'), desc: t('settings.smtp.form.quotaExceededDescription'), checked: enableQuota, toggle: setEnableQuota },
				{ key: 'schedule', label: t('settings.smtp.form.scheduleToggleLabel'), desc: t('settings.smtp.form.scheduleToggleDescription'), checked: enableSchedule, toggle: setEnableSchedule },
		  ]
		: resourceType === 'server'
		  ? [
					{ key: 'offline', label: t('settings.smtp.form.serverOfflineLabel'), desc: t('settings.smtp.form.serverOfflineDescription'), checked: enableOffline, toggle: setEnableOffline },
					{ key: 'online', label: t('settings.smtp.form.serverOnlineLabel'), desc: t('settings.smtp.form.serverOnlineDescription'), checked: enableOnline, toggle: setEnableOnline },
		    ]
		  : [
					{ key: 'offline', label: t('settings.smtp.form.clientOfflineLabel'), desc: t('settings.smtp.form.clientOfflineDescription'), checked: enableOffline, toggle: setEnableOffline },
					{ key: 'online', label: t('settings.smtp.form.clientOnlineLabel'), desc: t('settings.smtp.form.clientOnlineDescription'), checked: enableOnline, toggle: setEnableOnline },
		    ]

	const modeBtnLabel: Record<Mode, string> = {
		off: t('settings.notifyResource.modeOff'),
		inherit: t('settings.notifyResource.modeInherit'),
		override: t('settings.notifyResource.modeOverride'),
	}
	const modeHint: Record<Mode, string> = {
		off: t('settings.notifyResource.hintOff'),
		inherit: t('settings.notifyResource.hintInherit'),
		override: t('settings.notifyResource.hintOverride'),
	}

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="max-w-lg">
				<DialogHeader>
					<DialogTitle>
						{t('settings.notifyResource.title')}
						{resourceLabel ? ` · ${resourceLabel}` : ''}
					</DialogTitle>
					<DialogDescription>{t('settings.notifyResource.description')}</DialogDescription>
				</DialogHeader>

				{loading ? (
					<p className="text-sm text-muted-foreground py-8 text-center">
						{t('settings.notifyResource.loading')}
					</p>
				) : (
					<div className="space-y-4">
						{/* 三段式：不提醒 / 全局配置 / 独立配置 */}
						<div className="flex gap-2">
							{MODES.map((v) => (
								<Button
									key={v}
									type="button"
									size="sm"
									variant={mode === v ? 'default' : 'outline'}
									onClick={() => setMode(v)}
								>
									{modeBtnLabel[v]}
								</Button>
							))}
						</div>
						<p className="text-xs text-muted-foreground">{modeHint[mode]}</p>

						{mode === 'off' && (
							<div className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">
								{t('settings.notifyResource.modeOffPanel')}
							</div>
						)}

						{mode === 'inherit' && (
							<div className="rounded-lg border p-3">
								<p className="text-sm font-medium mb-2">{t('settings.notifyResource.globalPreviewTitle')}</p>
								<ul className="grid grid-cols-2 gap-x-4 gap-y-1.5 text-xs text-muted-foreground">
									{globalRows.map((row) => (
										<li key={row.label} className="flex items-center justify-between gap-2">
											<span className="truncate">{row.label}</span>
											<span className={row.on ? 'text-green-600 font-medium' : 'font-medium'}>
												{row.on
													? t('settings.notifyResource.stateOn')
													: t('settings.notifyResource.stateOff')}
											</span>
										</li>
									))}
								</ul>
							</div>
						)}

						{mode === 'override' && (
							<div className="space-y-3">
								{overrideRows.map((row) => (
									<div
										key={row.key}
										className="flex flex-row items-center justify-between rounded-lg border p-3 shadow-sm"
									>
										<div className="space-y-0.5">
											<p className="text-sm font-medium leading-none">{row.label}</p>
											<p className="text-xs text-muted-foreground">{row.desc}</p>
										</div>
										<Switch checked={row.checked} onCheckedChange={row.toggle} />
									</div>
								))}

								{/* 掉线防抖：独立于全局单独设置，0=默认 60s */}
								<div className="rounded-lg border p-3 shadow-sm">
									<p className="text-sm font-medium leading-none mb-1">
										{t('settings.notifyResource.delayLabel')}
									</p>
									<Select
										value={String(offlineDelay ?? 0)}
										onValueChange={(v) => setOfflineDelay(Number(v))}
									>
										<SelectTrigger className="w-full">
											<SelectValue />
										</SelectTrigger>
										<SelectContent>
											{OFFLINE_DELAY_OPTIONS.map((opt) => (
												<SelectItem key={opt.value} value={String(opt.value)}>
													{t(`settings.smtp.form.${opt.key}`)}
												</SelectItem>
											))}
										</SelectContent>
									</Select>
								</div>
							</div>
						)}
					</div>
				)}

				<DialogFooter>
					<Button variant="outline" onClick={() => onOpenChange(false)}>
						{t('settings.notifyResource.cancel')}
					</Button>
					<Button onClick={onSave} disabled={loading || saving}>
						{saving ? t('settings.notifyResource.saving') : t('settings.notifyResource.save')}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	)
}
