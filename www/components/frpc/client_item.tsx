import { ColumnDef, Table, TableMeta } from '@tanstack/react-table'
import { ArrowDown, ArrowUp, MoreHorizontal, Pencil } from 'lucide-react'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'

import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import React, { useState } from 'react'
import { ClientEnvFile, ExecCommandStr, LinuxInstallCommand, WindowsInstallCommand } from '@/lib/consts'
import { useMutation, useQuery } from '@tanstack/react-query'
import { deleteClient, listClient, swapClientSort, updateClientInfo } from '@/api/client'
import { useRouter } from 'next/router'
import { useStore } from '@nanostores/react'
import { $frontendPreference, $platformInfo } from '@/store/user'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { getClientsStatus } from '@/api/platform'
import { Client, ClientType, RespCode } from '@/lib/pb/common'
import { ClientStatus, ClientStatus_Status } from '@/lib/pb/api_master'
import { startFrpc, stopFrpc } from '@/api/frp'
import { Badge } from '../ui/badge'
import { ResourceNotifyDialog } from '@/components/platform/resource_notify_dialog'
import { ClientDetail } from '../base/client_detail'
import { Input } from '../ui/input'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { $clientTableRefetchTrigger } from '@/store/refetch-trigger'
import { NeedUpgrade } from '@/config/notify'
import { Label } from '../ui/label'
import { Checkbox } from '../ui/checkbox'
import { ClientUpgradeDialog } from '../base/client_upgrade_dialog'
import { NodeInfoEditDialog } from '../base/node_info_edit_dialog'

export type ClientTableSchema = {
  id: string
  status: 'invalid' | 'valid'
  secret: string
  stopped: boolean
  info?: string
  config?: string
  comment?: string
  sortIndex?: number
  originClient: Client
  clientIds: string[]
}

export interface TableMetaType extends TableMeta<ClientTableSchema> {
  refetch: () => void
  /** 当前列表总条数（服务端分页），用于计算全局首/尾行禁用上下移按钮 */
  total?: number
  /** 是否允许调整排序（搜索/筛选时禁用） */
  sortEnabled?: boolean
}

export const columns: ColumnDef<ClientTableSchema>[] = [
  {
    accessorKey: 'comment',
    header: function Header() {
      const { t } = useTranslation()
      return t('client.name')
    },
    cell: ({ row }) => {
      return <ClientName client={row.original} />
    },
  },
  {
    accessorKey: 'sortIndex',
    header: function Header() {
      const { t } = useTranslation()
      return t('client.sort')
    },
    cell: ({ row }) => {
      return <ClientSort client={row.original} />
    },
  },
  {
    accessorKey: 'status',
    header: function Header() {
      const { t } = useTranslation()
      return t('client.status')
    },
    cell: ({ row }) => {
      function Cell({ client }: { client: ClientTableSchema }) {
        const { t } = useTranslation()
        return (
          <div className={`font-medium ${client.status === 'valid' ? 'text-green-500' : 'text-red-500'} min-w-12`}>
            {client.status === 'valid' ? t('client.status_configured') : t('client.status_unconfigured')}
          </div>
        )
      }
      return <Cell client={row.original} />
    },
  },
  {
    accessorKey: 'info',
    header: function Header() {
      const { t } = useTranslation()
      return t('client.info')
    },
    cell: ({ row }) => {
      const client = row.original
      return <ClientInfo client={client} />
    },
  },
  {
    accessorKey: 'secret',
    header: function Header() {
      const { t } = useTranslation()
      return t('client.secret')
    },
    cell: ({ row }) => {
      const client = row.original
      return <ClientSecret client={client} />
    },
  },
  {
    id: 'action',
    cell: ({ row, table }) => {
      const client = row.original
      return (
        <ClientActions
          client={client}
          rowIndex={row.index}
          table={table as Table<ClientTableSchema> & { options: { meta: TableMetaType } }}
        />
      )
    },
  },
]

export const ClientInfo = ({ client }: { client: ClientTableSchema }) => {
  const { t } = useTranslation()
  const { data: clientsStatus } = useQuery({
    queryKey: ['clientsStatus', client.id],
    queryFn: async () => {
      return await getClientsStatus({
        clientIds: [client.id],
        clientType: ClientType.FRPC,
      })
    },
  })

  const trans = (info: ClientStatus | undefined) => {
    let statusText:
      | 'client.status_online'
      | 'client.status_offline'
      | 'client.status_error'
      | 'client.status_pause'
      | 'client.status_unknown' = 'client.status_unknown'
    if (info === undefined) {
      return statusText
    }
    if (info.status === ClientStatus_Status.ONLINE) {
      statusText = 'client.status_online'
      if (client.stopped) {
        statusText = 'client.status_pause'
      }
    } else if (info.status === ClientStatus_Status.OFFLINE) {
      statusText = 'client.status_offline'
    } else if (info.status === ClientStatus_Status.ERROR) {
      statusText = 'client.status_error'
    }
    return statusText
  }

  const infoColor =
    clientsStatus?.clients[client.id]?.status === ClientStatus_Status.ONLINE
      ? client.stopped
        ? 'text-yellow-500'
        : 'text-green-500'
      : 'text-red-500'

  return (
    <div className="flex items-center gap-2 flex-row">
      <Badge variant={'secondary'} className={`p-2 border font-mono w-fit ${infoColor} text-nowrap rounded-full h-6`}>
        {`${clientsStatus?.clients[client.id].ping}ms,${t(trans(clientsStatus?.clients[client.id]))}`}
      </Badge>
      {clientsStatus?.clients[client.id].version && <ClientDetail clientStatus={clientsStatus?.clients[client.id]} />}
      {NeedUpgrade(clientsStatus?.clients[client.id].version) && (
        <Badge variant={'destructive'} className={`p-2 border font-mono w-fit text-nowrap rounded-full h-6`}>
          {t('client.need_upgrade')}
        </Badge>
      )}
      {client.originClient.ephemeral && (
        <Badge variant={'secondary'} className={`p-2 border font-mono w-fit text-nowrap rounded-full h-6`}>
          {t('client.temp_node')}
        </Badge>
      )}
    </div>
  )
}

export const ClientSecret = ({ client }: { client: ClientTableSchema }) => {
  const { t } = useTranslation()
  const platformInfo = useStore($platformInfo)

  if (!platformInfo) {
    return (
      <Button variant="link" className="px-0">
        {client.secret}
      </Button>
    )
  }

  return (
    <Popover>
      <PopoverTrigger asChild>
        <div className="group relative cursor-pointer inline-block font-mono text-nowrap">
          <span className="opacity-0 group-hover:opacity-100 transition-opacity duration-200">{client.secret}</span>
          <span className="absolute inset-0 opacity-100 group-hover:opacity-0 transition-opacity duration-200">
            {'*'.repeat(client.secret.length)}
          </span>
        </div>
      </PopoverTrigger>
      <PopoverContent className="w-[32rem] max-w-[95vw]">
        <div className="grid gap-4">
          <div className="space-y-2">
            <h4 className="font-medium leading-none">{t('client.start.title')}</h4>
            <p className="text-sm text-muted-foreground">
              {t('client.start.description')} (
              <a
                className="text-blue-500"
                href="https://github.com/VaalaCat/frp-panel/releases"
                target="_blank"
                rel="noopener noreferrer"
              >
                {t('common.download')}
              </a>
              )
            </p>
          </div>
          <div className="grid gap-2">
            <pre className="bg-muted p-3 rounded-md font-mono text-sm overflow-x-auto whitespace-pre-wrap break-all">
              {ExecCommandStr('client', client, platformInfo)}
            </pre>
            <Button
              size="sm"
              variant="outline"
              className="w-full"
              onClick={() => navigator.clipboard.writeText(ExecCommandStr('client', client, platformInfo))}
              disabled={!platformInfo}
            >
              {t('common.copy')}
            </Button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  )
}

export interface ClientItemProps {
  client: ClientTableSchema
  table: Table<ClientTableSchema> & { options: { meta: TableMetaType } }
  rowIndex: number
}

// 名称/序号编辑的公共状态与保存逻辑（名称列与序号列共用）
const useClientInfoEdit = (clientId: string) => {
  const { t } = useTranslation()
  const [editOpen, setEditOpen] = useState(false)
  const [saving, setSaving] = useState(false)

  const handleSave = async (comment: string, sortIndex: number) => {
    setSaving(true)
    try {
      const resp = await updateClientInfo({ clientId, comment, sortIndex })
      if (resp.status?.code !== RespCode.SUCCESS) {
        throw new Error(resp.status?.message ?? 'unknown error')
      }
      toast(t('client.edit_success'))
      setEditOpen(false)
      $clientTableRefetchTrigger.set(Math.random())
    } catch (e) {
      toast(t('client.edit_failed'), { description: e instanceof Error ? e.message : String(e) })
    } finally {
      setSaving(false)
    }
  }

  return { editOpen, setEditOpen, saving, handleSave }
}

// 名称列单元格：展示 comment（未设置时显示占位符），点击名称查看安装命令，铅笔按钮打开名称/序号编辑对话框
export const ClientName = ({ client }: { client: ClientTableSchema }) => {
  const { t } = useTranslation()
  const platformInfo = useStore($platformInfo)
  const frontendPreference = useStore($frontendPreference)
  const { editOpen, setEditOpen, saving, handleSave } = useClientInfoEdit(client.id)

  const displayName = client.comment ? client.comment : t('client.name_placeholder')

  return (
    <div className="flex items-center gap-1">
      <Popover>
        <PopoverTrigger asChild>
          <Button variant="link" className="px-0 max-w-full truncate" title={t('client.name_install_hint')}>
            {displayName}
          </Button>
        </PopoverTrigger>
        <PopoverContent className="w-80">
          {platformInfo ? (
          <div className="grid gap-4">
            <div className="space-y-2">
              <h4 className="font-medium leading-none">{t('client.install.title')}</h4>
              <p className="text-sm text-muted-foreground">{t('client.install.description')}</p>
            </div>
            <div className="grid gap-2">
              <div className="grid grid-cols-2 items-center gap-4 justify-items-center">
                <Label>{t('client.install.use_github_proxy_url')}</Label>
                <Checkbox onCheckedChange={(checked) => {
                  $frontendPreference.set({ ...frontendPreference, useServerGithubProxyUrl: checked === 'indeterminate' ? false : checked })
                }} defaultChecked={frontendPreference.useServerGithubProxyUrl} />
              </div>
              <div className="grid grid-cols-2 items-center gap-4 justify-items-center">
                <Label>{t('client.install.github_proxy_url')}</Label>
                <Input value={frontendPreference.githubProxyUrl} onChange={(e) =>
                  $frontendPreference.set({ ...frontendPreference, githubProxyUrl: e.target.value })} />
              </div>
              <div className="grid grid-cols-2 items-center gap-4">
                <Button
                  onClick={() =>
                    navigator.clipboard.writeText(
                      WindowsInstallCommand('client', client, {
                        ...platformInfo,
                        githubProxyUrl: frontendPreference.useServerGithubProxyUrl && frontendPreference.githubProxyUrl ? frontendPreference.githubProxyUrl : platformInfo?.githubProxyUrl || '',
                      }, frontendPreference.useServerGithubProxyUrl),
                    )
                  }
                  disabled={!platformInfo}
                  size="sm"
                  variant="outline"
                >
                  {t('client.install.windows')}
                </Button>
                <Input
                  readOnly
                  value={WindowsInstallCommand('client', client, {
                    ...platformInfo,
                    githubProxyUrl: frontendPreference.useServerGithubProxyUrl && frontendPreference.githubProxyUrl ? frontendPreference.githubProxyUrl : platformInfo?.githubProxyUrl || '',
                  }, frontendPreference.useServerGithubProxyUrl)}
                  className="flex-1"
                />
              </div>
              <div className="grid grid-cols-2 items-center gap-4">
                <Button
                  onClick={() =>
                    navigator.clipboard.writeText(LinuxInstallCommand('client', client, {
                      ...platformInfo,
                      githubProxyUrl: frontendPreference.useServerGithubProxyUrl && frontendPreference.githubProxyUrl ? frontendPreference.githubProxyUrl : platformInfo?.githubProxyUrl || '',
                    }, frontendPreference.useServerGithubProxyUrl))
                  }
                  disabled={!platformInfo}
                  size="sm"
                  variant="outline"
                >
                  {t('client.install.linux')}
                </Button>
                <Input
                  readOnly
                  value={LinuxInstallCommand('client', client, {
                    ...platformInfo,
                    githubProxyUrl: frontendPreference.useServerGithubProxyUrl && frontendPreference.githubProxyUrl ? frontendPreference.githubProxyUrl : platformInfo?.githubProxyUrl || '',
                  }, frontendPreference.useServerGithubProxyUrl)}
                  className="flex-1"
                />
              </div>
            </div>
          </div>
          ) : (
            <p className="text-sm text-muted-foreground">{t('common.loading')}</p>
          )}
        </PopoverContent>
      </Popover>
      <Button
        variant="ghost"
        size="icon"
        className="h-6 w-6 shrink-0"
        onClick={() => setEditOpen(true)}
        title={t('common.edit.title')}
      >
        <Pencil className="h-3.5 w-3.5" />
        <span className="sr-only">{t('common.edit.title')}</span>
      </Button>
      <NodeInfoEditDialog
        open={editOpen}
        onOpenChange={setEditOpen}
        defaultComment={client.comment ?? ''}
        defaultSortIndex={client.sortIndex ?? 0}
        saving={saving}
        onSave={handleSave}
      />
    </div>
  )
}

// 序号列单元格：展示排序序号（0 表示未设置），点击可打开同一个编辑对话框修改序号
export const ClientSort = ({ client }: { client: ClientTableSchema }) => {
  const { t } = useTranslation()
  const { editOpen, setEditOpen, saving, handleSave } = useClientInfoEdit(client.id)

  const sortIndex = client.sortIndex ?? 0
  return (
    <div className="flex items-center">
      <Button
        variant="ghost"
        className="h-7 px-2 font-mono"
        onClick={() => setEditOpen(true)}
        title={t('common.edit.title')}
      >
        {sortIndex > 0 ? sortIndex : '—'}
      </Button>
      <NodeInfoEditDialog
        open={editOpen}
        onOpenChange={setEditOpen}
        defaultComment={client.comment ?? ''}
        defaultSortIndex={sortIndex}
        saving={saving}
        onSave={handleSave}
      />
    </div>
  )
}

export const ClientActions: React.FC<ClientItemProps> = ({ client, table, rowIndex }) => {
  const { t } = useTranslation()
  const router = useRouter()
  const platformInfo = useStore($platformInfo)
  const frontendPreference = useStore($frontendPreference)
  const [upgradeDialogOpen, setUpgradeDialogOpen] = useState(false)
  const [notifyDialogOpen, setNotifyDialogOpen] = useState(false)

  const removeClient = useMutation({
    mutationFn: deleteClient,
    onSuccess: () => {
      toast(t('client.delete.success'))
      $clientTableRefetchTrigger.set(Math.random())
    },
    onError: (e) => {
      toast(t('client.delete.failed'), {
        description: e.message,
      })
      $clientTableRefetchTrigger.set(Math.random())
    },
  })

  const stopClient = useMutation({
    mutationFn: stopFrpc,
    onSuccess: () => {
      toast(t('client.operation.stop_success'))
      $clientTableRefetchTrigger.set(Math.random())
    },
    onError: (e) => {
      toast(t('client.operation.stop_failed'), {
        description: e.message,
      })
      $clientTableRefetchTrigger.set(Math.random())
    },
  })

  const startClient = useMutation({
    mutationFn: startFrpc,
    onSuccess: () => {
      toast(t('client.operation.start_success'))
      $clientTableRefetchTrigger.set(Math.random())
    },
    onError: (e) => {
      toast(t('client.operation.start_failed'), {
        description: e.message,
      })
      $clientTableRefetchTrigger.set(Math.random())
    },
  })

  // 与上/下一项交换排序位置（服务端跨分页处理），成功后刷新列表
  const swapClient = useMutation({
    mutationFn: (up: boolean) => swapClientSort({ clientId: client.id, up }),
    onSuccess: (resp) => {
      if (resp.status?.code !== RespCode.SUCCESS) {
        toast(t('client.sort_failed'), { description: resp.status?.message })
        return
      }
      toast(t('client.sort_success'))
      $clientTableRefetchTrigger.set(Math.random())
    },
    onError: (e) => {
      toast(t('client.sort_failed'), {
        description: e.message,
      })
      $clientTableRefetchTrigger.set(Math.random())
    },
  })

  // 基于服务端分页计算全局位置：首行禁用上移、尾行禁用下移；搜索/筛选时全部禁用
  const { pagination } = table.getState()
  const meta = table.options.meta
  const globalIndex = pagination.pageIndex * pagination.pageSize + rowIndex
  const sortEnabled = meta?.sortEnabled !== false
  const total = meta?.total
  const upDisabled = !sortEnabled || swapClient.isPending || globalIndex <= 0
  const downDisabled = !sortEnabled || swapClient.isPending || total === undefined || globalIndex >= total - 1

  const createAndDownloadFile = (fileName: string, content: string) => {
    const aTag = document.createElement('a')
    const blob = new Blob([content])
    aTag.download = fileName
    aTag.href = URL.createObjectURL(blob)
    aTag.click()
    URL.revokeObjectURL(aTag.href)
  }

  const githubProxyUrl =
    frontendPreference.useServerGithubProxyUrl && frontendPreference.githubProxyUrl
      ? frontendPreference.githubProxyUrl
      : platformInfo?.githubProxyUrl || ''

  return (
    <>
      <ClientUpgradeDialog
        open={upgradeDialogOpen}
        onOpenChange={setUpgradeDialogOpen}
        clientId={client.id}
        defaultGithubProxy={githubProxyUrl || undefined}
        defaultUseGithubProxy={true}
        defaultServiceName="frpp"
        onDispatched={() => {
          $clientTableRefetchTrigger.set(Math.random())
        }}
      />

      {/* 通知提醒设置：三段式（不提醒/全局配置/独立配置） */}
      <ResourceNotifyDialog
        open={notifyDialogOpen}
        onOpenChange={setNotifyDialogOpen}
        resourceType="client"
        resourceId={client.id}
        resourceLabel={client.comment || client.id}
      />

      <div className="flex items-center justify-end gap-1">
        <Button
          variant="ghost"
          size="icon"
          className="h-8 w-8"
          disabled={upDisabled}
          onClick={() => swapClient.mutate(true)}
          title={t('client.move_up')}
        >
          <ArrowUp className="h-4 w-4" />
          <span className="sr-only">{t('client.move_up')}</span>
        </Button>
        <Button
          variant="ghost"
          size="icon"
          className="h-8 w-8"
          disabled={downDisabled}
          onClick={() => swapClient.mutate(false)}
          title={t('client.move_down')}
        >
          <ArrowDown className="h-4 w-4" />
          <span className="sr-only">{t('client.move_down')}</span>
        </Button>

      <Dialog>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" className="h-8 w-8 p-0">
            <span className="sr-only">{t('client.actions_menu.open_menu')}</span>
            <MoreHorizontal className="h-4 w-4" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuLabel>{t('client.actions_menu.title')}</DropdownMenuLabel>
          <DropdownMenuItem
            onClick={() => {
              try {
                if (platformInfo) {
                  navigator.clipboard.writeText(ExecCommandStr('client', client, platformInfo))
                  toast(t('client.actions_menu.copy_success'))
                } else {
                  toast(t('client.actions_menu.copy_failed'))
                }
              } catch (error) {
                toast(t('client.actions_menu.copy_failed'), {
                  description: JSON.stringify(error),
                })
              }
            }}
          >
            {t('client.actions_menu.copy_start_command')}
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={() => {
              try {
                if (platformInfo) {
                  navigator.clipboard.writeText(LinuxInstallCommand('client', client, {
                    ...platformInfo,
                    githubProxyUrl: frontendPreference.useServerGithubProxyUrl && frontendPreference.githubProxyUrl ? frontendPreference.githubProxyUrl : platformInfo.githubProxyUrl,
                  }, frontendPreference.useServerGithubProxyUrl))
                  toast(t('client.actions_menu.copy_success'))
                } else {
                  toast(t('client.actions_menu.copy_failed'))
                }
              } catch (error) {
                toast(t('client.actions_menu.copy_failed'), {
                  description: JSON.stringify(error),
                })
              }
            }}
          >
            {t('client.actions_menu.copy_install_command')}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem
            onClick={() => {
              router.push({ pathname: '/clientedit', query: { clientID: client.id } })
            }}
          >
            {t('client.actions_menu.edit_config')}
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={() => {
              try {
                if (platformInfo) {
                  createAndDownloadFile('.env', ClientEnvFile(client, platformInfo))
                }
              } catch (error) {
                toast(t('client.actions_menu.download_failed'), {
                  description: JSON.stringify(error),
                })
              }
            }}
          >
            {t('client.actions_menu.download_config')}
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={() => {
              router.push({
                pathname: '/streamlog',
                query: { clientID: client.id, clientType: ClientType.FRPC.toString() },
              })
            }}
          >
            {t('client.actions_menu.realtime_log')}
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={() => {
              router.push({
                pathname: '/console',
                query: { clientID: client.id, clientType: ClientType.FRPC.toString() },
              })
            }}
          >
            {t('client.actions_menu.remote_terminal')}
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={(e) => {
              e.preventDefault()
              e.stopPropagation()
              setNotifyDialogOpen(true)
            }}
          >
            {t('settings.notifyResource.menuTitle')}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem
            onClick={(e) => {
              e.preventDefault()
              e.stopPropagation()
              setUpgradeDialogOpen(true)
            }}
          >
            {t('client.actions_menu.upgrade')}
          </DropdownMenuItem>
          {!client.stopped && (
            <DropdownMenuItem className="text-destructive" onClick={() => stopClient.mutate({ clientId: client.id })}>
              {t('client.actions_menu.pause')}
            </DropdownMenuItem>
          )}
          {client.stopped && (
            <DropdownMenuItem onClick={() => startClient.mutate({ clientId: client.id })}>
              {t('client.actions_menu.resume')}
            </DropdownMenuItem>
          )}
          <DialogTrigger asChild>
            <DropdownMenuItem className="text-destructive">{t('client.actions_menu.delete')}</DropdownMenuItem>
          </DialogTrigger>
        </DropdownMenuContent>
      </DropdownMenu>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('client.delete.title')}</DialogTitle>
          <DialogDescription>
            <p className="text-destructive">{t('client.delete.description')}</p>
            <p className="text-gray-500 border-l-4 border-gray-500 pl-4 py-2">{t('client.delete.warning')}</p>
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <DialogClose asChild>
            <Button type="submit" onClick={() => removeClient.mutate({ clientId: client.id })}>
              {t('client.delete.confirm')}
            </Button>
          </DialogClose>
        </DialogFooter>
      </DialogContent>
      </Dialog>
      </div>
    </>
  )
}
