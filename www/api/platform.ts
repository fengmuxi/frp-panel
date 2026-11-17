import http from '@/api/http'
import { API_PATH } from '@/lib/consts'
import { GetClientsStatusRequest, GetClientsStatusResponse } from '@/lib/pb/api_master'
import { GetPlatformInfoResponse } from '@/lib/pb/api_user'
import { BaseResponse } from '@/types/api'

export const getPlatformInfo = async () => {
  const res = await http.get(API_PATH + '/platform/baseinfo')
  return GetPlatformInfoResponse.fromJson((res.data as BaseResponse).body)
}

export const getClientsStatus = async (req: GetClientsStatusRequest) => {
  const res = await http.post(API_PATH + '/platform/clientsstatus', GetClientsStatusRequest.toJson(req))
  return GetClientsStatusResponse.fromJson((res.data as BaseResponse).body)
}

// SMTP 服务器连接配置（仅管理员可改；通知开关见 api/notify.ts，按用户独立）
export interface SmtpSettings {
  host: string
  port: number
  username: string
  password: string
  has_password: boolean
  from: string
  from_name: string
  encryption: string
}

export const getSmtpSettings = async (): Promise<SmtpSettings> => {
  const res = await http.post(API_PATH + '/platform/smtp/get')
  return (res.data as BaseResponse).body as SmtpSettings
}

export const updateSmtpSettings = async (req: SmtpSettings): Promise<SmtpSettings> => {
  const res = await http.post(API_PATH + '/platform/smtp/update', req)
  return (res.data as BaseResponse).body as SmtpSettings
}

// 发送通用连通性测试邮件，密码留空表示沿用已保存的密码，to 留空表示发给当前登录用户
export const sendTestEmail = async (req: Partial<SmtpSettings> & { to?: string }) => {
  const res = await http.post(API_PATH + '/platform/smtp/test', req)
  return (res.data as BaseResponse).body
}
