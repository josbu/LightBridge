export default {
    title: '模型列表',
    description: '查看当前分组和账号目录中可用的模型、费用与使用方式',
    searchPlaceholder: '搜索模型、分组或来源...',
    empty: '暂无模型目录，请先在账号中拉取上游模型或维护模型列表',
    modelCount: '{count} 个模型',
    sourceCount: '{count} 个来源',
    sourceDetails: '来源明细',
    unknownAccount: '未知账号',
    noGroups: '未绑定分组',
    noPrice: '未配置价格',
    priceTokenRange: '输入 {input} / 输出 {output}',
    priceRequestRange: '每次 {price}',
    usageUnknown: '未标注',
    views: {
      merged: '合并',
      by_group: '按分组',
      by_channel: '按渠道',
      by_account: '按账号'
    },
    usageModes: {
      chat: '对话',
      responses: 'Responses',
      embeddings: '向量',
      image: '图片',
      audio: '音频'
    },
    // 监控状态
    setupMonitor: '设置监控',
    monitorStatus: {
      operational: '在线',
      degraded: '降级',
      failed: '故障',
      error: '异常'
    },
    quickMonitorTitle: '为 {model} 设置监控',
    quickMonitorSelectSource: '选择监控来源',
    quickMonitorSourceHint: '将使用所选来源的端点和密钥配置监控',
    quickMonitorInterval: '检测间隔',
    quickMonitorSubmit: '创建并启用',
    allGroups: '全部分组',
    selectAccountMonitor: '为 {model} 设置监控',
    selectAccountHint: '选择要监控的账号来源，系统将使用该账号的配置创建监控'
  }
