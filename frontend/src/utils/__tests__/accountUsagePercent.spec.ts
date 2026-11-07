import { describe, expect, it } from 'vitest'
import { accountMatchesUsageRange, getAccountUsagePercent } from '../accountUsagePercent'

describe('getAccountUsagePercent', () => {
  it('reads openai percent and zeroes expired windows', () => {
    const now = new Date('2026-11-06T12:00:00Z')
    expect(
      getAccountUsagePercent(
        {
          platform: 'openai',
          extra: {
            codex_5h_used_percent: 72,
            codex_5h_reset_at: '2026-11-06T11:00:00Z'
          }
        },
        '5h',
        now
      )
    ).toBe(0)
    expect(
      getAccountUsagePercent(
        {
          platform: 'openai',
          extra: {
            codex_5h_used_percent: 72,
            codex_5h_reset_at: '2026-11-06T13:00:00Z'
          }
        },
        '5h',
        now
      )
    ).toBe(72)
  })

  it('scales anthropic fractions to percent', () => {
    expect(
      getAccountUsagePercent(
        {
          platform: 'anthropic',
          session_window_end: '2099-01-01T00:00:00Z',
          extra: { session_window_utilization: 0.42 }
        },
        '5h'
      )
    ).toBeCloseTo(42)
  })
})

describe('accountMatchesUsageRange', () => {
  it('excludes accounts without snapshots when a bound is set', () => {
    expect(
      accountMatchesUsageRange(
        { platform: 'gemini', extra: {} },
        { usage_5h_min: 10 }
      )
    ).toBe(false)
  })

  it('matches openai accounts inside the range', () => {
    expect(
      accountMatchesUsageRange(
        {
          platform: 'openai',
          extra: {
            codex_5h_used_percent: 55,
            codex_5h_reset_at: '2099-01-01T00:00:00Z'
          }
        },
        { usage_5h_min: 50, usage_5h_max: 60 }
      )
    ).toBe(true)
  })
})
