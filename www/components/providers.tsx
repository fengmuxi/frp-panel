import React from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  SidebarProvider,
} from "@/components/ui/sidebar"
import { Toaster } from './ui/sonner'
import { SessionGuard } from './session-guard'

const queryClient = new QueryClient()

export const Providers = ({ children }: { children: React.ReactNode }) => {
  return (
      <SidebarProvider>
        <QueryClientProvider client={queryClient}>
          {/* 全局登录态守卫：token 失效（过期/被吊销）时统一清态并跳登录页 */}
          <SessionGuard />
          {children}
        </QueryClientProvider>
        <Toaster />
      </SidebarProvider>
  )
}
