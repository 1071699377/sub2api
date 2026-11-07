export default {
  trafficMonitor: {
    title: 'Traffic Monitor',
    description:
      'Realtime abnormal users from window max/min/avg and robust population stats; highlight users that keep hitting',
    refresh: 'Refresh',
    autoRefresh: 'Auto-refresh every 10s',
    autoRefreshPaused: 'Auto-refresh paused',
    pause: 'Pause',
    resume: 'Resume',
    window: 'Window',
    windows: {
      '1min': '1 min',
      '5min': '5 min',
      '30min': '30 min',
      '1h': '1 hour'
    },
    persistHits: 'Persist hits N',
    persistHitsHint: 'Highlight users with consecutive hits ≥ N (~1 hit per 10s)',
    sortBy: 'Sort',
    sort: {
      persist: 'Persist first',
      zScore: 'Z-score',
      current: 'Current traffic'
    },
    disabled: 'Realtime monitoring is disabled. Enable it in Ops settings first.',
    empty: 'No abnormal candidates in the current window',
    emptyUnconfirmedOnly: 'Only unconfirmed candidates so far — lower N or keep watching',
    loading: 'Loading…',
    lastUpdated: 'Updated at {time}',
    persistentTitle: 'Persistent anomalies',
    persistentEmpty: 'No users reached the persist threshold yet',
    badgeConfirmed: 'Confirmed',
    badgePersistent: 'Persist ×{count}',
    badgeWatching: 'Watching',
    deltaUp: '↑',
    deltaDown: '↓',
    deltaFlat: '→',
    summary: {
      activeUsers: 'Active users',
      persistent: 'Persistent',
      candidates: 'Candidates',
      max: 'Max req/min',
      min: 'Min req/min',
      avg: 'Avg req/min',
      median: 'Median',
      mad: 'MAD'
    },
    columns: {
      user: 'User',
      hits: 'Hits',
      current: 'Current',
      max: 'Max',
      min: 'Min',
      avg: 'Avg',
      zScore: 'Z-score',
      concurrency: 'Concurrency',
      queue: 'Queue',
      reasons: 'Reasons',
      status: 'Status'
    },
    reasons: {
      z_score: 'High vs population',
      near_population_max: 'Near window peak',
      high_concurrency: 'High concurrency / queue'
    },
    openUsage: 'View usage'
  }
}
