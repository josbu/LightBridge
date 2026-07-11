  // Available Channels (user-facing)
export default {
    title: '可用渠道',
    description: '查看您可访问的渠道与其支持的模型、定价',
    searchPlaceholder: '搜索渠道或模型...',
    empty: '暂无可用渠道',
    noModels: '未配置模型',
    noPricing: '未配置定价',
    exclusive: '专属',
    public: '公开',
    exclusiveTooltip: '管理员授权给你的专属分组',
    publicTooltip: '对所有用户公开的分组',
    columns: {
      name: '渠道名',
      description: '描述',
      platform: '平台',
      groups: '我可访问的分组',
      supportedModels: '支持模型'
    },
    pricing: {
      billingMode: '计费模式',
      billingModeToken: '按 Token',
      billingModePerRequest: '按次',
      billingModeImage: '按图片',
      inputPrice: '输入',
      outputPrice: '输出',
      cacheWritePrice: '缓存写入',
      cacheReadPrice: '缓存读取',
      imageOutputPrice: '图片输出',
      perRequestPrice: '每次请求',
      intervals: '阶梯定价',
      unitPerMillion: '/ 1M token',
      unitPerRequest: '/ 次'
    }
  }
