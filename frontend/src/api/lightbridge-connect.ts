import { apiClient } from './client'
import type {
  VerifyLightBridgeConnectTokenRequest,
  VerifyLightBridgeConnectTokenResponse,
  LightBridgeConnectAlertConfig
} from '@/types'

export const lightBridgeConnectAPI = {
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
