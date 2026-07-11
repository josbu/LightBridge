export default {
    title: '邀请返利',
    description: '邀请新用户注册，并将返利额度转入账户余额',
    yourCode: '我的邀请码',
    inviteLink: '邀请链接',
    copyCode: '复制邀请码',
    copyLink: '复制链接',
    codeCopied: '邀请码已复制',
    linkCopied: '邀请链接已复制',
    loadFailed: '加载邀请返利数据失败',
    transferFailed: '转入余额失败',
    stats: {
      rebateRate: '我的返利比例',
      rebateRateHint: '被邀请用户每次充值后你可获得的返利比例',
      invitedUsers: '邀请人数',
      availableQuota: '可转返利额度',
      frozenQuota: '冻结中',
      frozenQuotaHint: '新产生的返利正在冻结期中',
      totalQuota: '历史返利额度'
    },
    transfer: {
      title: '返利额度转余额',
      description: '将当前可用返利额度一键转入账户余额',
      button: '转入余额',
      transferring: '转入中...',
      empty: '当前没有可转入额度',
      success: '已转入余额：{amount}'
    },
    invitees: {
      title: '已邀请用户',
      empty: '暂无邀请记录',
      columns: {
        email: '邮箱',
        username: '用户名',
        rebate: '返利明细',
        joinedAt: '注册时间'
      }
    },
    tips: {
      title: '使用说明',
      line1: '将邀请码或邀请链接分享给新用户。',
      line2: '被邀请用户充值后，你可获得 {rate} 的返利额度。',
      line3: '返利额度可随时转入账户余额。',
      line4: '新产生的返利需要经过冻结期后才能提现。'
    }
  }
