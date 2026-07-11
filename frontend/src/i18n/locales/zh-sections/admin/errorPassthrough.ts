    // Error Passthrough Rules
export default {
      title: '错误透传规则',
      description: '配置上游错误如何返回给客户端',
      createRule: '创建规则',
      editRule: '编辑规则',
      deleteRule: '删除规则',
      noRules: '暂无规则',
      createFirstRule: '创建第一条错误透传规则',
      allPlatforms: '所有平台',
      passthrough: '透传',
      custom: '自定义',
      code: '状态码',
      body: '消息体',
      skipMonitoring: '跳过监控',

      // Columns
      columns: {
        priority: '优先级',
        name: '名称',
        conditions: '匹配条件',
        platforms: '平台',
        behavior: '响应行为',
        status: '状态',
        actions: '操作'
      },

      // Match Mode
      matchMode: {
        any: '错误码 或 关键词',
        all: '错误码 且 关键词',
        anyHint: '状态码匹配任一错误码，或消息包含任一关键词',
        allHint: '状态码匹配任一错误码，且消息包含任一关键词'
      },

      // Form
      form: {
        name: '规则名称',
        namePlaceholder: '例如：上下文超限透传',
        priority: '优先级',
        priorityHint: '数值越小优先级越高，优先匹配',
        description: '规则描述',
        descriptionPlaceholder: '描述此规则的用途...',
        matchConditions: '匹配条件',
        errorCodes: '错误码',
        errorCodesPlaceholder: '422, 400, 429',
        errorCodesHint: '多个错误码用逗号分隔',
        keywords: '关键词',
        keywordsPlaceholder: '每行一个关键词\ncontext limit\nmodel not supported',
        keywordsHint: '每行一个关键词，不区分大小写',
        matchMode: '匹配模式',
        platforms: '适用平台',
        platformsHint: '不选择表示适用于所有平台',
        responseBehavior: '响应行为',
        passthroughCode: '透传上游状态码',
        responseCode: '自定义状态码',
        passthroughBody: '透传上游错误信息',
        customMessage: '自定义错误信息',
        customMessagePlaceholder: '返回给客户端的错误信息...',
        skipMonitoring: '跳过运维监控记录',
        skipMonitoringHint: '开启后，匹配此规则的错误不会被记录到运维监控中',
        enabled: '启用此规则'
      },

      // Messages
      nameRequired: '请输入规则名称',
      conditionsRequired: '请至少配置一个错误码或关键词',
      ruleCreated: '规则创建成功',
      ruleUpdated: '规则更新成功',
      ruleDeleted: '规则删除成功',
      deleteConfirm: '确定要删除规则 "{name}" 吗？',
      failedToLoad: '加载规则失败',
      failedToSave: '保存规则失败',
      failedToDelete: '删除规则失败',
      failedToToggle: '切换状态失败'
    }
