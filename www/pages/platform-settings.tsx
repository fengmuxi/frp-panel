import { Providers } from '@/components/providers'
import { RootLayout } from '@/components/layout'
import { Header } from '@/components/header'
import { PlatformSettingsForm } from '@/components/platform/settings'
import { SmtpSettingsForm } from '@/components/platform/smtp-settings'

export default function PlatformSettingsPage() {
  return (
    <Providers>
      <RootLayout mainHeader={<Header />}>
        <div className="w-full py-4 space-y-4">
          <PlatformSettingsForm />
          <SmtpSettingsForm />
        </div>
      </RootLayout>
    </Providers>
  )
}
