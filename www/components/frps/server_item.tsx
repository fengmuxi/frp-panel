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
import React from 'react'
import { ClientEnvFile, ExecCommandStr, LinuxInstallCommand, WindowsInstallCommand } from '@/lib/consts'
import { useMutation, useQuery } from '@tanstack/react-query'
import { deleteServer, swapServerSort, updateServerInfo } from '@/api/server'
import { useRouter } from 'next/router'
import { useStore } from '@nanostores/react'
import { $frontendPreference, $platformInfo } from '@/store/user'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { getClientsStatus } from '@/api/platform'
import { ClientType, RespCode } from '@/lib/pb/common'
import { ClientStatus, ClientStatus_Status } from '@/lib/pb/api_master'
import { Badge } from '../ui/badge'
import { ResourceNotifyDialog } from '@/components/platform/resource_notify_dialog'
import { ClientDetail } from '../base/client_detail'
import { useTranslation } from 'react-i18next'
import { Input } from '@/components/ui/input'
import { toast } from 'sonner'
import { $serverTableRefetchTrigger } from '@/store/refetch-trigger'
import { Checkbox } from '../ui/checkbox'
import { Label } from '../ui/label'
import { NodeInfoEditDialog } from '../base/node_info_edit_dialog'

export type ServerTableSchema = {
  id: string
  status: 'invalid' | 'valid'
  secret: string
  stopped: boolean
  info?: string
  ip: string
  config?: string
  frpsUrls: string[]
  comment?: string
  sortIndex?: number
}

export interface ServerTableMeta extends TableMeta<ServerTableSchema> {
  /** 当前列表总条数（服务端分页），用于计算全局首/尾行禁用上下移按钮 */
  total?: number
  /** 是否允许调整排序（搜索/筛选时禁用） */
  sortEnabled?: boolean
}

export const columns: ColumnDef<ServerTableSchema>[] = [
  {
    accessorKey: 'comment',
    header: function Header() {
      const { t } = useTranslation()
      return t('server.name')
    },
    cell: ({ row }) => {
      return <ServerName server={row.original} />
    },
  },
  {
    accessorKey: 'sortIndex',
    header: function Header() {
      const { t } = useTranslation()
      return t('server.sort')
    },
    cell: ({ row }) => {
      return <ServerSort server={row.original} />
    },
  },
  {
    accessorKey: 'status',
    header: function Header() {
      const { t } = useTranslation()
      return t('server.status')
    },
    cell: ({ row }) => {
      function Cell({ server }: { server: ServerTableSchema }) {
        const { t } = useTranslation()
        return (
          <div className={`font-medium ${server.status === 'valid' ? 'text-green-500' : 'text-red-500'} min-w-12`}>
            {server.status === 'valid' ? t('server.status_configured') : t('server.status_unconfigured')}
          </div>
        )
      }
      return <Cell server={row.original} />
    },
  },
  {
    accessorKey: 'info',
    header: function Header() {
      const { t } = useTranslation()
      return t('server.info')
    },
    cell: ({ row }) => {
      const server = row.original
      return <ServerInfo server={server} />
    },
  },
  {
    accessorKey: 'ip',
    header: function Header() {
      const { t } = useTranslation()
      return t('server.ip')
    },
    cell: ({ row }) => {
      return row.original.ip
    },
  },
  {
    accessorKey: 'secret',
    header: function Header() {
      const { t } = useTranslation()
      return t('server.secret')
    },
    cell: ({ row }) => {
      const server = row.original
      return <ServerSecret server={server} />
    },
  },
  {
    id: 'action',
    cell: ({ row, table }) => {
      const server = row.original
      return (
        <ServerActions
          server={server}
          rowIndex={row.index}
          table={table as Table<ServerTableSchema> & { options: { meta: ServerTableMeta } }}
        />
      )
    },
  },
]

export const ServerInfo = ({ server }: { server: ServerTableSchema }) => {
  const { t } = useTranslation()
  const { data: clientsStatus } = useQuery({
    queryKey: ['clientsStatus', server.id],
    queryFn: async () => {
      return await getClientsStatus({
        clientIds: [server.id],
        clientType: ClientType.FRPS,
      })
    },
  })

  const trans = (info: ClientStatus | undefined) => {
    let statusText: 'server.status_online' | 'server.status_offline' |
      'server.status_error' | 'server.status_pause' |
      'server.status_unknown' = 'server.status_unknown'
    if (info === undefined) {
      return statusText
    }
    if (info.status === ClientStatus_Status.ONLINE) {
      statusText = 'server.status_online'
      if (server.stopped) {
        statusText = 'server.status_pause'
      }
    } else if (info.status === ClientStatus_Status.OFFLINE) {
      statusText = 'server.status_offline'
    } else if (info.status === ClientStatus_Status.ERROR) {
      statusText = 'server.status_error'
    }
    return statusText
  }

  const infoColor =
    clientsStatus?.clients[server.id]?.status === ClientStatus_Status.ONLINE ? (
      server.stopped ? 'text-yellow-500' : 'text-green-500') : 'text-red-500'

  return (
    <div className="flex items-center gap-2 flex-row">
      <Badge variant={"secondary"} className={`p-2 border rounded font-mono w-fit ${infoColor} text-nowrap rounded-full h-6`}>
        {`${clientsStatus?.clients[server.id]?.ping}ms,${t(trans(clientsStatus?.clients[server.id]))}`}
      </Badge>
      {clientsStatus?.clients[server.id]?.version &&
        <ClientDetail clientStatus={clientsStatus?.clients[server.id]} />
      }
    </div>
  )
}

export const ServerSecret = ({ server }: { server: ServerTableSchema }) => {
  const { t } = useTranslation()
  const platformInfo = useStore($platformInfo)

  if (!platformInfo) {
    return (
      <Button variant="link" className="px-0">
        {server.secret}
      </Button>
    )
  }

  return (
    <Popover>
      <PopoverTrigger asChild>
        <div className="group relative cursor-pointer inline-block font-mono text-nowrap">
          <span className="opacity-0 group-hover:opacity-100 transition-opacity duration-200">
            {server.secret}
          </span>
          <span className="absolute inset-0 opacity-100 group-hover:opacity-0 transition-opacity duration-200">
            {'*'.repeat(server.secret.length)}
          </span>
        </div>
      </PopoverTrigger>
      <PopoverContent className="w-[32rem] max-w-[95vw]">
        <div className="grid gap-4">
          <div className="space-y-2">
            <h4 className="font-medium leading-none">{t('server.start.title')}</h4>
            <p className="text-sm text-muted-foreground">
              {t('server.start.description')} (<a className='text-blue-500' href='https://github.com/VaalaCat/frp-panel/releases' target="_blank" rel="noopener noreferrer">{t('common.download')}</a>)
            </p>
          </div>
          <div className="grid gap-2">
            <pre className="bg-muted p-3 rounded-md font-mono text-sm overflow-x-auto whitespace-pre-wrap break-all">
              {ExecCommandStr('server', server, platformInfo)}
            </pre>
            <Button
              size="sm"
              variant="outline"
              className="w-full"
              onClick={() => navigator.clipboard.writeText(ExecCommandStr('server', server, platformInfo))}
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

export interface ServerItemProps {
  server: ServerTableSchema
  table: Table<ServerTableSchema> & { options: { meta: ServerTableMeta } }
  rowIndex: number
}

// 名称/序号编辑的公共状态与保存逻辑（名称列与序号列共用）
const useServerInfoEdit = (serverId: string) => {
  const { t } = useTranslation()
  const [editOpen, setEditOpen] = React.useState(false)
  const [saving, setSaving] = React.useState(false)

  const handleSave = async (comment: string, sortIndex: number) => {
    setSaving(true)
    try {
      const resp = await updateServerInfo({ serverId, comment, sortIndex })
      if (resp.status?.code !== RespCode.SUCCESS) {
        throw new Error(resp.status?.message ?? 'unknown error')
      }
      toast(t('server.edit_success'))
      setEditOpen(false)
      $serverTableRefetchTrigger.set(Math.random())
    } catch (e) {
      toast(t('server.edit_failed'), { description: e instanceof Error ? e.message : String(e) })
    } finally {
      setSaving(false)
    }
  }

  return { editOpen, setEditOpen, saving, handleSave }
}

// 名称列单元格：展示 comment（未设置时显示占位符），点击名称查看安装命令，铅笔按钮打开名称/序号编辑对话框
export const ServerName = ({ server }: { server: ServerTableSchema }) => {
  const { t } = useTranslation()
  const platformInfo = useStore($platformInfo)
  const frontendPreference = useStore($frontendPreference)
  const { editOpen, setEditOpen, saving, handleSave } = useServerInfoEdit(server.id)

  const displayName = server.comment ? server.comment : t('server.name_placeholder')

  return (
    <div className="flex items-center gap-1">
      <Popover>
        <PopoverTrigger asChild>
          <Button variant="link" className="px-0 max-w-full truncate" title={t('server.name_install_hint')}>
            {displayName}
          </Button>
        </PopoverTrigger>
        <PopoverContent className="w-80">
          {platformInfo ? (
          <div className="grid gap-4">
            <div className="space-y-2">
              <h4 className="font-medium leading-none">{t('server.install.title')}</h4>
              <p className="text-sm text-muted-foreground">{t('server.install.description')}</p>
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
                    navigator.clipboard.writeText(WindowsInstallCommand('server', server, {
                      ...platformInfo,
                      githubProxyUrl: frontendPreference.useServerGithubProxyUrl && frontendPreference.githubProxyUrl ? frontendPreference.githubProxyUrl : platformInfo?.githubProxyUrl || '',
                    }, frontendPreference.useServerGithubProxyUrl))}
                  disabled={!platformInfo}
                  size="sm"
                  variant="outline"
                >
                  {t('server.install.windows')}
                </Button>
                <Input
                  readOnly
                  value={WindowsInstallCommand('server', server, {
                    ...platformInfo,
                    githubProxyUrl: frontendPreference.useServerGithubProxyUrl && frontendPreference.githubProxyUrl ? frontendPreference.githubProxyUrl : platformInfo?.githubProxyUrl || '',
                  }, frontendPreference.useServerGithubProxyUrl)}
                  className="flex-1"
                />
              </div>
              <div className="grid grid-cols-2 items-center gap-4">
                <Button
                  onClick={() =>
                    navigator.clipboard.writeText(LinuxInstallCommand('server', server, {
                      ...platformInfo,
                      githubProxyUrl: frontendPreference.useServerGithubProxyUrl && frontendPreference.githubProxyUrl ? frontendPreference.githubProxyUrl : platformInfo?.githubProxyUrl || '',
                    }, frontendPreference.useServerGithubProxyUrl))}
                  disabled={!platformInfo}
                  size="sm"
                  variant="outline"
                >
                  {t('server.install.linux')}
                </Button>
                <Input
                  readOnly
                  value={LinuxInstallCommand('server', server, {
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
        defaultComment={server.comment ?? ''}
        defaultSortIndex={server.sortIndex ?? 0}
        saving={saving}
        onSave={handleSave}
      />
    </div>
  )
}

// 序号列单元格：展示排序序号（0 表示未设置），点击可打开同一个编辑对话框修改序号
export const ServerSort = ({ server }: { server: ServerTableSchema }) => {
  const { t } = useTranslation()
  const { editOpen, setEditOpen, saving, handleSave } = useServerInfoEdit(server.id)

  const sortIndex = server.sortIndex ?? 0
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
        defaultComment={server.comment ?? ''}
        defaultSortIndex={sortIndex}
        saving={saving}
        onSave={handleSave}
      />
    </div>
  )
}

export const ServerActions: React.FC<ServerItemProps> = ({ server, table, rowIndex }) => {
  const { t } = useTranslation()
  const router = useRouter()
  const platformInfo = useStore($platformInfo)
  const [notifyDialogOpen, setNotifyDialogOpen] = React.useState(false)

  const removeServer = useMutation({
    mutationFn: deleteServer,
    onSuccess: () => {
      toast(t('server.delete.success'))
      $serverTableRefetchTrigger.set(Math.random())
    },
    onError: (e) => {
      toast(t('server.delete.failed'), {
        description: e.message,
      })
      $serverTableRefetchTrigger.set(Math.random())
    },
  })

  // 与上/下一项交换排序位置（服务端跨分页处理），成功后刷新列表
  const swapServer = useMutation({
    mutationFn: (up: boolean) => swapServerSort({ serverId: server.id, up }),
    onSuccess: (resp) => {
      if (resp.status?.code !== RespCode.SUCCESS) {
        toast(t('server.sort_failed'), { description: resp.status?.message })
        return
      }
      toast(t('server.sort_success'))
      $serverTableRefetchTrigger.set(Math.random())
    },
    onError: (e) => {
      toast(t('server.sort_failed'), {
        description: e.message,
      })
      $serverTableRefetchTrigger.set(Math.random())
    },
  })

  // 基于服务端分页计算全局位置：首行禁用上移、尾行禁用下移；搜索/筛选时全部禁用
  const { pagination } = table.getState()
  const meta = table.options.meta
  const globalIndex = pagination.pageIndex * pagination.pageSize + rowIndex
  const sortEnabled = meta?.sortEnabled !== false
  const total = meta?.total
  const upDisabled = !sortEnabled || swapServer.isPending || globalIndex <= 0
  const downDisabled = !sortEnabled || swapServer.isPending || total === undefined || globalIndex >= total - 1

  const createAndDownloadFile = (fileName: string, content: string) => {
    const aTag = document.createElement('a')
    const blob = new Blob([content])
    aTag.download = fileName
    aTag.href = URL.createObjectURL(blob)
    aTag.click()
    URL.revokeObjectURL(aTag.href)
  }

  return (
    <div className="flex items-center justify-end gap-1">
      <Button
        variant="ghost"
        size="icon"
        className="h-8 w-8"
        disabled={upDisabled}
        onClick={() => swapServer.mutate(true)}
        title={t('server.move_up')}
      >
        <ArrowUp className="h-4 w-4" />
        <span className="sr-only">{t('server.move_up')}</span>
      </Button>
      <Button
        variant="ghost"
        size="icon"
        className="h-8 w-8"
        disabled={downDisabled}
        onClick={() => swapServer.mutate(false)}
        title={t('server.move_down')}
      >
        <ArrowDown className="h-4 w-4" />
        <span className="sr-only">{t('server.move_down')}</span>
      </Button>

      <Dialog>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" className="h-8 w-8 p-0">
            <span className="sr-only">{t('server.actions_menu.open_menu')}</span>
            <MoreHorizontal className="h-4 w-4" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuLabel>{t('server.actions_menu.title')}</DropdownMenuLabel>
          <DropdownMenuItem
            onClick={() => {
              try {
                if (platformInfo) {
                  navigator.clipboard.writeText(ExecCommandStr('server', server, platformInfo))
                  toast(t('server.actions_menu.copy_success'))
                } else {
                  toast(t('server.actions_menu.copy_failed'))
                }
              } catch (error) {
                toast(t('server.actions_menu.copy_failed'))
              }
            }}
          >
            {t('server.actions_menu.copy_command')}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem
            onClick={() => {
              router.push({ pathname: '/serveredit', query: { serverID: server.id } })
            }}
          >
            {t('server.actions_menu.edit_config')}
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={() => {
              try {
                if (platformInfo) {
                  createAndDownloadFile('.env', ClientEnvFile(server, platformInfo))
                }
              } catch (error) {
                toast(t('server.actions_menu.download_failed'), {
                  description: JSON.stringify(error),
                })
              }
            }}
          >
            {t('server.actions_menu.download_config')}
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={() => {
              router.push({ pathname: '/streamlog', query: { serverID: server.id, clientType: ClientType.FRPS.toString() } })
            }}
          >
            {t('server.actions_menu.realtime_log')}
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={() => {
              router.push({ pathname: '/console', query: { serverID: server.id, clientType: ClientType.FRPS.toString() } })
            }}
          >
            {t('server.actions_menu.remote_terminal')}
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
          <DialogTrigger asChild>
            <DropdownMenuItem className="text-destructive">{t('server.actions_menu.delete')}</DropdownMenuItem>
          </DialogTrigger>
        </DropdownMenuContent>
      </DropdownMenu>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('server.delete.title')}</DialogTitle>
          <DialogDescription>
            <p className="text-destructive">{t('server.delete.description')}</p>
            <p className="text-gray-500 border-l-4 border-gray-500 pl-4 py-2">
              {t('server.delete.warning')}
            </p>
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <DialogClose asChild>
            <Button type="submit" onClick={() => removeServer.mutate({ serverId: server.id })}>
              {t('server.delete.confirm')}
            </Button>
          </DialogClose>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    {/* 通知提醒设置：三段式（不提醒/全局配置/独立配置） */}
    <ResourceNotifyDialog
      open={notifyDialogOpen}
      onOpenChange={setNotifyDialogOpen}
      resourceType="server"
      resourceId={server.id}
      resourceLabel={server.comment || server.id}
    />
    </div>
  )
}
