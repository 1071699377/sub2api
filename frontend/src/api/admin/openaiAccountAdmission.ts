/**
 * 新 OpenAI OAuth 账号准入设置
 */

import { apiClient } from '../client'

export interface OpenAIAccountAdmissionSettings {
  enabled: boolean
  model_id: string
  reasoning_effort: string
  wait_seconds: number
  pass_count: number
  passed_concurrency: number
  isolation_count: number
  isolation_group_id: number
  continue_after_pass: boolean
  pass_answer: string
  degraded_answer: string
}

export const OPENAI_ADMISSION_REASONING_EFFORTS = ['none', 'minimal', 'low', 'medium', 'high', 'xhigh'] as const

export async function getOpenAIAccountAdmissionSettings(): Promise<OpenAIAccountAdmissionSettings> {
  const { data } = await apiClient.get<OpenAIAccountAdmissionSettings>('/admin/settings/openai-account-admission')
  return data
}

export async function updateOpenAIAccountAdmissionSettings(
  settings: OpenAIAccountAdmissionSettings
): Promise<OpenAIAccountAdmissionSettings> {
  const { data } = await apiClient.put<OpenAIAccountAdmissionSettings>('/admin/settings/openai-account-admission', settings)
  return data
}
