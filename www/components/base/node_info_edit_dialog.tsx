import React from 'react'
import { useTranslation } from 'react-i18next'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

export interface NodeInfoEditDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** 当前名称（comment 字段），作为初始值与“未修改”判断基准 */
  defaultComment: string
  /** 当前排序序号，作为初始值与“未修改”判断基准 */
  defaultSortIndex: number
  /** 保存进行中，禁用保存按钮 */
  saving?: boolean
  /** 保存回调，序号已解析为非负整数（非法输入按 0 处理） */
  onSave: (comment: string, sortIndex: number) => void
}

// 名称（备注）+ 排序序号 编辑对话框，客户端/服务端列表共用。
// 打开时以传入值重置表单；内容无变化时保存按钮保持禁用。
export const NodeInfoEditDialog: React.FC<NodeInfoEditDialogProps> = ({
  open,
  onOpenChange,
  defaultComment,
  defaultSortIndex,
  saving,
  onSave,
}) => {
  const { t } = useTranslation()
  const [comment, setComment] = React.useState(defaultComment)
  const [sortIndex, setSortIndex] = React.useState(String(defaultSortIndex))

  React.useEffect(() => {
    if (open) {
      setComment(defaultComment)
      setSortIndex(String(defaultSortIndex))
    }
  }, [open, defaultComment, defaultSortIndex])

  const parsedSortIndex = Number.parseInt(sortIndex, 10)
  const normalizedSortIndex = Number.isNaN(parsedSortIndex) || parsedSortIndex < 0 ? 0 : parsedSortIndex
  const unchanged = comment === defaultComment && normalizedSortIndex === defaultSortIndex

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t('common.edit.title')}</DialogTitle>
          <DialogDescription>{t('common.edit.description')}</DialogDescription>
        </DialogHeader>
        <div className="grid gap-4">
          <div className="grid gap-2">
            <Label htmlFor="node-edit-comment">{t('common.edit.comment')}</Label>
            <Input
              id="node-edit-comment"
              value={comment}
              placeholder={t('common.edit.comment_placeholder')}
              onChange={(e) => setComment(e.target.value)}
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="node-edit-sort">{t('common.edit.sort_index')}</Label>
            <Input
              id="node-edit-sort"
              type="number"
              min={0}
              value={sortIndex}
              onChange={(e) => setSortIndex(e.target.value)}
            />
            <p className="text-sm text-muted-foreground">{t('common.edit.sort_index_hint')}</p>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button disabled={unchanged || saving} onClick={() => onSave(comment, normalizedSortIndex)}>
            {saving ? t('common.saving') : t('common.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
