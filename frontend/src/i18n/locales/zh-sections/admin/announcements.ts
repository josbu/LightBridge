    // Announcements
export default {
      title: '公告管理',
      description: '创建公告并按条件投放',
      createAnnouncement: '创建公告',
      editAnnouncement: '编辑公告',
      deleteAnnouncement: '删除公告',
      searchAnnouncements: '搜索公告...',
      status: '状态',
      allStatus: '全部状态',
      columns: {
        title: '标题',
        status: '状态',
        notifyMode: '通知方式',
        targeting: '展示条件',
        timeRange: '有效期',
        createdAt: '创建时间',
        actions: '操作'
      },
      statusLabels: {
        draft: '草稿',
        active: '展示中',
        archived: '已归档'
      },
      notifyModeLabels: {
        silent: '静默',
        popup: '弹窗'
      },
      form: {
        title: '标题',
        content: '内容（支持 Markdown）',
        status: '状态',
        notifyMode: '通知方式',
        notifyModeHint: '弹窗模式会自动弹出通知给用户',
        startsAt: '开始时间',
        endsAt: '结束时间',
        startsAtHint: '留空表示立即生效',
        endsAtHint: '留空表示永久生效',
        targetingMode: '展示条件',
        targetingAll: '所有用户',
        targetingCustom: '按条件',
        addOrGroup: '添加 OR 条件组',
        addAndCondition: '添加 AND 条件',
        conditionType: '条件类型',
        conditionSubscription: '订阅套餐',
        conditionBalance: '余额',
        operator: '运算符',
        balanceValue: '余额阈值',
        selectPackages: '选择套餐'
      },
      operators: {
        gt: '>',
        gte: '≥',
        lt: '<',
        lte: '≤',
        eq: '='
      },
      targetingSummaryAll: '全部用户',
      targetingSummaryCustom: '自定义（{groups} 组）',
      timeImmediate: '立即',
      timeNever: '永久',
      readStatus: '已读情况',
      eligible: '符合条件',
      readAt: '已读时间',
      unread: '未读',
      searchUsers: '搜索用户...',
      failedToLoad: '加载公告失败',
      failedToCreate: '创建公告失败',
      failedToUpdate: '更新公告失败',
      failedToDelete: '删除公告失败',
      failedToLoadReadStatus: '加载已读情况失败',
      deleteConfirm: '确定要删除该公告吗？此操作无法撤销。'
    }
