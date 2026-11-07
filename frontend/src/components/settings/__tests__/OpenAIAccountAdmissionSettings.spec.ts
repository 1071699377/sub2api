import { describe, it, expect, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const { getSettings, updateSettings, getAllGroups, showError, showSuccess } = vi.hoisted(() => ({
  getSettings: vi.fn(),
  updateSettings: vi.fn(),
  getAllGroups: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn()
}))

vi.mock('@/api/admin/openaiAccountAdmission', () => ({
  getOpenAIAccountAdmissionSettings: getSettings,
  updateOpenAIAccountAdmissionSettings: updateSettings,
  OPENAI_ADMISSION_REASONING_EFFORTS: ['low', 'medium']
}))
vi.mock('@/api/admin/groups', () => ({ getAll: getAllGroups }))
vi.mock('@/stores', () => ({ useAppStore: () => ({ showError, showSuccess }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

import OpenAIAccountAdmissionSettings from '../OpenAIAccountAdmissionSettings.vue'

const base = {
  enabled: false,
  model_id: 'gpt-6.1-sol',
  reasoning_effort: 'low',
  wait_seconds: 60,
  pass_count: 6,
  passed_concurrency: 2,
  isolation_count: 6,
  isolation_group_id: 0,
  continue_after_pass: false,
  pass_answer: '21',
  degraded_answer: '29'
}

function mountCard() {
  return mount(OpenAIAccountAdmissionSettings, {
    global: { stubs: { Toggle: { props: ['modelValue'], emits: ['update:modelValue'], template: '<button class="toggle" @click="$emit(\'update:modelValue\', !modelValue)" />' } } }
  })
}

describe('OpenAIAccountAdmissionSettings', () => {
  beforeEach(() => {
    for (const fn of [getSettings, updateSettings, getAllGroups, showError, showSuccess]) fn.mockReset()
    getSettings.mockResolvedValue({ ...base })
    getAllGroups.mockResolvedValue([{ id: 7, name: 'isolation' }])
    updateSettings.mockImplementation(async (payload) => payload)
  })

  it('loads settings and only lists OpenAI groups', async () => {
    const wrapper = mountCard()
    await flushPromises()
    expect(getAllGroups).toHaveBeenCalledWith('openai')
    expect(wrapper.find('[data-testid="openai-admission-group"]').text()).toContain('isolation (#7)')
  })

  it('requires an isolation group before enabling', async () => {
    const wrapper = mountCard()
    await flushPromises()
    await wrapper.find('[data-testid="openai-admission-enabled"]').trigger('click')
    await wrapper.find('[data-testid="openai-admission-save"]').trigger('click')
    expect(updateSettings).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('admin.settings.openaiAccountAdmission.isolationGroupRequired')
  })

  it('saves the full payload', async () => {
    const wrapper = mountCard()
    await flushPromises()
    await wrapper.find('[data-testid="openai-admission-enabled"]').trigger('click')
    await wrapper.find('[data-testid="openai-admission-group"]').setValue('7')
    await wrapper.find('[data-testid="openai-admission-save"]').trigger('click')
    await flushPromises()
    expect(updateSettings).toHaveBeenCalledWith({ ...base, enabled: true, isolation_group_id: 7 })
    expect(showSuccess).toHaveBeenCalledWith('admin.settings.openaiAccountAdmission.saved')
  })

  it('shows load failures inline without a global toast', async () => {
    getSettings.mockRejectedValue(new Error('boom'))
    const wrapper = mountCard()
    await flushPromises()
    expect(wrapper.find('[data-testid="openai-admission-load-error"]').text()).toBe('boom')
    expect(showError).not.toHaveBeenCalled()
  })
})
