"use client"

import { useEffect, useState } from 'react'
import { ChevronRight, type LucideIcon } from "lucide-react"
import { useRouter } from 'next/navigation'
import { useRouter as usePagesRouter } from 'next/router'
import { PAGE_PARENTS } from "@/lib/routes"

import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import {
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
} from "@/components/ui/sidebar"

export function NavMain({
  items,
}: {
  items: {
    title: string
    url: string
    icon?: LucideIcon
    isActive?: boolean
    items?: {
      title: string
      url: string
    }[]
  }[]
}) {
  const router = useRouter()
  // Pages Router 的 pathname：随客户端路由更新，用于判断激活菜单
  const { pathname } = usePagesRouter()

  // 当前页面匹配菜单 url，或当前是该菜单下的二级页面（如 /wg/network-detail → 组网/网络）时均算命中
  const urlSelected = (url: string) => pathname === url || PAGE_PARENTS[pathname] === url
  // 某个带子菜单的组是否包含当前路由
  const groupActive = (item: (typeof items)[number]) =>
    urlSelected(item.url) || !!item.items?.some((sub) => urlSelected(sub.url))

  // 受控展开状态：初始展开包含当前路由的组，路由变化时自动展开命中组；
  // 用户手动收起/展开由 onOpenChange 记录，未命中的组保持用户当前状态
  const [openMap, setOpenMap] = useState<Record<string, boolean>>(() =>
    Object.fromEntries(items.map((item) => [item.title, groupActive(item)])),
  )

  useEffect(() => {
    setOpenMap((prev) => {
      const next = { ...prev }
      for (const item of items) {
        if (item.items && groupActive(item)) {
          next[item.title] = true
        }
      }
      return next
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pathname])

  return (
    <SidebarMenu>
      {items.map((item) => (
        <Collapsible
          key={item.title}
          asChild
          open={!!openMap[item.title]}
          onOpenChange={(open) => setOpenMap((prev) => ({ ...prev, [item.title]: open }))}
          className="group/collapsible"
        >
          <>
            {!item.items && <SidebarMenuItem>
              <SidebarMenuButton isActive={urlSelected(item.url)} onClick={() => router.push(item.url)} tooltip={item.title}>
                {item.icon && <item.icon />}
                <span>{item.title}</span>
              </SidebarMenuButton>
            </SidebarMenuItem>
            }
            {item.items && <SidebarMenuItem>
              <CollapsibleTrigger asChild>
                <SidebarMenuButton tooltip={item.title}>
                  {item.icon && <item.icon />}
                  <span>{item.title}</span>
                  <ChevronRight className="ml-auto transition-transform duration-200 group-data-[state=open]/collapsible:rotate-90" />
                </SidebarMenuButton>
              </CollapsibleTrigger>
              <CollapsibleContent>
                <SidebarMenuSub>
                  {item.items?.map((subItem) => (
                    <SidebarMenuSubItem key={subItem.title}>
                      {/* 客户端路由跳转，避免原生 <a href> 触发整页刷新导致侧边栏状态重置 */}
                      <SidebarMenuSubButton asChild isActive={urlSelected(subItem.url)}>
                        <a
                          href={subItem.url}
                          onClick={(e) => {
                            e.preventDefault()
                            router.push(subItem.url)
                          }}
                        >
                          <span>{subItem.title}</span>
                        </a>
                      </SidebarMenuSubButton>
                    </SidebarMenuSubItem>
                  ))}
                </SidebarMenuSub>
              </CollapsibleContent>
            </SidebarMenuItem>}
          </>
        </Collapsible>
      ))}
    </SidebarMenu>
  )
}
