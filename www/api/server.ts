import http from '@/api/http'
import { API_PATH } from '@/lib/consts'
import {
  DeleteServerRequest,
  DeleteServerResponse,
  GetServerRequest,
  GetServerResponse,
  InitServerRequest,
  InitServerResponse,
  ListServersRequest,
  ListServersResponse,
  SwapServerSortRequest,
  SwapServerSortResponse,
  UpdateServerInfoRequest,
  UpdateServerInfoResponse,
} from '@/lib/pb/api_server'
import { BaseResponse } from '@/types/api'

export const getServer = async (req: GetServerRequest) => {
  const res = await http.post(API_PATH + '/server/get', GetServerRequest.toJson(req))
  return GetServerResponse.fromJson((res.data as BaseResponse).body)
}

export const listServer = async (req: ListServersRequest) => {
  const res = await http.post(API_PATH + '/server/list', ListServersRequest.toJson(req))
  return ListServersResponse.fromJson((res.data as BaseResponse).body)
}

export const deleteServer = async (req: DeleteServerRequest) => {
  const res = await http.post(API_PATH + '/server/delete', DeleteServerRequest.toJson(req))
  return DeleteServerResponse.fromJson((res.data as BaseResponse).body)
}

export const initServer = async (req: InitServerRequest) => {
  const res = await http.post(API_PATH + '/server/init', InitServerRequest.toJson(req))
  return InitServerResponse.fromJson((res.data as BaseResponse).body)
}

// 轻量更新服务端名称（备注）与排序序号
export const updateServerInfo = async (req: UpdateServerInfoRequest) => {
  const res = await http.post(API_PATH + '/server/update_info', UpdateServerInfoRequest.toJson(req))
  return UpdateServerInfoResponse.fromJson((res.data as BaseResponse).body)
}

// 与相邻项交换排序位置（跨分页由服务端处理）
export const swapServerSort = async (req: SwapServerSortRequest) => {
  const res = await http.post(API_PATH + '/server/swap_sort', SwapServerSortRequest.toJson(req))
  return SwapServerSortResponse.fromJson((res.data as BaseResponse).body)
}
