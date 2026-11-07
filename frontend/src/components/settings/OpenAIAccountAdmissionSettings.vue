<template>
  <div class="card">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
        {{ t('admin.settings.openaiAccountAdmission.title') }}
      </h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
        {{ t('admin.settings.openaiAccountAdmission.description') }}
      </p>
    </div>
    <div class="space-y-5 p-6">
      <div v-if="loading" class="flex items-center gap-2 text-gray-500">
        <div class="h-4 w-4 animate-spin rounded-full border-b-2 border-primary-600"></div>
        {{ t('common.loading') }}
      </div>

      <p v-else-if="loadError" class="text-sm text-red-600 dark:text-red-400" data-testid="openai-admission-load-error">
        {{ loadError }}
      </p>

      <template v-else>
        <div class="flex items-center justify-between">
          <div>
            <label class="font-medium text-gray-900 dark:text-white">
              {{ t('admin.settings.openaiAccountAdmission.enabled') }}
            </label>
            <p class="text-sm text-gray-500 dark:text-gray-400">
              {{ t('admin.settings.openaiAccountAdmission.enabledHint') }}
            </p>
          </div>
          <Toggle v-model="form.enabled" data-testid="openai-admission-enabled" />
        </div>

        <div class="grid grid-cols-1 gap-4 border-t border-gray-100 pt-4 dark:border-dark-700 md:grid-cols-2">
          <div class="md:col-span-2">
            <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t('admin.settings.openaiAccountAdmission.isolationGroup') }}
            </label>
            <select v-model.number="form.isolation_group_id" class="input w-full md:w-80" data-testid="openai-admission-group">
              <option :value="0">{{ t('admin.settings.openaiAccountAdmission.isolationGroupPlaceholder') }}</option>
              <option v-for="g in groups" :key="g.id" :value="g.id">{{ g.name }} (#{{ g.id }})</option>
            </select>
          </div>

          <div>
            <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t('admin.settings.openaiAccountAdmission.modelId') }}
            </label>
            <input v-model.trim="form.model_id" type="text" class="input w-full" />
          </div>
          <div>
            <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t('admin.settings.openaiAccountAdmission.reasoningEffort') }}
            </label>
            <select v-model="form.reasoning_effort" class="input w-full">
              <option v-for="e in efforts" :key="e" :value="e">{{ e }}</option>
            </select>
          </div>

          <div>
            <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t('admin.settings.openaiAccountAdmission.waitSeconds') }}
            </label>
            <input v-model.number="form.wait_seconds" type="number" min="1" max="86400" class="input w-32" />
          </div>
          <div>
            <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t('admin.settings.openaiAccountAdmission.passedConcurrency') }}
            </label>
            <input v-model.number="form.passed_concurrency" type="number" min="1" max="10000" class="input w-32" />
          </div>

          <div>
            <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t('admin.settings.openaiAccountAdmission.passCount') }} / {{ t('admin.settings.openaiAccountAdmission.passAnswer') }}
            </label>
            <div class="flex gap-2">
              <input v-model.number="form.pass_count" type="number" min="1" max="1000" class="input w-24" />
              <input v-model.trim="form.pass_answer" type="text" maxlength="32" class="input w-24" />
            </div>
          </div>
          <div>
            <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t('admin.settings.openaiAccountAdmission.isolationCount') }} / {{ t('admin.settings.openaiAccountAdmission.degradedAnswer') }}
            </label>
            <div class="flex gap-2">
              <input v-model.number="form.isolation_count" type="number" min="1" max="1000" class="input w-24" />
              <input v-model.trim="form.degraded_answer" type="text" maxlength="32" class="input w-24" />
            </div>
          </div>

          <div class="flex items-center justify-between md:col-span-2">
            <div>
              <label class="font-medium text-gray-900 dark:text-white">
                {{ t('admin.settings.openaiAccountAdmission.continueAfterPass') }}
              </label>
              <p class="text-sm text-gray-500 dark:text-gray-400">
                {{ t('admin.settings.openaiAccountAdmission.continueAfterPassHint') }}
              </p>
            </div>
            <Toggle v-model="form.continue_after_pass" />
          </div>
        </div>

        <div class="flex justify-end border-t border-gray-100 pt-4 dark:border-dark-700">
          <button
            type="button"
            class="btn btn-primary btn-sm"
            :disabled="saving"
            data-testid="openai-admission-save"
            @click="save"
          >
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'
import { useAppStore } from '@/stores'
import { getAll as getAllGroups } from '@/api/admin/groups'
import {
  getOpenAIAccountAdmissionSettings,
  updateOpenAIAccountAdmissionSettings,
  OPENAI_ADMISSION_REASONING_EFFORTS,
  type OpenAIAccountAdmissionSettings
} from '@/api/admin/openaiAccountAdmission'

const { t } = useI18n()
const appStore = useAppStore()

const efforts = OPENAI_ADMISSION_REASONING_EFFORTS
const loading = ref(true)
const saving = ref(false)
const loadError = ref('')
const groups = ref<{ id: number; name: string }[]>([])
const form = reactive<OpenAIAccountAdmissionSettings>({
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
})

function errorMessage(err: unknown, fallback: string): string {
  const e = err as { response?: { data?: { message?: string } }; message?: string }
  return e?.response?.data?.message || e?.message || fallback
}

async function load() {
  loading.value = true
  try {
    const [settings, openaiGroups] = await Promise.all([
      getOpenAIAccountAdmissionSettings(),
      getAllGroups('openai').catch(() => [])
    ])
    Object.assign(form, settings)
    groups.value = (openaiGroups || []).map((g) => ({ id: g.id, name: g.name }))
    loadError.value = ''
  } catch (err) {
    // 加载失败只在卡片内提示，不弹全局 toast，避免干扰设置页其他区块。
    loadError.value = errorMessage(err, t('admin.settings.openaiAccountAdmission.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function save() {
  if (form.enabled && !form.isolation_group_id) {
    appStore.showError(t('admin.settings.openaiAccountAdmission.isolationGroupRequired'))
    return
  }
  saving.value = true
  try {
    const updated = await updateOpenAIAccountAdmissionSettings({ ...form })
    Object.assign(form, updated)
    appStore.showSuccess(t('admin.settings.openaiAccountAdmission.saved'))
  } catch (err) {
    appStore.showError(errorMessage(err, t('admin.settings.openaiAccountAdmission.saveFailed')))
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>
