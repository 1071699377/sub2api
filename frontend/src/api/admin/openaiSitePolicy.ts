/**
 * OpenAI 站点策略 API：站点模型白名单 + 全局生图开关。
 * 后端：backend/internal/handler/admin/openai_site_policy_handler.go
 */
import { apiClient } from '../client'

export interface OpenAISitePolicy {
  /** 站点允许的 OpenAI 模型；空数组表示不限制 */
  allowed_models: string[]
  /** 全局生图开关，关闭时所有生图请求 403 */
  image_generation_enabled: boolean
  /** 内置 OpenAI 模型目录（手动选择时的候选） */
  default_models: string[]
}

export interface UpdateOpenAISitePolicyRequest {
  allowed_models: string[]
  image_generation_enabled: boolean
}

export interface OpenAISiteUpstreamModelsResult {
  models: string[]
  accounts_probed: number
  accounts_failed: number
  errors?: string[]
}

export interface OpenAISitePolicyApplyResult {
  updated: number
  skipped: number
  failed: number
  errors?: string[]
}

const BASE = '/admin/settings/openai-site-policy'

export async function getOpenAISitePolicy(): Promise<OpenAISitePolicy> {
  const { data } = await apiClient.get<OpenAISitePolicy>(BASE)
  return data
}

export async function updateOpenAISitePolicy(payload: UpdateOpenAISitePolicyRequest): Promise<OpenAISitePolicy> {
  const { data } = await apiClient.put<OpenAISitePolicy>(BASE, payload)
  return data
}

export async function syncOpenAISitePolicyUpstreamModels(): Promise<OpenAISiteUpstreamModelsResult> {
  const { data } = await apiClient.post<OpenAISiteUpstreamModelsResult>(`${BASE}/sync-upstream`, {}, { timeout: 120000 })
  return data
}

export async function applyOpenAISitePolicyToAccounts(): Promise<OpenAISitePolicyApplyResult> {
  const { data } = await apiClient.post<OpenAISitePolicyApplyResult>(`${BASE}/apply-to-accounts`, {}, { timeout: 120000 })
  return data
}

export const openaiSitePolicyAPI = {
  getOpenAISitePolicy,
  updateOpenAISitePolicy,
  syncOpenAISitePolicyUpstreamModels,
  applyOpenAISitePolicyToAccounts
}

export default openaiSitePolicyAPI
