<template>
  <div class="card">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-white">OpenAI 站点策略</h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
        控制站点对外提供哪些 OpenAI 模型，以及是否允许生图。保存后立即生效（多实例约 30 秒内同步）。
      </p>
    </div>

    <div v-if="loading" class="flex items-center gap-2 p-6 text-gray-500">
      <div class="h-4 w-4 animate-spin rounded-full border-b-2 border-primary-600"></div>
      {{ t('common.loading') }}
    </div>

    <div v-else class="space-y-6 p-6">
      <!-- 全局生图开关 -->
      <div class="flex items-center justify-between">
        <div>
          <label class="font-medium text-gray-900 dark:text-white">允许生图</label>
          <p class="text-sm text-gray-500 dark:text-gray-400">
            关闭后所有分组（包括未绑定分组的 Key）的生图请求一律返回 403：/v1/images、异步生图任务、Responses/Chat 里的生图工具。Codex 客户端自带的 image_generation 工具会被自动去掉，不影响正常对话。
          </p>
        </div>
        <Toggle v-model="form.image_generation_enabled" />
      </div>

      <!-- 模型白名单 -->
      <div class="space-y-4 border-t border-gray-100 pt-5 dark:border-dark-700">
        <div class="flex items-center justify-between">
          <div>
            <label class="font-medium text-gray-900 dark:text-white">限制可用的 OpenAI 模型</label>
            <p class="text-sm text-gray-500 dark:text-gray-400">
              开启后：新建/导入的 OpenAI 账号只映射下面勾选的模型；账号「同步上游模型」和 /v1/models、Codex 模型列表只显示勾选的模型。关闭表示不限制。
            </p>
          </div>
          <Toggle v-model="restricted" />
        </div>

        <template v-if="restricted">
          <!-- 已选模型 -->
          <div>
            <div class="mb-2 flex items-center justify-between">
              <span class="text-sm font-medium text-gray-700 dark:text-gray-300">已选模型（{{ selected.length }}）</span>
              <button v-if="selected.length > 0" type="button" class="text-xs text-red-600 hover:underline" @click="selected = []">清空</button>
            </div>
            <div v-if="selected.length === 0" class="rounded-lg border border-dashed border-gray-300 p-3 text-sm text-gray-500 dark:border-dark-600">
              还没有选择模型，请从下方候选中勾选，或手动添加。
            </div>
            <div v-else class="flex flex-wrap gap-2">
              <span
                v-for="model in selected"
                :key="model"
                class="inline-flex items-center gap-1 rounded-full bg-primary-50 px-3 py-1 text-xs font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300"
              >
                {{ model }}
                <button type="button" class="ml-0.5 text-primary-500 hover:text-red-600" :aria-label="`移除 ${model}`" @click="toggleModel(model)">×</button>
              </span>
            </div>
          </div>

          <!-- 候选来源 -->
          <div class="flex flex-wrap items-center gap-2">
            <button type="button" class="btn btn-secondary btn-sm" :disabled="syncing" @click="syncUpstream">
              {{ syncing ? '正在同步…' : '从上游同步' }}
            </button>
            <input
              v-model="customModel"
              type="text"
              class="input w-56"
              placeholder="手动输入模型 ID，回车添加"
              @keydown.enter.prevent="addCustom"
            />
            <button type="button" class="btn btn-secondary btn-sm" @click="addCustom">添加</button>
            <span v-if="syncInfo" class="text-xs text-gray-500 dark:text-gray-400">{{ syncInfo }}</span>
          </div>

          <div>
            <div class="mb-2 flex items-center justify-between">
              <span class="text-sm font-medium text-gray-700 dark:text-gray-300">
                候选模型（{{ candidates.length }}）
                <span class="font-normal text-gray-500">内置目录{{ upstreamModels.length > 0 ? ' + 上游同步结果' : '' }}</span>
              </span>
              <div class="flex items-center gap-3 text-xs">
                <input v-model="filter" type="text" class="input h-7 w-40 text-xs" placeholder="筛选" />
                <button type="button" class="text-primary-600 hover:underline" @click="selectAllVisible">全选当前</button>
              </div>
            </div>
            <div class="grid max-h-72 grid-cols-1 gap-1 overflow-y-auto rounded-lg border border-gray-200 p-2 sm:grid-cols-2 lg:grid-cols-3 dark:border-dark-600">
              <label
                v-for="model in visibleCandidates"
                :key="model"
                class="flex cursor-pointer items-center gap-2 rounded px-2 py-1 text-sm hover:bg-gray-50 dark:hover:bg-dark-700"
              >
                <input type="checkbox" :checked="selectedSet.has(model)" @change="toggleModel(model)" />
                <span class="truncate text-gray-800 dark:text-gray-200" :title="model">{{ model }}</span>
                <span v-if="upstreamSet.has(model)" class="ml-auto shrink-0 text-[10px] text-emerald-600">上游</span>
              </label>
              <div v-if="visibleCandidates.length === 0" class="col-span-full p-2 text-sm text-gray-500">没有匹配的模型</div>
            </div>
          </div>
        </template>
      </div>

      <!-- 保存 -->
      <div class="flex flex-wrap items-center justify-between gap-3 border-t border-gray-100 pt-5 dark:border-dark-700">
        <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
          <input v-model="applyToAccounts" type="checkbox" />
          保存时同时更新所有现有 OpenAI 账号的模型白名单
        </label>
        <button type="button" class="btn btn-primary" :disabled="saving" @click="save">
          {{ saving ? t('common.saving') : t('common.save') }}
        </button>
      </div>
      <p class="-mt-3 text-xs text-gray-500 dark:text-gray-400">
        勾选后，所有 OpenAI 账号（spark 影子账号除外）的 model_mapping 会被替换为上面的列表；如果关闭了限制，会清空它们的 model_mapping，账号恢复为支持全部模型。
      </p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'
import { useAppStore } from '@/stores'
import { extractApiErrorMessage } from '@/utils/apiError'
import {
  applyOpenAISitePolicyToAccounts,
  getOpenAISitePolicy,
  syncOpenAISitePolicyUpstreamModels,
  updateOpenAISitePolicy
} from '@/api/admin/openaiSitePolicy'
import { applyOpenAISitePolicy } from '@/composables/useModelWhitelist'

const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(true)
const saving = ref(false)
const syncing = ref(false)
const restricted = ref(true)
const applyToAccounts = ref(true)
const selected = ref<string[]>([])
const defaultModels = ref<string[]>([])
const upstreamModels = ref<string[]>([])
const customModel = ref('')
const filter = ref('')
const syncInfo = ref('')
const form = reactive({ image_generation_enabled: false })

const selectedSet = computed(() => new Set(selected.value))
const upstreamSet = computed(() => new Set(upstreamModels.value))

// 候选 = 已选 ∪ 上游同步结果 ∪ 内置目录（已选的排在最前，便于查看）
const candidates = computed(() => {
  const out: string[] = []
  const seen = new Set<string>()
  for (const model of [...selected.value, ...upstreamModels.value, ...defaultModels.value]) {
    if (!model || seen.has(model)) continue
    seen.add(model)
    out.push(model)
  }
  return out
})

const visibleCandidates = computed(() => {
  const keyword = filter.value.trim().toLowerCase()
  if (!keyword) return candidates.value
  return candidates.value.filter(model => model.toLowerCase().includes(keyword))
})

function toggleModel(model: string) {
  if (selectedSet.value.has(model)) {
    selected.value = selected.value.filter(m => m !== model)
  } else {
    selected.value = [...selected.value, model]
  }
}

function selectAllVisible() {
  const next = [...selected.value]
  for (const model of visibleCandidates.value) {
    if (!next.includes(model)) next.push(model)
  }
  selected.value = next
}

function addCustom() {
  const model = customModel.value.trim()
  if (!model) return
  if (/\s/.test(model)) {
    appStore.showError('模型 ID 不能包含空白字符')
    return
  }
  if (!selectedSet.value.has(model)) selected.value = [...selected.value, model]
  customModel.value = ''
}

async function load() {
  loading.value = true
  try {
    const policy = await getOpenAISitePolicy()
    form.image_generation_enabled = policy.image_generation_enabled
    restricted.value = policy.allowed_models.length > 0
    selected.value = [...policy.allowed_models]
    defaultModels.value = policy.default_models || []
    applyOpenAISitePolicy(policy)
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, '加载 OpenAI 站点策略失败'))
  } finally {
    loading.value = false
  }
}

async function syncUpstream() {
  syncing.value = true
  syncInfo.value = ''
  try {
    const result = await syncOpenAISitePolicyUpstreamModels()
    upstreamModels.value = result.models
    if (result.accounts_probed === 0) {
      syncInfo.value = '没有可用的 OpenAI 账号可供同步'
    } else {
      syncInfo.value = `已从 ${result.accounts_probed - result.accounts_failed}/${result.accounts_probed} 个账号获取到 ${result.models.length} 个模型`
    }
    if (result.accounts_failed > 0 && result.errors?.length) {
      appStore.showWarning(`部分账号同步失败：${result.errors[0]}`)
    }
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, '从上游同步模型失败'))
  } finally {
    syncing.value = false
  }
}

async function save() {
  if (restricted.value && selected.value.length === 0) {
    appStore.showError('已开启模型限制，请至少选择一个模型；如果不想限制，请关闭「限制可用的 OpenAI 模型」')
    return
  }
  saving.value = true
  try {
    const policy = await updateOpenAISitePolicy({
      allowed_models: restricted.value ? selected.value : [],
      image_generation_enabled: form.image_generation_enabled
    })
    applyOpenAISitePolicy(policy)
    selected.value = [...policy.allowed_models]
    restricted.value = policy.allowed_models.length > 0
    if (applyToAccounts.value) {
      const result = await applyOpenAISitePolicyToAccounts()
      if (result.failed > 0) {
        appStore.showWarning(`已保存；账号更新 ${result.updated} 个成功，${result.failed} 个失败：${result.errors?.[0] ?? ''}`)
      } else {
        appStore.showSuccess(`已保存，并更新了 ${result.updated} 个 OpenAI 账号`)
      }
    } else {
      appStore.showSuccess('已保存')
    }
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, '保存 OpenAI 站点策略失败'))
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>
