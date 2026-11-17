import http from '@/api/http'
import { API_PATH } from '@/lib/consts'
import { BaseResponse } from '@/types/api'

// 通知开关配置：每个用户独立读写自己的开关（服务端手写 JSON 结构，非 proto 生成）
export interface NotifyPrefs {
  enable_server_offline: boolean
  enable_client_offline: boolean
  enable_proxy_offline: boolean
  enable_login_notify: boolean
  enable_server_online: boolean
  enable_client_online: boolean
  enable_proxy_online: boolean
  // 自动规则触发的提醒
  enable_quota_exceeded: boolean
  enable_schedule_toggle: boolean
  // 下线通知防抖时长（秒），0=默认 60s
  offline_delay_server: number
  offline_delay_client: number
  offline_delay_proxy: number
}

export const getNotifyPrefs = async (): Promise<NotifyPrefs> => {
  const res = await http.post(API_PATH + '/notify/prefs/get')
  return (res.data as BaseResponse).body as NotifyPrefs
}

export const updateNotifyPrefs = async (req: NotifyPrefs): Promise<NotifyPrefs> => {
  const res = await http.post(API_PATH + '/notify/prefs/update', req)
  return (res.data as BaseResponse).body as NotifyPrefs
}

// 用管理员已保存的 SMTP 配置给当前登录用户自己发送一封样例通知；
// template 留空发送通用测试邮件，指定业务模板（server_offline/login 等）时渲染对应内容
export const sendNotifyTest = async (req: { template?: string }) => {
  const res = await http.post(API_PATH + '/notify/prefs/test', req)
  return (res.data as BaseResponse).body
}

// 资源级通知覆盖配置（三段式）：每个服务端/客户端/隧道独立设置
// 优先级：off（不提醒）> override（独立配置）> inherit（跟随用户全局，无记录默认）
export interface ResourceNotifyPrefs {
  // server | client | proxy；proxy 的 resource_id 为 proxy_config 行主键
  type: string
  resource_id: string
  mode: 'off' | 'inherit' | 'override'
  // mode=override 时的独立配置（server/client 用前 3 项，proxy 全用）
  enable_offline: boolean
  enable_online: boolean
  // 掉线防抖（秒），0=默认 60s
  offline_delay: number
  enable_quota_exceeded: boolean
  enable_schedule_toggle: boolean
}

export const getResourceNotifyPrefs = async (req: {
  type: string
  resource_id: string
}): Promise<ResourceNotifyPrefs> => {
  const res = await http.post(API_PATH + '/notify/resource/get', req)
  return (res.data as BaseResponse).body as ResourceNotifyPrefs
}

export const updateResourceNotifyPrefs = async (
  req: ResourceNotifyPrefs
): Promise<ResourceNotifyPrefs> => {
  const res = await http.post(API_PATH + '/notify/resource/update', req)
  return (res.data as BaseResponse).body as ResourceNotifyPrefs
}
