"use client"

import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { initClient } from '@/api/client'
import { Label } from '@/components/ui/label'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { RespCode } from '@/lib/pb/common'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

export const CreateClientDialog = ({refetchTrigger}: {refetchTrigger?: (randStr: string) => void}) => {
  const { t } = useTranslation()
  const [clientName, setClientName] = useState<string | undefined>()
  const [dialogOpen, setDialogOpen] = useState(false)
  const newClient = useMutation({
    mutationFn: initClient,
  })

  const handleNewClient = async () => {
    toast(t('client.create.submitting'))
    try {
      // ID 由后端自动生成，这里只提交名称（必填，不允许重复）
      let resp = await newClient.mutateAsync({ comment: clientName })
      if (resp.status?.code !== RespCode.SUCCESS) {
        toast(t('client.create.error'),{
          description: resp.status?.message
        })
        return
      }
      toast(t('client.create.success'))
      refetchTrigger && refetchTrigger(JSON.stringify(Math.random()))
      // 创建成功后关闭弹窗并清空输入
      setDialogOpen(false)
      setClientName(undefined)
    } catch (error) {
      toast(t('client.create.error'), {
        description: JSON.stringify(error)
      })
    }
  }

  return (
    <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
      <DialogTrigger asChild>
        <Button variant="outline">
          {t('client.create.button')}
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('client.create.title')}</DialogTitle>
          <DialogDescription>{t('client.create.description')}</DialogDescription>
        </DialogHeader>

        <Label>{t('client.create.name')}</Label>
        <Input
          value={clientName}
          placeholder={t('client.create.name_placeholder')}
          onChange={(e) => setClientName(e.target.value)}
        />
        <DialogFooter>
          <Button onClick={handleNewClient}
          disabled={!clientName || clientName.trim().length === 0}
          className='w-full'>{t('client.create.submit')}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
