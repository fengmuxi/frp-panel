import { SET_TOKEN_HEADER, X_CLIENT_REQUEST_ID } from '@/lib/consts'
import { $token } from '@/store/user'
import { handleSessionExpired } from '@/lib/session'
import axios from 'axios'
import { v4 as uuidv4 } from 'uuid'

const instance = axios.create({})

instance.interceptors.request.use((request) => {
  // 无 token 时不再发送 "Bearer undefined"（原实现字符串拼接恒为真值）
  const rawToken = $token.get()
  if (rawToken) {
    request.headers.Authorization = 'Bearer ' + rawToken
  }
  request.headers[X_CLIENT_REQUEST_ID] = uuidv4()
  return request
})

// 会话失效判据：后端 common.ErrUnAuthorized 的 JSON 分支回 HTTP 200 + {code:401,msg}
// （protobuf 分支回 HTTP 401，走下面的 error 拦截器）
const UNAUTHORIZED_CODE = 401

instance.interceptors.response.use(
  (response) => {
    if (response.headers?.[SET_TOKEN_HEADER]) {
      $token.set(response.headers[SET_TOKEN_HEADER])
    }
    const data: any = response.data
    if (data && typeof data === 'object' && data.code === UNAUTHORIZED_CODE) {
      // JWT 过期/被吊销/用户被删除：清 token 并跳登录页，页面不再停在"请求失败"
      handleSessionExpired(typeof data.msg === 'string' ? data.msg : 'unauthorized')
      throw new Error(typeof data.msg === 'string' ? data.msg : 'unauthorized')
    }
    if (data?.code != 200) {
      throw data?.msg
    }
    return response
  },
  (error) => {
    if (error?.response?.status === UNAUTHORIZED_CODE) {
      handleSessionExpired('http 401')
    }
    throw error
  }
)

export default instance
