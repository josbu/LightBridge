package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"sync"
	"time"
)

// LightBridgeConnectSyncService 定期同步所有 LightBridge Connect 账号的余额
type LightBridgeConnectSyncService struct {
	lbcService *LightBridgeConnectService
	db         *sql.DB
	interval   time.Duration
	stopChan   chan struct{}
	wg         sync.WaitGroup
}

// NewLightBridgeConnectSyncService 创建同步服务
func NewLightBridgeConnectSyncService(
	lbcService *LightBridgeConnectService,
	db *sql.DB,
	interval time.Duration,
) *LightBridgeConnectSyncService {
	return &LightBridgeConnectSyncService{
		lbcService: lbcService,
		db:         db,
		interval:   interval,
		stopChan:   make(chan struct{}),
	}
}

// Start 启动定时同步任务
func (s *LightBridgeConnectSyncService) Start() {
	s.wg.Add(1)
	go s.syncLoop()
	log.Printf("[LightBridge Connect] Sync service started (interval: %v)", s.interval)
}

// Stop 停止定时同步任务
func (s *LightBridgeConnectSyncService) Stop() {
	close(s.stopChan)
	s.wg.Wait()
	log.Println("[LightBridge Connect] Sync service stopped")
}

// syncLoop 主同步循环
func (s *LightBridgeConnectSyncService) syncLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// 首次启动时立即执行一次同步
	s.syncAllAccounts()

	for {
		select {
		case <-ticker.C:
			s.syncAllAccounts()
		case <-s.stopChan:
			return
		}
	}
}

// syncAllAccounts 同步所有启用 LightBridge Connect 的账号
func (s *LightBridgeConnectSyncService) syncAllAccounts() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// 查询所有启用 LightBridge Connect 的账号
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, lightbridge_connect
		FROM accounts
		WHERE deleted_at IS NULL
		  AND status = 'active'
		  AND lightbridge_connect IS NOT NULL
		  AND lightbridge_connect::text != 'null'
	`)
	if err != nil {
		log.Printf("[LightBridge Connect] Failed to query accounts: %v", err)
		return
	}
	defer rows.Close()

	var accountCount int
	var successCount int
	var failedCount int

	for rows.Next() {
		accountCount++

		var accountID int64
		var configJSON string

		if err := rows.Scan(&accountID, &configJSON); err != nil {
			log.Printf("[LightBridge Connect] Failed to scan account: %v", err)
			continue
		}

		var config LightBridgeConnectConfig
		if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
			log.Printf("[LightBridge Connect] Failed to parse config for account %d: %v", accountID, err)
			continue
		}

		// 检查是否到达同步间隔
		if config.Quota != nil && config.Quota.LastSyncAt != nil {
			elapsed := time.Since(*config.Quota.LastSyncAt)
			if elapsed < time.Duration(config.SyncInterval)*time.Second {
				continue // 尚未到达同步间隔
			}
		}

		// 执行同步
		if s.syncAccount(ctx, accountID, &config) {
			successCount++
		} else {
			failedCount++
		}
	}

	if accountCount > 0 {
		log.Printf("[LightBridge Connect] Sync completed: %d accounts (success: %d, failed: %d)",
			accountCount, successCount, failedCount)
	}
}

// syncAccount 同步单个账号
func (s *LightBridgeConnectSyncService) syncAccount(ctx context.Context, accountID int64, config *LightBridgeConnectConfig) bool {
	// 获取旧余额
	oldBalance := int64(0)
	if config.Quota != nil {
		oldBalance = config.Quota.Balance
	}

	// 同步余额
	quotaInfo, err := s.lbcService.SyncNewAPIQuota(ctx, config)
	if err != nil {
		// 记录失败日志
		s.db.ExecContext(ctx, `
			INSERT INTO lightbridge_connect_quota_logs
			(account_id, sync_type, sync_success, error_message)
			VALUES ($1, $2, $3, $4)
		`, accountID, "auto", false, err.Error())

		log.Printf("[LightBridge Connect] Failed to sync account %d: %v", accountID, err)
		return false
	}

	// 更新配置
	config.Quota = quotaInfo
	configJSON, _ := json.Marshal(config)

	_, err = s.db.ExecContext(ctx, `
		UPDATE accounts
		SET lightbridge_connect = $1, updated_at = NOW()
		WHERE id = $2
	`, configJSON, accountID)

	if err != nil {
		log.Printf("[LightBridge Connect] Failed to update account %d: %v", accountID, err)
		return false
	}

	// 记录成功日志
	s.db.ExecContext(ctx, `
		INSERT INTO lightbridge_connect_quota_logs
		(account_id, balance_before, balance_after, change_amount, sync_type, sync_success)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, accountID, oldBalance, quotaInfo.Balance, quotaInfo.Balance-oldBalance, "auto", true)

	// 检查是否需要触发警报
	alert := s.lbcService.CheckQuotaAlert(config, oldBalance, quotaInfo.Balance)
	if alert != nil {
		// 保存警报
		s.db.ExecContext(ctx, `
			INSERT INTO lightbridge_connect_alerts
			(account_id, alert_type, severity, message, metadata)
			VALUES ($1, $2, $3, $4, $5)
		`, accountID, alert.Type, alert.Severity, alert.Message, "{}")

		// 发送警报（异步）
		go s.lbcService.SendAlert(context.Background(), accountID, config, alert)

		// 自动禁用账号（如果余额耗尽且配置了自动禁用）
		if alert.Type == "quota_exhausted" && config.Alert != nil && config.Alert.AutoDisableOnLow {
			_, err := s.db.ExecContext(ctx, `
				UPDATE accounts
				SET status = 'paused', updated_at = NOW()
				WHERE id = $1
			`, accountID)

			if err == nil {
				log.Printf("[LightBridge Connect] Account %d auto-disabled due to quota exhaustion", accountID)
			}
		}

		log.Printf("[LightBridge Connect] Alert triggered for account %d: %s (%s)",
			accountID, alert.Type, alert.Severity)
	}

	return true
}
