import { Providers } from '@/components/providers'
import { RootLayout } from '@/components/layout'
import { Header } from '@/components/header'
import { SmtpSettingsForm } from '@/components/platform/smtp-settings'
import { NotifyPrefsForm } from '@/components/platform/notify-prefs'

export default function MailSettingsPage() {
  return (
    <Providers>
      <RootLayout mainHeader={<Header />}>
        <div className="w-full py-4 space-y-4">
          {/* SMTP 服务器：仅管理员可配置 */}
          <SmtpSettingsForm />
          {/* 通知开关：所有登录用户均可配置自己的 */}
          <NotifyPrefsForm />
        </div>
      </RootLayout>
    </Providers>
  )
}
