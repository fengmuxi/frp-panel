import { ServerConfig } from '@/types/server'
import { useEffect, useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
import * as z from 'zod'
import { Button } from '@/components/ui/button'
import { Form } from '@/components/ui/form'
import { ZodIPSchema, ZodPortSchema, ZodStringSchema } from '@/lib/consts'
import { RespCode, Server } from '@/lib/pb/common'
import { updateFRPS } from '@/api/frp'
import { useMutation } from '@tanstack/react-query'
import { Label } from '@radix-ui/react-label'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { HostField, PortField } from '../base/form-field'
import { Checkbox } from '../ui/checkbox'
import { FormDescription } from '../ui/form'

// 清空输入框时值为 ""，预处理为 undefined，使 optional 字段可通过校验，
// 且 JSON.stringify 时省略该键，从而真正从配置中移除该字段
const emptyToUndefined = (v: unknown) => (v === '' || v === null ? undefined : v)

const ServerConfigSchema = z.object({
  bindAddr: z.preprocess(emptyToUndefined, ZodIPSchema.default('0.0.0.0').optional()),
  bindPort: ZodPortSchema.default(7000),
  // proxyBindAddr 默认 0.0.0.0：留空会被 frps 继承为主监听地址（bindAddr），
  // 主监听是公网 IP 时隧道端口将无法通过 127.0.0.1 被本机反代访问
  proxyBindAddr: z.preprocess(emptyToUndefined, ZodIPSchema.default('0.0.0.0').optional()),
  vhostHTTPPort: z.preprocess(emptyToUndefined, ZodPortSchema.optional()),
  subDomainHost: z.preprocess(emptyToUndefined, ZodStringSchema.optional()),
  publicHost: z.preprocess(emptyToUndefined, ZodStringSchema.optional()),
  quicBindPort: z.preprocess(emptyToUndefined, ZodPortSchema.optional()),
  kcpBindPort: z.preprocess(emptyToUndefined, ZodPortSchema.optional()),
})

export const ServerConfigZodSchema = ServerConfigSchema

export interface FRPSFormProps {
  serverID: string
  server: Server
  frpsUrls: string[]
}

const FRPSForm: React.FC<FRPSFormProps> = ({ serverID, server, frpsUrls }) => {
  const { t } = useTranslation()
  const form = useForm<z.infer<typeof ServerConfigZodSchema>>({
    resolver: zodResolver(ServerConfigZodSchema),
  })
  // 保存后是否自动同步下游隧道连接配置（重建 ServerAddr/ServerPort 并热更新）。
  // 不属于 frps 配置 JSON 的一部分，单独用状态管理
  const [syncClients, setSyncClients] = useState(true)

  const updateFrps = useMutation({ mutationFn: updateFRPS })

  useEffect(() => {
    form.reset({})
  }, [])

  useEffect(() => {
    form.reset(JSON.parse(server?.config || '{}') as ServerConfig)
  }, [server])

  const onSubmit = async (values: z.infer<typeof ServerConfigZodSchema>) => {
    try {
      const { publicHost, ...rest } = values
      let resp = await updateFrps.mutateAsync({
        serverIp: publicHost,
        serverId: serverID,
        frpsUrls: frpsUrls,
        syncClients: syncClients,
        // @ts-ignore
        config: Buffer.from(
          JSON.stringify({
            ...rest,
          } as ServerConfig),
        ),
      })
      toast(resp.status?.code === RespCode.SUCCESS ? t('server.operation.update_success') : t('server.operation.update_failed'), {
        description: resp.status?.message,
      })
    } catch (error) {
      console.error(error)
      toast(t('server.operation.update_title'), {
        description: t('server.operation.update_failed')
      })
    }
  }

  return (
    <div className="flex flex-col w-full pt-2">
      <Label className="text-sm font-medium">{t('server.form.comment_title', { id: server?.comment || t('server.name_placeholder') })}</Label>
      <p className="text-sm text-muted-foreground">{t('server.form.comment_hint')}</p>
      <p className="text-sm border rounded p-2 my-2">
        {server?.comment == undefined || server?.comment === '' ? t('server.form.comment_empty') : server?.comment}
      </p>
      {serverID && (
        <Form {...form}>
          <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-4 px-0.5">
            <HostField name="publicHost" label={t('server.form.public_host')} hint="server.form.public_host_hint" placeholder='8.8.8.8' control={form.control} defaultValue={server?.ip}/>
            <PortField name="bindPort" label={t('server.form.bind_port')} hint="server.form.bind_port_hint" control={form.control} />
            <HostField name="bindAddr" label={t('server.form.bind_addr')} hint="server.form.bind_addr_hint" control={form.control} />
            <HostField name="proxyBindAddr" label={t('server.form.proxy_bind_addr')} hint="server.form.proxy_bind_addr_hint" control={form.control} />
            <PortField name="vhostHTTPPort" label={t('server.form.vhost_http_port')} hint="server.form.vhost_http_port_hint" control={form.control} />
            <HostField name="subDomainHost" label={t('server.form.subdomain_host')} hint="server.form.subdomain_host_hint" control={form.control} />
            <PortField name="quicBindPort" label={t('server.form.quic_bind_port')} hint="server.form.quic_bind_port_hint" control={form.control} />
            <PortField name="kcpBindPort" label={t('server.form.kcp_bind_port')} hint="server.form.kcp_bind_port_hint" control={form.control} />
            <div className="flex items-start justify-between rounded-lg border p-3">
              <div className="space-y-0.5">
                <Label className="text-sm font-medium">{t('server.form.sync_clients')}</Label>
                <p className="text-xs text-muted-foreground">{t('server.form.sync_clients_hint')}</p>
              </div>
              <Checkbox checked={syncClients} onCheckedChange={(v) => setSyncClients(v === true)} className="mt-1" />
            </div>
            <Button type="submit">{t('common.submit')}</Button>
          </form>
        </Form>
      )}
    </div>
  )
}

export default FRPSForm
