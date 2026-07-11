    // Available Channels (aggregated read-only view)
export default {
      title: '可用渠道',
      description: '按渠道聚合查看关联分组与支持模型（已展开通配符）',
      searchPlaceholder: '搜索渠道或模型...',
      columns: {
        name: '渠道名',
        status: '状态',
        billingSource: '计费模型来源',
        groups: '关联分组',
        supportedModels: '支持模型'
      },
      empty: '暂无数据',
      noGroups: '未关联分组',
      noModels: '未配置模型映射',
      noPricing: '未配置定价',
      statusActive: '启用',
      statusDisabled: '停用',
      billingSource: {
        requested: '请求模型',
        upstream: '上游模型',
        channel_mapped: '映射后模型'
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
