import { toast } from 'sonner'
import { useTranslation } from 'react-i18next'
import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import * as z from 'zod'

import { NotifyPrefs, getNotifyPrefs, sendNotifyTest, updateNotifyPrefs } from '@/api/notify'

import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage, FormDescription } from '@/components/ui/form'
import { Switch } from '@/components/ui/switch'
import { Button } from '@/components/ui/button'
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from '@/components/ui/select'
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter as DialogFooterUI,
	DialogHeader,
	DialogTitle,
	DialogTrigger,
} from '@/components/ui/dialog'

// 清空输入框时值为 ""，预处理为 undefined，避免 coerce 出 0 或阻塞校验（同 frps_form 惯例）
const emptyToUndefined = (v: unknown) => (v === '' || v === null ? undefined : v)

// 通知开关（按用户独立，所有登录用户均可配置自己的），默认全部关闭
const PrefsSchema = z.object({
	enable_server_offline: z.boolean().default(false),
	enable_client_offline: z.boolean().default(false),
	enable_proxy_offline: z.boolean().default(false),
	enable_login_notify: z.boolean().default(false),
	enable_server_online: z.boolean().default(false),
	enable_client_online: z.boolean().default(false),
	enable_proxy_online: z.boolean().default(false),
	// 自动规则触发的提醒
	enable_quota_exceeded: z.boolean().default(false),
	enable_schedule_toggle: z.boolean().default(false),
	// 下线通知防抖时长（秒），0=默认 60s
	offline_delay_server: z.preprocess(emptyToUndefined, z.coerce.number().min(0).max(3600).default(0)),
	offline_delay_client: z.preprocess(emptyToUndefined, z.coerce.number().min(0).max(3600).default(0)),
	offline_delay_proxy: z.preprocess(emptyToUndefined, z.coerce.number().min(0).max(3600).default(0)),
})

// 防抖时长选项（秒）与展示文案（资源级设置弹窗复用）
export const OFFLINE_DELAY_OPTIONS = [
	{ value: 0, key: 'offlineDelayDefault' },
	{ value: 30, key: 'offlineDelay30s' },
	{ value: 60, key: 'offlineDelay1m' },
	{ value: 120, key: 'offlineDelay2m' },
	{ value: 300, key: 'offlineDelay5m' },
	{ value: 600, key: 'offlineDelay10m' },
] as const

type PrefsFormValues = z.infer<typeof PrefsSchema>

export function NotifyPrefsForm() {
	const { t } = useTranslation()
	const [loading, setLoading] = useState(true)
	const [initial, setInitial] = useState<PrefsFormValues | null>(null)
	const [open, setOpen] = useState(false)
	// testing 记录正在发送的测试模板（'test'=通用连通性测试），null=空闲
	const [testing, setTesting] = useState<string | null>(null)

	const form = useForm<PrefsFormValues>({
		resolver: zodResolver(PrefsSchema),
		defaultValues: {
			enable_server_offline: false,
			enable_client_offline: false,
			enable_proxy_offline: false,
			enable_login_notify: false,
			enable_server_online: false,
			enable_client_online: false,
			enable_proxy_online: false,
			enable_quota_exceeded: false,
			enable_schedule_toggle: false,
			offline_delay_server: 0,
			offline_delay_client: 0,
			offline_delay_proxy: 0,
		},
	})

	// 组件挂载时读取自己的通知开关配置
	useEffect(() => {
		let canceled = false
		;(async () => {
			try {
				const p = await getNotifyPrefs()
				if (canceled) return
				const values: PrefsFormValues = {
					enable_server_offline: !!p.enable_server_offline,
					enable_client_offline: !!p.enable_client_offline,
					enable_proxy_offline: !!p.enable_proxy_offline,
					enable_login_notify: !!p.enable_login_notify,
					enable_server_online: !!p.enable_server_online,
					enable_client_online: !!p.enable_client_online,
					enable_proxy_online: !!p.enable_proxy_online,
					enable_quota_exceeded: !!p.enable_quota_exceeded,
					enable_schedule_toggle: !!p.enable_schedule_toggle,
					offline_delay_server: p.offline_delay_server ?? 0,
					offline_delay_client: p.offline_delay_client ?? 0,
					offline_delay_proxy: p.offline_delay_proxy ?? 0,
				}
				form.reset(values)
				setInitial(form.getValues())
			} catch (err) {
				toast.error(String(err))
			} finally {
				if (!canceled) setLoading(false)
			}
		})()
		return () => {
			canceled = true
		}
	}, [])

	const buildPayload = (values: PrefsFormValues): NotifyPrefs => ({
		enable_server_offline: values.enable_server_offline,
		enable_client_offline: values.enable_client_offline,
		enable_proxy_offline: values.enable_proxy_offline,
		enable_login_notify: values.enable_login_notify,
		enable_server_online: values.enable_server_online,
		enable_client_online: values.enable_client_online,
		enable_proxy_online: values.enable_proxy_online,
		enable_quota_exceeded: values.enable_quota_exceeded,
		enable_schedule_toggle: values.enable_schedule_toggle,
		offline_delay_server: values.offline_delay_server,
		offline_delay_client: values.offline_delay_client,
		offline_delay_proxy: values.offline_delay_proxy,
	})

	const onSubmit = async (values: PrefsFormValues) => {
		try {
			await updateNotifyPrefs(buildPayload(values))
			toast.success(t('settings.smtp.toast.updateSuccess'))
			form.reset(values)
			setInitial(form.getValues())
		} catch (err) {
			toast.error(String(err))
		}
	}

	// onTest 用管理员已保存的 SMTP 配置给自己发送一封样例邮件：
	// template 为空 = 通用连通性测试；传业务模板枚举时渲染对应通知内容
	const onTest = async (template?: string) => {
		const key = template || 'test'
		setTesting(key)
		try {
			await sendNotifyTest({ template })
			toast.success(t('settings.smtp.toast.testSent'))
		} catch (err) {
			toast.error(String(err))
		} finally {
			setTesting(null)
		}
	}

	// 通知开关行的"测试"按钮：用指定模板发送样例邮件（发给本人）
	const notifyTestButton = (template: string) => (
		<Button
			type="button"
			variant="outline"
			size="sm"
			className="h-7 px-2 text-xs"
			disabled={!!testing}
			onClick={() => onTest(template)}
		>
			{testing === template ? t('settings.smtp.toast.testSending') : t('settings.smtp.actions.test')}
		</Button>
	)

	if (loading || !initial) {
		return (
			<div className="flex items-center justify-center h-64">
				<p className="text-gray-500">{t('settings.smtp.loading')}</p>
			</div>
		)
	}

	return (
		<Card className="max-w-lg mx-auto">
			<CardHeader className="border-b">
				<CardTitle>{t('settings.smtp.title')}</CardTitle>
				<CardDescription>{t('settings.smtp.description')}</CardDescription>
			</CardHeader>
			<CardContent>
				<Form {...form}>
					<form className="space-y-6 pt-2">
						{/* 下线通知开关，默认全部关闭 */}
						<FormField
							control={form.control}
							name="enable_server_offline"
							render={({ field }) => (
								<FormItem className="flex flex-row items-center justify-between rounded-lg border p-3 shadow-sm">
									<div className="space-y-0.5">
										<FormLabel>{t('settings.smtp.form.serverOfflineLabel')}</FormLabel>
										<FormDescription>{t('settings.smtp.form.serverOfflineDescription')}</FormDescription>
									</div>
									<FormControl>
										<div className="flex items-center gap-2">
											{notifyTestButton('server_offline')}
											<Switch checked={field.value} onCheckedChange={field.onChange} />
										</div>
									</FormControl>
								</FormItem>
							)}
						/>
						<FormField
							control={form.control}
							name="enable_client_offline"
							render={({ field }) => (
								<FormItem className="flex flex-row items-center justify-between rounded-lg border p-3 shadow-sm">
									<div className="space-y-0.5">
										<FormLabel>{t('settings.smtp.form.clientOfflineLabel')}</FormLabel>
										<FormDescription>{t('settings.smtp.form.clientOfflineDescription')}</FormDescription>
									</div>
									<FormControl>
										<div className="flex items-center gap-2">
											{notifyTestButton('client_offline')}
											<Switch checked={field.value} onCheckedChange={field.onChange} />
										</div>
									</FormControl>
								</FormItem>
							)}
						/>
						<FormField
							control={form.control}
							name="enable_proxy_offline"
							render={({ field }) => (
								<FormItem className="flex flex-row items-center justify-between rounded-lg border p-3 shadow-sm">
									<div className="space-y-0.5">
										<FormLabel>{t('settings.smtp.form.proxyOfflineLabel')}</FormLabel>
										<FormDescription>{t('settings.smtp.form.proxyOfflineDescription')}</FormDescription>
									</div>
									<FormControl>
										<div className="flex items-center gap-2">
											{notifyTestButton('proxy_offline')}
											<Switch checked={field.value} onCheckedChange={field.onChange} />
										</div>
									</FormControl>
								</FormItem>
							)}
						/>
						{/* 下线通知防抖：离线持续超过所选时长才发送提醒 */}
						<div className="rounded-lg border p-3 shadow-sm">
							<FormLabel className="font-medium">{t('settings.smtp.form.offlineDelaySection')}</FormLabel>
							<FormDescription className="text-xs mb-3">
								{t('settings.smtp.form.offlineDelayHint')}
							</FormDescription>
							<div className="grid grid-cols-3 gap-3">
								<FormField
									control={form.control}
									name="offline_delay_server"
									render={({ field }) => (
										<FormItem>
											<FormLabel className="text-xs">{t('settings.smtp.form.offlineDelayServer')}</FormLabel>
											<Select
												value={String(field.value ?? 0)}
												onValueChange={(v) => field.onChange(Number(v))}
											>
												<FormControl>
													<SelectTrigger>
														<SelectValue />
													</SelectTrigger>
												</FormControl>
												<SelectContent>
													{OFFLINE_DELAY_OPTIONS.map((opt) => (
														<SelectItem key={opt.value} value={String(opt.value)}>
															{t(`settings.smtp.form.${opt.key}`)}
														</SelectItem>
													))}
												</SelectContent>
											</Select>
										</FormItem>
									)}
								/>
								<FormField
									control={form.control}
									name="offline_delay_client"
									render={({ field }) => (
										<FormItem>
											<FormLabel className="text-xs">{t('settings.smtp.form.offlineDelayClient')}</FormLabel>
											<Select
												value={String(field.value ?? 0)}
												onValueChange={(v) => field.onChange(Number(v))}
											>
												<FormControl>
													<SelectTrigger>
														<SelectValue />
													</SelectTrigger>
												</FormControl>
												<SelectContent>
													{OFFLINE_DELAY_OPTIONS.map((opt) => (
														<SelectItem key={opt.value} value={String(opt.value)}>
															{t(`settings.smtp.form.${opt.key}`)}
														</SelectItem>
													))}
												</SelectContent>
											</Select>
										</FormItem>
									)}
								/>
								<FormField
									control={form.control}
									name="offline_delay_proxy"
									render={({ field }) => (
										<FormItem>
											<FormLabel className="text-xs">{t('settings.smtp.form.offlineDelayProxy')}</FormLabel>
											<Select
												value={String(field.value ?? 0)}
												onValueChange={(v) => field.onChange(Number(v))}
											>
												<FormControl>
													<SelectTrigger>
														<SelectValue />
													</SelectTrigger>
												</FormControl>
												<SelectContent>
													{OFFLINE_DELAY_OPTIONS.map((opt) => (
														<SelectItem key={opt.value} value={String(opt.value)}>
															{t(`settings.smtp.form.${opt.key}`)}
														</SelectItem>
													))}
												</SelectContent>
											</Select>
										</FormItem>
									)}
								/>
							</div>
						</div>

						{/* 上线提醒开关 */}
						<FormField
							control={form.control}
							name="enable_server_online"
							render={({ field }) => (
								<FormItem className="flex flex-row items-center justify-between rounded-lg border p-3 shadow-sm">
									<div className="space-y-0.5">
										<FormLabel>{t('settings.smtp.form.serverOnlineLabel')}</FormLabel>
										<FormDescription>{t('settings.smtp.form.serverOnlineDescription')}</FormDescription>
									</div>
									<FormControl>
										<div className="flex items-center gap-2">
											{notifyTestButton('server_online')}
											<Switch checked={field.value} onCheckedChange={field.onChange} />
										</div>
									</FormControl>
								</FormItem>
							)}
						/>
						<FormField
							control={form.control}
							name="enable_client_online"
							render={({ field }) => (
								<FormItem className="flex flex-row items-center justify-between rounded-lg border p-3 shadow-sm">
									<div className="space-y-0.5">
										<FormLabel>{t('settings.smtp.form.clientOnlineLabel')}</FormLabel>
										<FormDescription>{t('settings.smtp.form.clientOnlineDescription')}</FormDescription>
									</div>
									<FormControl>
										<div className="flex items-center gap-2">
											{notifyTestButton('client_online')}
											<Switch checked={field.value} onCheckedChange={field.onChange} />
										</div>
									</FormControl>
								</FormItem>
							)}
						/>
						<FormField
							control={form.control}
							name="enable_proxy_online"
							render={({ field }) => (
								<FormItem className="flex flex-row items-center justify-between rounded-lg border p-3 shadow-sm">
									<div className="space-y-0.5">
										<FormLabel>{t('settings.smtp.form.proxyOnlineLabel')}</FormLabel>
										<FormDescription>{t('settings.smtp.form.proxyOnlineDescription')}</FormDescription>
									</div>
									<FormControl>
										<div className="flex items-center gap-2">
											{notifyTestButton('proxy_online')}
											<Switch checked={field.value} onCheckedChange={field.onChange} />
										</div>
									</FormControl>
								</FormItem>
							)}
						/>
						{/* 流量超额与定时规则触发的自动启停提醒 */}
						<FormField
							control={form.control}
							name="enable_quota_exceeded"
							render={({ field }) => (
								<FormItem className="flex flex-row items-center justify-between rounded-lg border p-3 shadow-sm">
									<div className="space-y-0.5">
										<FormLabel>{t('settings.smtp.form.quotaExceededLabel')}</FormLabel>
										<FormDescription>{t('settings.smtp.form.quotaExceededDescription')}</FormDescription>
									</div>
									<FormControl>
										<div className="flex items-center gap-2">
											{notifyTestButton('quota_exceeded')}
											<Switch checked={field.value} onCheckedChange={field.onChange} />
										</div>
									</FormControl>
								</FormItem>
							)}
						/>
						<FormField
							control={form.control}
							name="enable_schedule_toggle"
							render={({ field }) => (
								<FormItem className="flex flex-row items-center justify-between rounded-lg border p-3 shadow-sm">
									<div className="space-y-0.5">
										<FormLabel>{t('settings.smtp.form.scheduleToggleLabel')}</FormLabel>
										<FormDescription>{t('settings.smtp.form.scheduleToggleDescription')}</FormDescription>
									</div>
									<FormControl>
										<div className="flex items-center gap-2">
											{notifyTestButton('schedule_toggle')}
											<Switch checked={field.value} onCheckedChange={field.onChange} />
										</div>
									</FormControl>
								</FormItem>
							)}
						/>
						<FormField
							control={form.control}
							name="enable_login_notify"
							render={({ field }) => (
								<FormItem className="flex flex-row items-center justify-between rounded-lg border p-3 shadow-sm">
									<div className="space-y-0.5">
										<FormLabel>{t('settings.smtp.form.loginLabel')}</FormLabel>
										<FormDescription>{t('settings.smtp.form.loginDescription')}</FormDescription>
									</div>
									<FormControl>
										<div className="flex items-center gap-2">
											{notifyTestButton('login')}
											<Switch checked={field.value} onCheckedChange={field.onChange} />
										</div>
									</FormControl>
								</FormItem>
							)}
						/>
					</form>
				</Form>
			</CardContent>
			<CardFooter className="flex justify-end gap-2">
				{/* 通用连通性测试（发给本人） */}
				<Button variant="outline" disabled={!!testing} onClick={() => onTest()}>
					{testing === 'test' ? t('settings.smtp.toast.testSending') : t('settings.smtp.actions.sendTest')}
				</Button>
				<Dialog open={open} onOpenChange={setOpen}>
					<DialogTrigger asChild>
						<Button
							disabled={JSON.stringify(form.getValues()) === JSON.stringify(initial)}
							onClick={() => setOpen(true)}
						>
							{t('settings.smtp.actions.saveChanges')}
						</Button>
					</DialogTrigger>
					<DialogContent>
						<DialogHeader>
							<DialogTitle>{t('settings.smtp.dialog.confirmSaveTitle')}</DialogTitle>
							<DialogDescription>{t('settings.smtp.dialog.confirmSaveDescription')}</DialogDescription>
						</DialogHeader>
						<DialogFooterUI>
							<Button variant={'destructive'} onClick={() => setOpen(false)}>
								{t('settings.smtp.dialog.cancel')}
							</Button>
							<Button
								variant={'secondary'}
								onClick={() => {
									form.handleSubmit(onSubmit)()
									setOpen(false)
								}}
							>
								{t('settings.smtp.dialog.confirm')}
							</Button>
						</DialogFooterUI>
					</DialogContent>
				</Dialog>
			</CardFooter>
		</Card>
	)
}
