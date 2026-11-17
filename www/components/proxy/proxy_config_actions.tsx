import { ProxyType, TypedProxyConfig } from '@/types/proxy'
import { useTranslation } from 'react-i18next'
import { Button } from '../ui/button'
import { BaseDropdownMenu } from '../base/drop-down-menu'
import { deleteProxyConfig, startProxy, stopProxy } from '@/api/proxy'
import { useMutation } from '@tanstack/react-query'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { ProxyConfigMutateForm } from './mutate_proxy_config'
import { ProxyRulesDialog } from './proxy_rules_dialog'
import { ProxyTrafficDetailDialog } from './proxy_traffic_detail_dialog'
import { ResourceNotifyDialog } from '@/components/platform/resource_notify_dialog'
import { useEffect, useMemo, useState } from 'react'
import { Row } from '@tanstack/react-table'
import { ProxyConfigTableSchema } from './proxy_config_item'
import { MoreHorizontal } from 'lucide-react'
import { toast } from 'sonner'
import { $proxyTableRefetchTrigger } from '@/store/refetch-trigger'
import { DropdownMenuGroup, DropdownMenuItem } from '../ui/dropdown-menu'

export interface ProxyConfigActionsProps {
  serverID: string
  clientID: string
  name: string
  row: Row<ProxyConfigTableSchema>
}

export function ProxyConfigActions({ serverID, clientID, name, row }: ProxyConfigActionsProps) {
  const { t } = useTranslation()
  const [proxyMutateFormOpen, setProxyMutateFormOpen] = useState(false)
  const [deleteWarnDialogOpen, setDeleteWarnDialogOpen] = useState(false)
  const [rulesDialogOpen, setRulesDialogOpen] = useState(false)
  const [trafficDialogOpen, setTrafficDialogOpen] = useState(false)
  const [notifyDialogOpen, setNotifyDialogOpen] = useState(false)
  const proxyConfig = row.original
  // 资源级通知配置按 proxy_config 行主键关联（与隧道改名无关）
  const proxyConfigId = row.original.originalProxyConfig.id

  const deleteProxyConfigMutation = useMutation({
    mutationKey: ['deleteProxyConfig', serverID, clientID, name],
    mutationFn: () =>
      deleteProxyConfig({
        serverId: serverID,
        clientId: clientID,
        name,
      }),
    onSuccess: () => {
      toast(t('proxy.action.delete_success'))
      $proxyTableRefetchTrigger.set(Math.random())
    },
    onError: (e) => {
      toast(t('proxy.action.delete_failed'), {
        description: JSON.stringify(e),
      })
      $proxyTableRefetchTrigger.set(Math.random())
    },
  })

  const stopProxyMutation = useMutation({
    mutationKey: ['stopProxy', serverID, clientID, name],
    mutationFn: () =>
      stopProxy({
        serverId: serverID,
        clientId: clientID,
        name,
      }),
    onSuccess: () => {
      toast(t('proxy.action.stop_success'))
      $proxyTableRefetchTrigger.set(Math.random())
    },
    onError: (e) => {
      toast(t('proxy.action.stop_failed'), {
        description: JSON.stringify(e),
      })
      $proxyTableRefetchTrigger.set(Math.random())
    },
  })

  const startProxyMutation = useMutation({
    mutationKey: ['startProxy', serverID, clientID, name],
    mutationFn: () =>
      startProxy({
        serverId: serverID,
        clientId: clientID,
        name,
      }),
    onSuccess: () => {
      toast(t('proxy.action.start_success'))
      $proxyTableRefetchTrigger.set(Math.random())
    },
    onError: (e) => {
      toast(t('proxy.action.start_failed'), {
        description: JSON.stringify(e),
      })
      $proxyTableRefetchTrigger.set(Math.random())
    },
  })

  const menuActions = [
    [
      {
        name: t('proxy.action.edit'),
        onClick: () => {
          setProxyMutateFormOpen(true)
        },
      },
      {
        name: t('proxy.rules.menu'),
        onClick: () => {
          setRulesDialogOpen(true)
        },
      },
      {
        name: t('traffic.chart.title'),
        onClick: () => {
          setTrafficDialogOpen(true)
        },
      },
      // 行主键缺失（异常数据）时不提供通知设置入口，避免无效请求
      ...(proxyConfigId
        ? [
            {
              name: t('settings.notifyResource.menuTitle'),
              onClick: () => {
                setNotifyDialogOpen(true)
              },
            },
          ]
        : []),
      {
        name: t('proxy.action.delete'),
        onClick: () => {
          setDeleteWarnDialogOpen(true)
        },
        className: 'text-destructive',
      },
    ],
  ]

  const extraButtons = useMemo(
    () => (
      <DropdownMenuGroup>
        {!proxyConfig.stopped && (
          <DropdownMenuItem className="text-destructive" onClick={() => stopProxyMutation.mutate()}>
            {t('client.actions_menu.pause')}
          </DropdownMenuItem>
        )}
        {proxyConfig.stopped && (
          <DropdownMenuItem onClick={() => startProxyMutation.mutate()}>
            {t('client.actions_menu.resume')}
          </DropdownMenuItem>
        )}
      </DropdownMenuGroup>
    ),
    [proxyConfig, startProxyMutation, stopProxyMutation, t],
  )

  return (
    <>
      <Dialog open={proxyMutateFormOpen} onOpenChange={setProxyMutateFormOpen}>
        <DialogContent className="max-h-[90vh] overflow-auto">
          <ProxyConfigMutateForm
            disableChangeProxyName
            defaultProxyConfig={JSON.parse(row.original.config || '{}') as TypedProxyConfig}
            overwrite={true}
            defaultOriginalProxyConfig={row.original.originalProxyConfig}
            onSuccess={() => setProxyMutateFormOpen(false)}
          />
        </DialogContent>
      </Dialog>
      <ProxyRulesDialog
        open={rulesDialogOpen}
        onOpenChange={setRulesDialogOpen}
        proxy={row.original.originalProxyConfig}
      />
      <ProxyTrafficDetailDialog
        open={trafficDialogOpen}
        onOpenChange={setTrafficDialogOpen}
        clientID={clientID}
        serverID={serverID}
        proxyName={name}
        proxyType={row.original.type}
      />
      {/* 通知提醒设置：三段式（不提醒/全局配置/独立配置） */}
      {proxyConfigId ? (
        <ResourceNotifyDialog
          open={notifyDialogOpen}
          onOpenChange={setNotifyDialogOpen}
          resourceType="proxy"
          resourceId={String(proxyConfigId)}
          resourceLabel={name}
        />
      ) : null}
      <Dialog open={deleteWarnDialogOpen} onOpenChange={setDeleteWarnDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('proxy.action.delete_tunnel')}</DialogTitle>
            <DialogDescription>
              <p className="text-destructive">{t('proxy.action.delete_attention_title')}</p>
              <p className="text-gray-500 border-l-4 border-gray-500 pl-4 py-2">
                {t('proxy.action.delete_attention_description')}
              </p>
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <DialogClose asChild>
              <Button
                type="submit"
                onClick={() => {
                  deleteProxyConfigMutation.mutate()
                }}
              >
                {t('proxy.action.delete_attention_confirm')}
              </Button>
            </DialogClose>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <BaseDropdownMenu
        menuGroup={menuActions}
        title={t('proxy.action.title')}
        trigger={
          <Button variant="ghost" className="h-8 w-8 p-0">
            <MoreHorizontal className="h-4 w-4" />
          </Button>
        }
        extraButtons={extraButtons}
      />
    </>
  )
}
