'use client'

import { useQuery } from '@tanstack/react-query'
import { getClient } from '@/api/client'
import { getServer } from '@/api/server'
import { ClientType } from '@/lib/pb/common'

/**
 * 根据节点 ID 与类型解析展示名称（comment），用于界面上把 ID 显示替换为名称。
 * 未查到或无名称时返回 undefined，由调用方决定兜底显示。
 */
export const useAgentName = (id: string | undefined, clientType: ClientType) => {
  const { data } = useQuery({
    queryKey: ['agentName', id, clientType],
    queryFn: async () => {
      if (!id) return undefined
      if (clientType === ClientType.FRPS) {
        const resp = await getServer({ serverId: id })
        return resp.server?.comment || undefined
      }
      const resp = await getClient({ clientId: id })
      return resp.client?.comment || undefined
    },
    enabled: !!id,
    staleTime: 30_000,
  })
  return data
}
