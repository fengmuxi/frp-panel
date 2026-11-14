"use client"

import { useEffect, useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { updateFRPC } from '@/api/frp'
import { Label } from '@/components/ui/label'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { RespCode, Client } from '@/lib/pb/common'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { TypedProxyForm } from './proxy_form'
import { ProxyType, TypedProxyConfig } from '@/types/proxy'
import { ClientConfig } from '@/types/client'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { QueryObserverResult, RefetchOptions } from '@tanstack/react-query'
import { GetClientResponse } from '@/lib/pb/api_client'

// 快速创建隧道支持的类型（与 TypedProxyForm 支持的类型保持一致）
const QUICK_PROXY_TYPES: ProxyType[] = ['tcp', 'udp', 'http', 'stcp']

// QUICK_PROXY_CREATE_FORM_ID 暴露内部表单 id，弹窗底部按钮通过原生
// form 属性远程触发 TypedProxyForm 的提交（走它的 zod 校验）
const QUICK_PROXY_CREATE_FORM_ID = 'quick-proxy-create-form'

export interface QuickProxyCreateDialogProps {
  clientID: string
  serverID: string
  frpsUrl?: string
  client?: Client
  clientConfig: ClientConfig
  clientProxyConfigs: TypedProxyConfig[]
  refetchClient: (options?: RefetchOptions) => Promise<QueryObserverResult<GetClientResponse, Error>>
}

// QuickProxyCreateDialog 把「新增隧道」收敛为一步：
// 弹窗内填名称（自动生成）+ 类型 + 端口等最少字段，点击「创建」即提交并下发，
// 不再需要「本地添加 → 展开条目 → 保存 → 提交变更」的多步流程。
export const QuickProxyCreateDialog: React.FC<QuickProxyCreateDialogProps> = ({
  clientID,
  serverID,
  frpsUrl,
  clientConfig,
  clientProxyConfigs,
  refetchClient,
}) => {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [proxyType, setProxyType] = useState<ProxyType>('tcp')
  const [proxyName, setProxyName] = useState<string>('')
  const [nameTouched, setNameTouched] = useState(false)
  // 草稿数组只包含正在创建的这条，TypedProxyForm 按 name 写回草稿
  const [draftConfigs, setDraftConfigs] = useState<TypedProxyConfig[]>([])

  // 生成不与现有隧道冲突的默认名称，如 tcp-1、tcp-2……
  const genDefaultName = (type: ProxyType, existing: TypedProxyConfig[]) => {
    const names = new Set(existing.map((p) => p.name))
    for (let i = 1; i < 1000; i++) {
      const candidate = `${type}-${i}`
      if (!names.has(candidate)) {
        return candidate
      }
    }
    return `${type}-${Math.random().toString(36).slice(2, 6)}`
  }

  useEffect(() => {
    if (open) {
      setProxyType('tcp')
      setProxyName(genDefaultName('tcp', clientProxyConfigs))
      setNameTouched(false)
      setDraftConfigs([])
    }
    // 仅在弹窗打开时基于当时的隧道列表生成一次默认名，列表后续变化不打扰用户输入
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const switchType = (type: ProxyType) => {
    setProxyType(type)
    // 名称未被手动修改时跟随类型重新生成
    if (!nameTouched) {
      setProxyName(genDefaultName(type, clientProxyConfigs))
    }
  }

  useEffect(() => {
    // 名称/类型变化时重建草稿条目；已有草稿字段在同名重建时保留由 TypedProxyForm 内部状态维持
    setDraftConfigs([{ name: proxyName, type: proxyType } as TypedProxyConfig])
  }, [proxyName, proxyType])

  const createProxy = useMutation({ mutationFn: updateFRPC })

  const handleSaved = async (cfg: TypedProxyConfig) => {
    if (clientProxyConfigs.some((p) => p.name === cfg.name)) {
      toast(t('proxy.form.create_failed'), {
        description: t('proxy.status.name_exists'),
      })
      return
    }

    try {
      const finalProxies = [...clientProxyConfigs, cfg]
      const res = await createProxy.mutateAsync({
        // @ts-ignore 与 frpc_form 的提交结构保持一致
        config: Buffer.from(
          JSON.stringify({
            proxies: finalProxies,
            transport: clientConfig.transport,
          } as ClientConfig),
        ),
        serverId: serverID,
        clientId: clientID,
        frpsUrl: frpsUrl,
      })
      if (res.status?.code !== RespCode.SUCCESS) {
        toast(t('proxy.form.create_failed'), {
          description: res.status?.message,
        })
        return
      }
      toast(t('proxy.form.create_success'))
      await refetchClient()
      setOpen(false)
    } catch (error) {
      toast(t('proxy.form.create_failed'), {
        description: JSON.stringify(error),
      })
    }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button className="my-2">{t('proxy.form.add')}</Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-lg max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{t('proxy.form.quick_create_title')}</DialogTitle>
          <DialogDescription>{t('proxy.form.quick_create_desc')}</DialogDescription>
        </DialogHeader>

        <div className="space-y-3">
          <div className="space-y-1">
            <Label className="text-sm font-medium">{t('proxy.form.name')}</Label>
            <Input
              value={proxyName}
              onChange={(e) => {
                setNameTouched(true)
                setProxyName(e.target.value)
              }}
              placeholder={t('proxy.form.name')}
            />
          </div>
          <div className="space-y-1">
            <Label className="text-sm font-medium">{t('proxy.form.protocol')}</Label>
            <Select value={proxyType} onValueChange={(v) => switchType(v as ProxyType)}>
              <SelectTrigger>
                <SelectValue placeholder={t('proxy.form.type')} />
              </SelectTrigger>
              <SelectContent>
                {QUICK_PROXY_TYPES.map((type) => (
                  <SelectItem key={type} value={type}>
                    {t(`proxy.type.${type}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {serverID && clientID && (
            <TypedProxyForm
              enablePreview
              defaultProxyConfig={draftConfigs[0]}
              proxyName={proxyName}
              serverID={serverID}
              clientID={clientID}
              clientProxyConfigs={draftConfigs}
              setClientProxyConfigs={setDraftConfigs}
              formId={QUICK_PROXY_CREATE_FORM_ID}
              onSaved={handleSaved}
              hideActions
            />
          )}
        </div>

        <Button
          type="submit"
          form={QUICK_PROXY_CREATE_FORM_ID}
          disabled={createProxy.isPending}
          className="w-full"
        >
          {t('proxy.form.create')}
        </Button>
      </DialogContent>
    </Dialog>
  )
}
