import { Button } from './ui/button'
import { useStore } from '@nanostores/react'
import { useRouter } from 'next/router'
import { $platformInfo, $userInfo, $token } from '@/store/user'
import { useQuery } from '@tanstack/react-query'
import { useEffect } from 'react'
import { getPlatformInfo } from '@/api/platform'
import { useTranslation } from 'react-i18next'
import { LanguageSwitcher } from './language-switcher'
import { ArrowLeft } from 'lucide-react'
import { NAV_TITLE_KEYS, PAGE_PARENTS } from '@/lib/routes'

export const Header = ({ title }: { title?: string }) => {
  const router = useRouter()
  const token = useStore($token)
  const currentPath = router.pathname
  const { t } = useTranslation()

  // 二级页面：返回所属一级页面
  const parentPath = PAGE_PARENTS[currentPath]
  const parentTitleKey = parentPath ? NAV_TITLE_KEYS[parentPath] : undefined

  useEffect(() => {
    // 本地守卫：localStorage 无 token（手动退出/从未登录）直接跳登录。
    // "有 token 但服务端已失效"（JWT 过期 / jti 被吊销 / 用户被删）不再依赖
    // 一次性的 getUserInfo 判定（此前 isOnline 一旦为 true 永不复核，SPA
    // 挂着不动永不跳转），统一由 SessionGuard 的 60s 心跳 + api/http.ts 的
    // 401 拦截器清态跳转。
    const isAuthPage = ['/login', '/register'].includes(currentPath)
    if (!token && !isAuthPage) {
      // replace 而非 push：避免"登录后按回退又回到失效页"的死循环
      router.replace(`/login?redirect=${encodeURIComponent(router.asPath)}`)
    }
  }, [token, router, currentPath])

  return (
    <div className="flex w-full justify-between items-center gap-2">
      <div className="flex items-center gap-2 min-w-0">
        {parentPath && (
          <Button
            variant="ghost"
            size="sm"
            className="gap-1"
            onClick={() => router.push(parentPath)}
          >
            <ArrowLeft className="h-4 w-4" />
            {parentTitleKey ? t(parentTitleKey) : t('common.back')}
          </Button>
        )}
        {title && <p className="font-bold truncate">{title}</p>}
        {!title && !parentPath && <p></p>}
      </div>
      <LanguageSwitcher />
    </div>
  )
}

export const RegisterAndLogin = () => {
  const router = useRouter()
  const userInfo = useStore($userInfo)
  const { t } = useTranslation()

  const platformInfo = useQuery({
    queryKey: ['platformInfo'],
    queryFn: getPlatformInfo,
  })

  useEffect(() => {
    $platformInfo.set(platformInfo.data)
  }, [platformInfo])

  // 用户信息查询统一由 SessionGuard 的 ['sessionGuard'] 心跳负责：
  // 此前这里另发一次 ['userInfo'] 请求，两个 key 各自判断登录态，
  // 会出现"header 认为已登录、侧边栏认为未登录"的竞态

  return (
    <>
      {!userInfo && (
        <Button variant="ghost" size="sm" onClick={() => router.push('/login')}>
          {t('common.login')}
        </Button>
      )}
      {!userInfo && (
        <Button variant="ghost" size="sm" onClick={() => router.push('/register')}>
          {t('common.register')}
        </Button>
      )}
    </>
  )
}
