/**
 * Read unified 0–100 upstream usage-window percent from account.extra
 * (mirrors backend accountUsagePercentSQL expiry / scale rules).
 */

const CN_PLATFORMS = new Set([
  'kimi',
  'zhipu',
  'deepseek',
  'minimax',
  'opencode_go',
  'command_code',
  'cline'
])

function asNumber(value: unknown): number | null {
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string' && value.trim() !== '') {
    const n = Number(value)
    return Number.isFinite(n) ? n : null
  }
  return null
}

function parseResetAt(value: unknown): Date | null {
  if (value == null) return null
  if (typeof value === 'number' && Number.isFinite(value)) {
    // unix seconds when > 1e9-ish; otherwise treat as ms
    const ms = value > 1e12 ? value : value * 1000
    const d = new Date(ms)
    return Number.isNaN(d.getTime()) ? null : d
  }
  const d = new Date(String(value))
  return Number.isNaN(d.getTime()) ? null : d
}

function inRange(value: number | null, min?: number | null, max?: number | null): boolean {
  if (min == null && max == null) return true
  if (value == null) return false
  if (min != null && value < min) return false
  if (max != null && value > max) return false
  return true
}

export function getAccountUsagePercent(
  account: {
    platform?: string
    session_window_end?: string | null
    extra?: Record<string, unknown> | null
  },
  window: '5h' | '7d',
  now: Date = new Date()
): number | null {
  const platform = String(account.platform || '')
  const extra = account.extra || {}

  if (platform === 'openai') {
    const usedKey = window === '5h' ? 'codex_5h_used_percent' : 'codex_7d_used_percent'
    const resetKey = window === '5h' ? 'codex_5h_reset_at' : 'codex_7d_reset_at'
    const used = asNumber(extra[usedKey])
    if (used == null) return null
    const resetAt = parseResetAt(extra[resetKey])
    if (resetAt && resetAt.getTime() <= now.getTime()) return 0
    return used
  }

  if (platform === 'anthropic') {
    if (window === '5h') {
      const used = asNumber(extra.session_window_utilization)
      if (used == null) return null
      const end = account.session_window_end ? new Date(account.session_window_end) : null
      if (end && !Number.isNaN(end.getTime()) && end.getTime() <= now.getTime()) return 0
      return used * 100
    }
    const used = asNumber(extra.passive_usage_7d_utilization)
    if (used == null) return null
    const resetAt = parseResetAt(extra.passive_usage_7d_reset)
    if (resetAt && resetAt.getTime() <= now.getTime()) return 0
    return used * 100
  }

  if (CN_PLATFORMS.has(platform)) {
    const usedKey = window === '5h' ? `${platform}_5h_used_percent` : `${platform}_weekly_used_percent`
    const resetKey = window === '5h' ? `${platform}_5h_reset_at` : `${platform}_weekly_reset_at`
    const used = asNumber(extra[usedKey])
    if (used == null) return null
    const resetAt = parseResetAt(extra[resetKey])
    if (resetAt && resetAt.getTime() <= now.getTime()) return 0
    return used
  }

  return null
}

export function accountMatchesUsageRange(
  account: {
    platform?: string
    session_window_end?: string | null
    extra?: Record<string, unknown> | null
  },
  filters: {
    usage_5h_min?: number | string | null
    usage_5h_max?: number | string | null
    usage_7d_min?: number | string | null
    usage_7d_max?: number | string | null
  }
): boolean {
  const toBound = (v: unknown): number | null => {
    if (v == null || v === '') return null
    const n = typeof v === 'number' ? v : Number(v)
    return Number.isFinite(n) ? n : null
  }
  const min5h = toBound(filters.usage_5h_min)
  const max5h = toBound(filters.usage_5h_max)
  const min7d = toBound(filters.usage_7d_min)
  const max7d = toBound(filters.usage_7d_max)
  if (min5h == null && max5h == null && min7d == null && max7d == null) return true
  if (min5h != null || max5h != null) {
    if (!inRange(getAccountUsagePercent(account, '5h'), min5h, max5h)) return false
  }
  if (min7d != null || max7d != null) {
    if (!inRange(getAccountUsagePercent(account, '7d'), min7d, max7d)) return false
  }
  return true
}

export function normalizeUsageBoundParam(value: unknown): string | undefined {
  if (value == null || value === '') return undefined
  const n = typeof value === 'number' ? value : Number(value)
  if (!Number.isFinite(n)) return undefined
  return String(n)
}
