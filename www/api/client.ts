import http from '@/api/http'
import { API_PATH } from '@/lib/consts'
import {
  DeleteClientRequest,
  DeleteClientResponse,
  GetClientRequest,
  GetClientResponse,
  InitClientRequest,
  InitClientResponse,
  ListClientsRequest,
  ListClientsResponse,
  SwapClientSortRequest,
  SwapClientSortResponse,
  UpdateClientInfoRequest,
  UpdateClientInfoResponse,
  UpgradeFrppRequest,
  UpgradeFrppResponse,
} from '@/lib/pb/api_client'
import { BaseResponse } from '@/types/api'

export const getClient = async (req: GetClientRequest) => {
  const res = await http.post(API_PATH + '/client/get', GetClientRequest.toJson(req))
  return GetClientResponse.fromJson((res.data as BaseResponse).body)
}

export const listClient = async (req: ListClientsRequest) => {
  const res = await http.post(API_PATH + '/client/list', ListClientsRequest.toJson(req))
  return ListClientsResponse.fromJson((res.data as BaseResponse).body)
}

export const deleteClient = async (req: DeleteClientRequest) => {
  const res = await http.post(API_PATH + '/client/delete', DeleteClientRequest.toJson(req))
  return DeleteClientResponse.fromJson((res.data as BaseResponse).body)
}

export const initClient = async (req: InitClientRequest) => {
  console.log('attempting init client:', InitClientRequest.toJsonString(req))
  const res = await http.post(API_PATH + '/client/init', InitClientRequest.toJson(req))
  return InitClientResponse.fromJson((res.data as BaseResponse).body)
}

// 轻量更新客户端名称（备注）与排序序号
export const updateClientInfo = async (req: UpdateClientInfoRequest) => {
  const res = await http.post(API_PATH + '/client/update_info', UpdateClientInfoRequest.toJson(req))
  return UpdateClientInfoResponse.fromJson((res.data as BaseResponse).body)
}

// 与相邻项交换排序位置（跨分页由服务端处理）
export const swapClientSort = async (req: SwapClientSortRequest) => {
  const res = await http.post(API_PATH + '/client/swap_sort', SwapClientSortRequest.toJson(req))
  return SwapClientSortResponse.fromJson((res.data as BaseResponse).body)
}

export const upgradeFrpp = async (req: UpgradeFrppRequest) => {
  const res = await http.post(API_PATH + '/client/upgrade', UpgradeFrppRequest.toJson(req))
  return UpgradeFrppResponse.fromJson((res.data as BaseResponse).body)
}
