'use client'

import { useStore } from '@nanostores/react'
import { useQuery } from '@tanstack/react-query'
import { useRouter } from 'next/router'
import { useEffect } from 'react'
import { getUserInfo } from '@/api/user'
import { handleSessionExpired } from '@/lib/session'
import { $token } from '@/store/user'

// 全局登录态守卫：挂在 Providers 里覆盖全部页面（含 terminal 这类不走
// RootLayout/Header 的页面）。职责：
// 1) 本地判断——localStorage 里没有 token（手动退出/从未登录）直接跳登录；
// 2) 远端判断——有 token 也周期性校验：JWT 过期、jti 被吊销（其他设备改密/
//    管理员封禁）、用户被删都会让 getUserInfo 返回 401，由 api/http.ts 的
//    响应拦截器统一走 handleSessionExpired 清态并跳登录。
// 注意：不要在 /login、/register 上再跳（避免与登录流程打架）。
export function SessionGuard() {
  const router = useRouter()
  const pathname = router.pathname
  const token = useStore($token)

  const onAuthPage = pathname === '/login' || pathname === '/register'

  useQuery({
    queryKey: ['sessionGuard', pathname],
    queryFn: getUserInfo,
    retry: false,
    enabled: !!token && !onAuthPage,
    // 会话心跳：最长 60s 内发现失效（JWT 过期/jti 被吊销/用户被删）
    refetchInterval: 60_000,
  })

  useEffect(() => {
    if (onAuthPage) {
      return
    }
    if (!token) {
      handleSessionExpired('no token')
    }
  }, [token, onAuthPage])

  return null
}
