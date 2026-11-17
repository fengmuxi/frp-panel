import { toast } from 'sonner'
import { useTranslation } from 'react-i18next'
import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import * as z from 'zod'
import { useStore } from '@nanostores/react'
import { $userInfo } from '@/store/user'

import { SmtpSettings, getSmtpSettings, sendTestEmail, updateSmtpSettings } from '@/api/platform'

import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage, FormDescription } from '@/components/ui/form'
import { Input } from '@/components/ui/input'
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

// 清空输入框时值为 ""，预处理为 undefined，避免 coerce 出 0 或阻塞必填校验（同 frps_form 惯例）
const emptyToUndefined = (v: unknown) => (v === '' || v === null ? undefined : v)

// SMTP 服务器连接配置（仅管理员）；通知开关按用户独立，见 notify-prefs.tsx
const SmtpSchema = z.object({
	host: z.string().trim().min(1),
	port: z.preprocess(emptyToUndefined, z.coerce.number().min(1).max(65535).default(465)),
	username: z.string(),
	// 密码留空表示保留已保存的密码
	password: z.preprocess(emptyToUndefined, z.string().optional()),
	from: z.string().trim().min(1),
	// 发件人显示名称（可选），留空则仅显示地址
	fromName: z.string().trim().optional(),
	encryption: z.enum(['none', 'starttls', 'ssl']),
})

type SmtpFormValues = z.infer<typeof SmtpSchema>

export function SmtpSettingsForm() {
	const { t } = useTranslation()
	const [loading, setLoading] = useState(true)
	const [initial, setInitial] = useState<SmtpFormValues | null>(null)
	const [open, setOpen] = useState(false)
	const [hasPassword, setHasPassword] = useState(false)
	const [testing, setTesting] = useState(false)

	const userInfo = useStore($userInfo)
	// SMTP 服务器配置仅管理员可设置
	const isAdmin = userInfo?.role === 'admin'

	const form = useForm<SmtpFormValues>({
		resolver: zodResolver(SmtpSchema),
		defaultValues: {
			host: '',
			port: 465,
			username: '',
			password: '',
			from: '',
			fromName: '',
			encryption: 'none',
		},
	})

	// 组件挂载时从服务端读取 SMTP 配置
	useEffect(() => {
		if (!isAdmin) {
			setLoading(false)
			return
		}
		let canceled = false
		;(async () => {
			try {
				const s = await getSmtpSettings()
				if (canceled) return
				const values: SmtpFormValues = {
					host: s.host ?? '',
					port: s.port || 465,
					username: s.username ?? '',
					password: '',
					from: s.from ?? '',
					fromName: s.from_name ?? '',
					encryption: (s.encryption || 'none') as SmtpFormValues['encryption'],
				}
				setHasPassword(!!s.has_password)
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
	}, [isAdmin])

	const buildPayload = (values: SmtpFormValues): SmtpSettings => ({
		host: values.host,
		port: values.port,
		username: values.username,
		password: values.password ?? '',
		has_password: hasPassword,
		from: values.from,
		from_name: values.fromName ?? '',
		encryption: values.encryption,
	})

	const onSubmit = async (values: SmtpFormValues) => {
		try {
			const saved = await updateSmtpSettings(buildPayload(values))
			setHasPassword(saved.has_password)
			toast.success(t('settings.smtp.toast.updateSuccess'))
			// 保存成功后清空密码输入，回显"已保存"
			form.reset({ ...values, password: '' })
			setInitial(form.getValues())
		} catch (err) {
			toast.error(String(err))
		}
	}

	// onTest 使用当前表单（未保存）值发送通用连通性测试邮件
	const onTest = async (values: SmtpFormValues) => {
		setTesting(true)
		try {
			await sendTestEmail(buildPayload(values))
			toast.success(t('settings.smtp.toast.testSent'))
		} catch (err) {
			toast.error(String(err))
		} finally {
			setTesting(false)
		}
	}

	// 非管理员不显示 SMTP 服务器卡片（通知开关卡片对所有人可见）
	if (!isAdmin) {
		return null
	}

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
				<CardTitle>{t('settings.smtp.serverTitle')}</CardTitle>
				<CardDescription>{t('settings.smtp.serverDescription')}</CardDescription>
			</CardHeader>
			<CardContent>
				<Form {...form}>
					<form className="space-y-6 pt-2">
						{/* SMTP 服务器 */}
						<div className="grid grid-cols-3 gap-4">
							<FormField
								control={form.control}
								name="host"
								render={({ field }) => (
									<FormItem className="col-span-2">
										<FormLabel>{t('settings.smtp.form.host')}</FormLabel>
										<FormControl>
											<Input placeholder={t('settings.smtp.form.hostPlaceholder')} {...field} />
										</FormControl>
										<FormMessage />
									</FormItem>
								)}
							/>
							<FormField
								control={form.control}
								name="port"
								render={({ field }) => (
									<FormItem>
										<FormLabel>{t('settings.smtp.form.port')}</FormLabel>
										<FormControl>
											<Input
												inputMode="numeric"
												placeholder={t('settings.smtp.form.portPlaceholder')}
												value={field.value ?? ''}
												onChange={(e) => field.onChange(e.target.value)}
												onBlur={field.onBlur}
												name={field.name}
												ref={field.ref}
											/>
										</FormControl>
										<FormMessage />
									</FormItem>
								)}
							/>
						</div>

						{/* 加密方式 */}
						<FormField
							control={form.control}
							name="encryption"
							render={({ field }) => (
								<FormItem>
									<FormLabel>{t('settings.smtp.form.encryption')}</FormLabel>
									<Select
										onValueChange={(v) => {
											field.onChange(v)
											// 端口联动：仅当当前端口是常见 SMTP 端口时自动跟随切换，
											// 用户自定义过端口则不打扰
											const cur = Number(form.getValues('port'))
											const defaults: Record<string, number> = { ssl: 465, starttls: 587, none: 25 }
											if ([465, 587, 25].includes(cur) && defaults[v]) {
												form.setValue('port', defaults[v])
											}
										}}
										value={field.value}
									>
										<FormControl>
											<SelectTrigger>
												<SelectValue placeholder={t('settings.smtp.form.encryption')} />
											</SelectTrigger>
										</FormControl>
										<SelectContent>
											<SelectItem value="none">{t('settings.smtp.form.encryptionNone')}</SelectItem>
											<SelectItem value="starttls">{t('settings.smtp.form.encryptionStarttls')}</SelectItem>
											<SelectItem value="ssl">{t('settings.smtp.form.encryptionSsl')}</SelectItem>
										</SelectContent>
									</Select>
									<FormDescription className="text-xs">
										{t('settings.smtp.form.encryptionHint')}
									</FormDescription>
									<FormMessage />
								</FormItem>
							)}
						/>

						{/* 账号密码 */}
						<div className="grid grid-cols-2 gap-4">
							<FormField
								control={form.control}
								name="username"
								render={({ field }) => (
									<FormItem>
										<FormLabel>{t('settings.smtp.form.username')}</FormLabel>
										<FormControl>
											<Input placeholder={t('settings.smtp.form.usernamePlaceholder')} {...field} />
										</FormControl>
										<FormMessage />
									</FormItem>
								)}
							/>
							<FormField
								control={form.control}
								name="password"
								render={({ field }) => (
									<FormItem>
										<FormLabel>{t('settings.smtp.form.password')}</FormLabel>
										<FormControl>
											<Input
												type="password"
												autoComplete="new-password"
												placeholder={
													hasPassword ? t('settings.smtp.form.passwordSaved') : t('settings.smtp.form.passwordPlaceholder')
												}
												{...field}
											/>
										</FormControl>
										<FormMessage />
									</FormItem>
								)}
							/>
						</div>

						{/* 发件人 */}
						<FormField
							control={form.control}
							name="from"
							render={({ field }) => (
								<FormItem>
									<FormLabel>{t('settings.smtp.form.from')}</FormLabel>
									<FormControl>
										<Input placeholder={t('settings.smtp.form.fromPlaceholder')} {...field} />
									</FormControl>
									<FormMessage />
								</FormItem>
							)}
						/>

						{/* 发件人名称（可选）：收件方显示的名称，如 FRP-Panel <noreply@example.com> */}
						<FormField
							control={form.control}
							name="fromName"
							render={({ field }) => (
								<FormItem>
									<FormLabel>{t('settings.smtp.form.fromName')}</FormLabel>
									<FormControl>
										<Input placeholder={t('settings.smtp.form.fromNamePlaceholder')} {...field} />
									</FormControl>
									<FormDescription className="text-xs">
										{t('settings.smtp.form.fromNameHint')}
									</FormDescription>
									<FormMessage />
								</FormItem>
							)}
						/>
					</form>
				</Form>
			</CardContent>
			<CardFooter className="flex justify-between gap-2">
				{/* 发送测试邮件：使用当前表单（未保存）值，收件人为当前登录用户 */}
				<Button variant="outline" disabled={testing || form.formState.isSubmitting} onClick={form.handleSubmit(onTest)}>
					{testing ? t('settings.smtp.toast.testSending') : t('settings.smtp.actions.sendTest')}
				</Button>
				<Dialog open={open} onOpenChange={setOpen}>
					<DialogTrigger asChild>
						<Button
							disabled={
								form.formState.isSubmitting ||
								JSON.stringify(form.getValues()) === JSON.stringify(initial)
							}
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
