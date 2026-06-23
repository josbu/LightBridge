import { apiClient } from './client'
import type {
  VerifyLightBridgeConnectTokenRequest,
  VerifyLightBridgeConnectTokenResponse,
  LightBridgeConnectAlertConfig
} from '@/types'

/** 单个账号的 LightBridge Connect 余额快照（账号列表批量查询用） */
export interface LightBridgeConnectBalanceItem {
  account_id: number
  instance_url: string
  balance: number // 余额（分）
  used: number // 已使用（分）
  currency: string
  last_sync_at?: string
}

export const lightBridgeConnectAPI = {
  /**
   * 批量获取若干账号已缓存的 LightBridge Connect 余额（不触发上游同步）。
   * 仅返回配置了 LightBridge Connect 的账号。
   */
  async batchBalances(accountIds: number[]): Promise<LightBridgeConnectBalanceItem[]> {
    if (accountIds.length === 0) return []
    const response = await apiClient.post<{ balances: LightBridgeConnectBalanceItem[] }>(
      '/admin/accounts/lightbridge-connect/batch-balances',
      { account_ids: accountIds }
    )
    return response.data?.balances ?? []
  },

  /**
   * 验证 LightBridge Connect 系统令牌
   */
  async verifyToken(
    accountId: number,
    data: VerifyLightBridgeConnectTokenRequest
  ): Promise<VerifyLightBridgeConnectTokenResponse> {
    const response = await apiClient.post(
      `/admin/accounts/${accountId}/lightbridge-connect/verify-token`,
      data
    )
    return response.data
  },

  /**
   * 获取账号余额（不更新数据库）
   */
  async getQuota(accountId: number): Promise<{
    balance: number
    used: number
    currency: string
    last_sync_at?: string
  }> {
    const response = await apiClient.get(`/admin/accounts/${accountId}/lightbridge-connect/quota`)
    return response.data
  },

  /**
   * 手动同步余额并更新数据库
   */
  async syncQuota(accountId: number): Promise<{
    success: boolean
    balance: number
    used: number
    currency: string
    last_sync_at?: string
    alert?: {
      type: string
      severity: string
      message: string
    }
  }> {
    const response = await apiClient.post(`/admin/accounts/${accountId}/lightbridge-connect/sync-quota`)
    return response.data
  },

  /**
   * 更新警报配置
   */
  async updateAlertConfig(
    accountId: number,
    alert: LightBridgeConnectAlertConfig
  ): Promise<{ success: boolean }> {
    const response = await apiClient.put(
      `/admin/accounts/${accountId}/lightbridge-connect/alert-config`,
      { alert }
    )
    return response.data
  }
}
