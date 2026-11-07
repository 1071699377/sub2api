export default {
  trafficMonitor: {
    title: '流量监控',
    description: '基于近窗用量 max/min/avg 与群体鲁棒统计，实时列出异常用户；连续命中可高亮持续异常',
    refresh: '刷新',
    autoRefresh: '每 10 秒自动刷新',
    autoRefreshPaused: '自动刷新已暂停',
    pause: '暂停',
    resume: '继续',
    window: '观察窗口',
    windows: {
      '1min': '1 分钟',
      '5min': '5 分钟',
      '30min': '30 分钟',
      '1h': '1 小时'
    },
    persistHits: '持续次数 N',
    persistHitsHint: '连续命中 ≥ N 的用户单独高亮（约每 10 秒计 1 次）',
    sortBy: '排序',
    sort: {
      persist: '持续优先',
      zScore: 'Z 分数',
      current: '当前流量'
    },
    disabled: '实时监控未启用，请先在运维设置中开启。',
    empty: '当前窗口未发现异常候选用户',
    emptyUnconfirmedOnly: '仅有未达确认阈值的候选，可调低持续次数或继续观察',
    loading: '加载中…',
    lastUpdated: '更新于 {time}',
    persistentTitle: '持续异常',
    persistentEmpty: '暂无达到持续次数的用户',
    badgeConfirmed: '已确认',
    badgePersistent: '持续 ×{count}',
    badgeWatching: '观察中',
    deltaUp: '↑',
    deltaDown: '↓',
    deltaFlat: '→',
    summary: {
      activeUsers: '活跃用户',
      persistent: '持续异常',
      candidates: '异常候选',
      max: '最大 req/min',
      min: '最小 req/min',
      avg: '平均 req/min',
      median: '中位数',
      mad: 'MAD'
    },
    columns: {
      user: '用户',
      hits: '连续',
      current: '当前',
      max: '最大',
      min: '最小',
      avg: '平均',
      zScore: 'Z 分数',
      concurrency: '并发',
      queue: '排队',
      reasons: '原因',
      status: '状态'
    },
    reasons: {
      z_score: '相对群体异常偏高',
      near_population_max: '接近窗口峰值',
      high_concurrency: '高并发/排队'
    },
    openUsage: '查看用量'
  }
}
