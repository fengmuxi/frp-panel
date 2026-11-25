// 会话失效统一出口（2026-11-24）：
// JWT 过期 / jti 被吊销 / 用户被删除后，此前前端只在"整页刷新且 sidebar 的
// 一次性 getUserInfo 失败"时才跳登录页；SPA 长时间挂着不动就永远停在旧页面
// （$token 持久化在 localStorage、从不清除，$statusOnline 也只算一次）。
// 现在所有 API 的 401（见 api/http.ts）与登录态心跳（见 components/header.tsx）
// 都走这里：清本地态 → 跳 /login 并带 redirect 回跳参数。
import { $statusOnline, $token, $userInfo } from '@/store/user'

let redirecting = false

/** 清空本地会话状态（与 NavUser 手动退出口径一致） */
export function clearAuthState() {
  $token.set(undefined)
  $userInfo.set(undefined)
  $statusOnline.set(false)
}

/** 会话失效：清状态 + 跳登录页（已在登录/注册页则只清状态，避免刷新循环） */
export function handleSessionExpired(reason?: string) {
  clearAuthState()
  if (typeof window === 'undefined' || redirecting) {
    return
  }
  const { pathname, search } = window.location
  if (pathname === '/login' || pathname === '/register') {
    return
  }
  redirecting = true
  if (reason) {
    console.warn('[session] session expired:', reason)
  }
  window.location.replace(`/login?redirect=${encodeURIComponent(pathname + search)}`)
}
