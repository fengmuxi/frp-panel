import http from '@/api/http'
import { API_PATH } from '@/lib/consts'
import { GetProxyStatsByClientIDRequest, GetProxyStatsByClientIDResponse } from '@/lib/pb/api_client'
import { GetProxyStatsByServerIDRequest, GetProxyStatsByServerIDResponse } from '@/lib/pb/api_server'
import { GetProxyTrafficStatsRequest, GetProxyTrafficStatsResponse } from '@/lib/pb/api_master'
import { BaseResponse } from '@/types/api'

/** 按时间区间查询隧道流量统计（首页总览/趋势/排行），int64 字段为 bigint，展示层用 Number() 转换 */
export const getProxyTrafficStats = async (req: GetProxyTrafficStatsRequest) => {
  const res = await http.post(API_PATH + '/proxy/traffic_stats', GetProxyTrafficStatsRequest.toJson(req))
  return GetProxyTrafficStatsResponse.fromJson((res.data as BaseResponse).body)
}

export const getProxyStatsByClientID = async (req: GetProxyStatsByClientIDRequest) => {
  // return {
  //   proxyInfos: [
  //     {
  //       name: "test",
  //       historyTrafficIn: BigInt(1024 * 1024 * 1024),
  //       historyTrafficOut: BigInt(1024 * 1024 * 1024 * 2),
  //       todayTrafficIn: BigInt(1024 * 1024 * 1024 * 4),
  //       todayTrafficOut: BigInt(1024 * 1024 * 1024 * 5),
  //     },
  //   ],
  // } as GetProxyByCIDResponse
  const res = await http.post(API_PATH + '/proxy/get_by_cid', GetProxyStatsByClientIDRequest.toJson(req))
  return GetProxyStatsByClientIDResponse.fromJson((res.data as BaseResponse).body)
}

export const getProxyStatsByServerID = async (req: GetProxyStatsByServerIDRequest) => {
  const res = await http.post(API_PATH + '/proxy/get_by_sid', GetProxyStatsByServerIDRequest.toJson(req))
  return GetProxyStatsByServerIDResponse.fromJson((res.data as BaseResponse).body)
}