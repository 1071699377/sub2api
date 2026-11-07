<template>
  <AppLayout>
    <div class="space-y-6 pb-12">
      <div class="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">
            {{ t('admin.trafficMonitor.title') }}
          </h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
            {{ t('admin.trafficMonitor.description') }}
          </p>
        </div>
        <div class="flex flex-wrap items-center gap-2 sm:gap-3">
          <label class="flex items-center gap-1.5 text-xs text-gray-500 dark:text-gray-400">
            <span>{{ t('admin.trafficMonitor.window') }}</span>
            <select
              v-model="windowKey"
              class="rounded-md border border-gray-300 bg-white px-2 py-1.5 text-sm text-gray-800 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-100"
              @change="onWindowChange"
            >
              <option v-for="w in windowOptions" :key="w" :value="w">
                {{ t(`admin.trafficMonitor.windows.${w}`) }}
              </option>
            </select>
          </label>

          <label
            class="flex items-center gap-1.5 text-xs text-gray-500 dark:text-gray-400"
            :title="t('admin.trafficMonitor.persistHitsHint')"
          >
            <span>{{ t('admin.trafficMonitor.persistHits') }}</span>
            <input
              v-model.number="persistHits"
              type="number"
              min="2"
              max="30"
              class="w-16 rounded-md border border-gray-300 bg-white px-2 py-1.5 text-sm text-gray-800 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-100"
              @change="onPersistHitsChange"
            >
          </label>

          <label class="flex items-center gap-1.5 text-xs text-gray-500 dark:text-gray-400">
            <span>{{ t('admin.trafficMonitor.sortBy') }}</span>
            <select
              v-model="sortKey"
              class="rounded-md border border-gray-300 bg-white px-2 py-1.5 text-sm text-gray-800 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-100"
            >
              <option value="persist">{{ t('admin.trafficMonitor.sort.persist') }}</option>
              <option value="zScore">{{ t('admin.trafficMonitor.sort.zScore') }}</option>
              <option value="current">{{ t('admin.trafficMonitor.sort.current') }}</option>
            </select>
          </label>

          <span class="text-xs text-gray-500 dark:text-gray-400">
            {{ paused ? t('admin.trafficMonitor.autoRefreshPaused') : t('admin.trafficMonitor.autoRefresh') }}
          </span>
          <button
            type="button"
            class="rounded-md border border-gray-300 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50 dark:border-dark-600 dark:text-gray-200 dark:hover:bg-dark-800"
            @click="togglePause"
          >
            {{ paused ? t('admin.trafficMonitor.resume') : t('admin.trafficMonitor.pause') }}
          </button>
          <button
            type="button"
            class="rounded-md bg-primary-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-primary-700 disabled:opacity-50"
            :disabled="loading"
            @click="load"
          >
            {{ t('admin.trafficMonitor.refresh') }}
          </button>
        </div>
      </div>

      <div
        v-if="errorMessage"
        class="rounded-xl bg-red-50 p-4 text-sm text-red-600 dark:bg-red-900/20 dark:text-red-400"
      >
        {{ errorMessage }}
      </div>

      <div
        v-else-if="payload && !payload.enabled"
        class="rounded-xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-800 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-200"
      >
        {{ t('admin.trafficMonitor.disabled') }}
      </div>

      <template v-else>
        <div class="grid grid-cols-2 gap-3 md:grid-cols-4 lg:grid-cols-8">
          <div
            v-for="card in summaryCards"
            :key="card.key"
            class="rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-900"
            :class="card.key === 'persistent' && persistentItems.length ? 'border-red-300 dark:border-red-800' : ''"
          >
            <div class="text-xs text-gray-500 dark:text-gray-400">{{ card.label }}</div>
            <div
              class="mt-1 text-xl font-semibold"
              :class="card.key === 'persistent' && persistentItems.length
                ? 'text-red-600 dark:text-red-400'
                : 'text-gray-900 dark:text-white'"
            >
              {{ card.value }}
            </div>
          </div>
        </div>

        <!-- Persistent anomalies -->
        <div class="overflow-hidden rounded-xl border border-red-200 bg-white dark:border-red-900/50 dark:bg-dark-900">
          <div class="flex items-center justify-between border-b border-red-100 px-4 py-3 dark:border-red-900/40">
            <span class="text-sm font-medium text-red-700 dark:text-red-300">
              {{ t('admin.trafficMonitor.persistentTitle') }}
              <span class="ml-1 text-xs font-normal text-red-500">(≥ {{ persistHits }})</span>
            </span>
            <span v-if="lastUpdated" class="text-xs text-gray-400">
              {{ t('admin.trafficMonitor.lastUpdated', { time: lastUpdated }) }}
            </span>
          </div>
          <div v-if="!persistentItems.length" class="p-4 text-sm text-gray-500">
            {{ t('admin.trafficMonitor.persistentEmpty') }}
          </div>
          <ul v-else class="divide-y divide-red-50 dark:divide-red-950/40">
            <li
              v-for="row in persistentItems"
              :key="`persist-${row.user_id}`"
              class="flex flex-col gap-2 px-4 py-3 sm:flex-row sm:items-center sm:justify-between bg-red-50/60 dark:bg-red-950/20 border-l-4 border-red-500"
            >
              <div class="min-w-0">
                <div class="flex flex-wrap items-center gap-2">
                  <span class="font-medium text-gray-900 dark:text-white">{{ displayName(row) }}</span>
                  <span class="inline-flex rounded bg-red-600 px-1.5 py-0.5 text-xs text-white">
                    {{ t('admin.trafficMonitor.badgePersistent', { count: row.hit_count || 0 }) }}
                  </span>
                  <span
                    v-if="row.confirmed"
                    class="inline-flex rounded bg-amber-100 px-1.5 py-0.5 text-xs text-amber-800 dark:bg-amber-900/40 dark:text-amber-200"
                  >
                    {{ t('admin.trafficMonitor.badgeConfirmed') }}
                  </span>
                </div>
                <div class="mt-0.5 text-xs text-gray-500">
                  #{{ row.user_id }} · {{ row.email || '—' }}
                  · {{ fmt(row.current) }} req/min
                  · z {{ fmt(row.z_score) }}
                  · {{ row.current_in_use }}/{{ row.max_capacity || '—' }}
                  <template v-if="row.waiting_in_queue"> · q {{ row.waiting_in_queue }}</template>
                </div>
                <div class="mt-1 flex flex-wrap gap-1">
                  <span
                    v-for="reason in row.reasons"
                    :key="reason"
                    class="inline-flex rounded bg-amber-100 px-1.5 py-0.5 text-xs text-amber-800 dark:bg-amber-900/40 dark:text-amber-200"
                  >
                    {{ reasonLabel(reason) }}
                  </span>
                </div>
              </div>
              <RouterLink
                class="shrink-0 text-sm text-primary-600 hover:underline dark:text-primary-400"
                :to="{ path: '/admin/usage', query: { user_id: String(row.user_id) } }"
              >
                {{ t('admin.trafficMonitor.openUsage') }}
              </RouterLink>
            </li>
          </ul>
        </div>

        <!-- Full candidate table -->
        <div class="overflow-hidden rounded-xl border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-900">
          <div class="flex items-center justify-between border-b border-gray-100 px-4 py-3 dark:border-dark-700">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-200">
              {{ t('admin.trafficMonitor.title') }}
            </span>
          </div>

          <div v-if="loading && !payload" class="p-6 text-sm text-gray-500">
            {{ t('admin.trafficMonitor.loading') }}
          </div>
          <div v-else-if="!items.length" class="p-6 text-sm text-gray-500">
            {{ t('admin.trafficMonitor.empty') }}
          </div>
          <div v-else class="overflow-x-auto">
            <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-700">
              <thead class="bg-gray-50 dark:bg-dark-800">
                <tr>
                  <th class="px-4 py-2 text-left font-medium text-gray-500">{{ t('admin.trafficMonitor.columns.user') }}</th>
                  <th class="px-4 py-2 text-left font-medium text-gray-500">{{ t('admin.trafficMonitor.columns.status') }}</th>
                  <th class="px-4 py-2 text-right font-medium text-gray-500">{{ t('admin.trafficMonitor.columns.hits') }}</th>
                  <th class="px-4 py-2 text-right font-medium text-gray-500">{{ t('admin.trafficMonitor.columns.current') }}</th>
                  <th class="px-4 py-2 text-right font-medium text-gray-500">{{ t('admin.trafficMonitor.columns.max') }}</th>
                  <th class="px-4 py-2 text-right font-medium text-gray-500">{{ t('admin.trafficMonitor.columns.min') }}</th>
                  <th class="px-4 py-2 text-right font-medium text-gray-500">{{ t('admin.trafficMonitor.columns.avg') }}</th>
                  <th class="px-4 py-2 text-right font-medium text-gray-500">{{ t('admin.trafficMonitor.columns.zScore') }}</th>
                  <th class="px-4 py-2 text-right font-medium text-gray-500">{{ t('admin.trafficMonitor.columns.concurrency') }}</th>
                  <th class="px-4 py-2 text-right font-medium text-gray-500">{{ t('admin.trafficMonitor.columns.queue') }}</th>
                  <th class="px-4 py-2 text-left font-medium text-gray-500">{{ t('admin.trafficMonitor.columns.reasons') }}</th>
                  <th class="px-4 py-2 text-right font-medium text-gray-500" />
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
                <tr
                  v-for="row in sortedItems"
                  :key="row.user_id"
                  class="hover:bg-gray-50 dark:hover:bg-dark-800/60"
                  :class="isPersistent(row)
                    ? 'bg-red-50/70 dark:bg-red-950/20 border-l-4 border-l-red-500'
                    : row.confirmed
                      ? 'bg-amber-50/40 dark:bg-amber-950/10'
                      : ''"
                >
                  <td class="px-4 py-2">
                    <div class="font-medium text-gray-900 dark:text-white">{{ displayName(row) }}</div>
                    <div class="text-xs text-gray-500">#{{ row.user_id }} · {{ row.email }}</div>
                  </td>
                  <td class="px-4 py-2">
                    <div class="flex flex-wrap gap-1">
                      <span
                        v-if="isPersistent(row)"
                        class="inline-flex rounded bg-red-600 px-1.5 py-0.5 text-xs text-white"
                      >
                        {{ t('admin.trafficMonitor.badgePersistent', { count: row.hit_count || 0 }) }}
                      </span>
                      <span
                        v-else-if="row.confirmed"
                        class="inline-flex rounded bg-amber-100 px-1.5 py-0.5 text-xs text-amber-800 dark:bg-amber-900/40 dark:text-amber-200"
                      >
                        {{ t('admin.trafficMonitor.badgeConfirmed') }}
                      </span>
                      <span
                        v-else
                        class="inline-flex rounded bg-gray-100 px-1.5 py-0.5 text-xs text-gray-600 dark:bg-dark-700 dark:text-gray-300"
                      >
                        {{ t('admin.trafficMonitor.badgeWatching') }}
                      </span>
                    </div>
                  </td>
                  <td class="px-4 py-2 text-right font-mono">{{ row.hit_count || 0 }}</td>
                  <td class="px-4 py-2 text-right font-mono">
                    {{ fmt(row.current) }}
                    <span class="ml-1 text-xs" :class="deltaClass(row)">{{ deltaLabel(row) }}</span>
                  </td>
                  <td class="px-4 py-2 text-right font-mono">{{ fmt(row.max) }}</td>
                  <td class="px-4 py-2 text-right font-mono">{{ fmt(row.min) }}</td>
                  <td class="px-4 py-2 text-right font-mono">{{ fmt(row.avg) }}</td>
                  <td class="px-4 py-2 text-right font-mono">{{ fmt(row.z_score) }}</td>
                  <td class="px-4 py-2 text-right font-mono">
                    {{ row.current_in_use }}/{{ row.max_capacity || '—' }}
                    <span class="text-xs text-gray-400">({{ fmt(row.load_percentage) }}%)</span>
                  </td>
                  <td class="px-4 py-2 text-right font-mono">{{ row.waiting_in_queue || 0 }}</td>
                  <td class="px-4 py-2">
                    <div class="flex flex-wrap gap-1">
                      <span
                        v-for="reason in row.reasons"
                        :key="reason"
                        class="inline-flex rounded bg-amber-100 px-1.5 py-0.5 text-xs text-amber-800 dark:bg-amber-900/40 dark:text-amber-200"
                      >
                        {{ reasonLabel(reason) }}
                      </span>
                    </div>
                  </td>
                  <td class="px-4 py-2 text-right">
                    <RouterLink
                      class="text-primary-600 hover:underline dark:text-primary-400"
                      :to="{ path: '/admin/usage', query: { user_id: String(row.user_id) } }"
                    >
                      {{ t('admin.trafficMonitor.openUsage') }}
                    </RouterLink>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useIntervalFn } from '@vueuse/core'
import { RouterLink } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import { adminAPI } from '@/api/admin'
import type { UserTrafficAnomalyItem, UserTrafficAnomalyResponse } from '@/api/admin/ops'

const { t } = useI18n()

const PERSIST_HITS_KEY = 'traffic_monitor_persist_hits'
const WINDOW_KEY = 'traffic_monitor_window'
const SORT_KEY = 'traffic_monitor_sort'

const windowOptions = ['1min', '5min', '30min', '1h'] as const
type WindowKey = (typeof windowOptions)[number]
type SortKey = 'persist' | 'zScore' | 'current'

const loading = ref(false)
const errorMessage = ref('')
const payload = ref<UserTrafficAnomalyResponse | null>(null)
const lastUpdated = ref('')
const paused = ref(false)
const prevCurrentByUser = ref<Record<number, number>>({})

const windowKey = ref<WindowKey>(readWindow())
const persistHits = ref(readPersistHits())
const sortKey = ref<SortKey>(readSort())

const items = computed(() => payload.value?.items ?? [])

const persistentItems = computed(() =>
  items.value
    .filter((row) => isPersistent(row))
    .slice()
    .sort((a, b) => (b.hit_count || 0) - (a.hit_count || 0) || (b.z_score || 0) - (a.z_score || 0))
)

const sortedItems = computed(() => {
  const list = items.value.slice()
  list.sort((a, b) => {
    if (sortKey.value === 'current') {
      return (b.current || 0) - (a.current || 0)
    }
    if (sortKey.value === 'zScore') {
      return (b.z_score || 0) - (a.z_score || 0) || (b.current || 0) - (a.current || 0)
    }
    // persist first
    const ap = isPersistent(a) ? 1 : 0
    const bp = isPersistent(b) ? 1 : 0
    if (ap !== bp) return bp - ap
    return (b.hit_count || 0) - (a.hit_count || 0) || (b.z_score || 0) - (a.z_score || 0)
  })
  return list
})

const summaryCards = computed(() => {
  const s = payload.value?.summary
  if (!s) return []
  return [
    { key: 'active', label: t('admin.trafficMonitor.summary.activeUsers'), value: String(s.active_users ?? 0) },
    { key: 'candidates', label: t('admin.trafficMonitor.summary.candidates'), value: String(items.value.length) },
    { key: 'persistent', label: t('admin.trafficMonitor.summary.persistent'), value: String(persistentItems.value.length) },
    { key: 'max', label: t('admin.trafficMonitor.summary.max'), value: fmt(s.max) },
    { key: 'min', label: t('admin.trafficMonitor.summary.min'), value: fmt(s.min) },
    { key: 'avg', label: t('admin.trafficMonitor.summary.avg'), value: fmt(s.avg) },
    { key: 'median', label: t('admin.trafficMonitor.summary.median'), value: fmt(s.median) },
    { key: 'mad', label: t('admin.trafficMonitor.summary.mad'), value: fmt(s.mad) }
  ]
})

function readPersistHits(): number {
  try {
    const raw = localStorage.getItem(PERSIST_HITS_KEY)
    const n = raw != null ? Number(raw) : 3
    if (!Number.isFinite(n)) return 3
    return Math.min(30, Math.max(2, Math.round(n)))
  } catch {
    return 3
  }
}

function readWindow(): WindowKey {
  try {
    const raw = localStorage.getItem(WINDOW_KEY)
    if (raw && (windowOptions as readonly string[]).includes(raw)) return raw as WindowKey
  } catch {
    /* ignore */
  }
  return '5min'
}

function readSort(): SortKey {
  try {
    const raw = localStorage.getItem(SORT_KEY)
    if (raw === 'zScore' || raw === 'current' || raw === 'persist') return raw
  } catch {
    /* ignore */
  }
  return 'persist'
}

function isPersistent(row: UserTrafficAnomalyItem): boolean {
  return (row.hit_count || 0) >= persistHits.value
}

function fmt(n: number | null | undefined): string {
  if (n == null || Number.isNaN(n)) return '0'
  return String(Math.round(n * 100) / 100)
}

function displayName(row: UserTrafficAnomalyItem): string {
  return row.username || row.email || `#${row.user_id}`
}

function reasonLabel(reason: string): string {
  const key = `admin.trafficMonitor.reasons.${reason}`
  const translated = t(key)
  return translated === key ? reason : translated
}

function deltaLabel(row: UserTrafficAnomalyItem): string {
  const prev = prevCurrentByUser.value[row.user_id]
  if (prev == null) return ''
  if (row.current > prev) return t('admin.trafficMonitor.deltaUp')
  if (row.current < prev) return t('admin.trafficMonitor.deltaDown')
  return t('admin.trafficMonitor.deltaFlat')
}

function deltaClass(row: UserTrafficAnomalyItem): string {
  const prev = prevCurrentByUser.value[row.user_id]
  if (prev == null) return 'text-gray-400'
  if (row.current > prev) return 'text-red-500'
  if (row.current < prev) return 'text-emerald-500'
  return 'text-gray-400'
}

function onPersistHitsChange() {
  const n = Math.min(30, Math.max(2, Math.round(Number(persistHits.value) || 3)))
  persistHits.value = n
  try {
    localStorage.setItem(PERSIST_HITS_KEY, String(n))
  } catch {
    /* ignore */
  }
}

function onWindowChange() {
  try {
    localStorage.setItem(WINDOW_KEY, windowKey.value)
  } catch {
    /* ignore */
  }
  void load()
}

watch(sortKey, (value) => {
  try {
    localStorage.setItem(SORT_KEY, value)
  } catch {
    /* ignore */
  }
})

async function load() {
  loading.value = true
  errorMessage.value = ''
  try {
    const data = await adminAPI.ops.getUserTrafficAnomaly(windowKey.value)
    const nextPrev: Record<number, number> = {}
    for (const item of payload.value?.items ?? []) {
      nextPrev[item.user_id] = item.current
    }
    prevCurrentByUser.value = nextPrev
    payload.value = data
    lastUpdated.value = new Date().toLocaleTimeString()
  } catch (err: any) {
    errorMessage.value = err?.message || String(err)
  } finally {
    loading.value = false
  }
}

const { pause, resume } = useIntervalFn(() => {
  void load()
}, 10000, { immediate: false })

function togglePause() {
  if (paused.value) {
    paused.value = false
    resume()
  } else {
    paused.value = true
    pause()
  }
}

onMounted(() => {
  void load()
  resume()
})

onUnmounted(() => {
  pause()
})
</script>
