package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	infraerrors "github.com/WilliamWang1721/LightBridge/internal/pkg/errors"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/pagination"
)

func (s *ContentModerationService) ListLogs(ctx context.Context, filter ContentModerationLogFilter) ([]ContentModerationLog, *pagination.PaginationResult, error) {
	if filter.Pagination.Page <= 0 {
		filter.Pagination.Page = 1
	}
	if filter.Pagination.PageSize <= 0 {
		filter.Pagination.PageSize = 20
	}
	if filter.Pagination.PageSize > 100 {
		filter.Pagination.PageSize = 100
	}
	if filter.Pagination.SortOrder == "" {
		filter.Pagination.SortOrder = pagination.SortOrderDesc
	}
	return s.repo.ListLogs(ctx, filter)
}

func (s *ContentModerationService) UnbanUser(ctx context.Context, userID int64) (*ContentModerationUnbanUserResult, error) {
	if s == nil || s.userRepo == nil {
		return nil, infraerrors.InternalServer("CONTENT_MODERATION_USER_REPOSITORY_UNAVAILABLE", "用户仓储不可用")
	}
	if userID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_USER_ID", "用户 ID 无效")
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return nil, infraerrors.NotFound("USER_NOT_FOUND", "用户不存在")
		}
		return nil, fmt.Errorf("get content moderation unban user: %w", err)
	}
	if user.Status != StatusActive {
		user.Status = StatusActive
		if err := s.userRepo.Update(ctx, user); err != nil {
			return nil, fmt.Errorf("update content moderation unban user: %w", err)
		}
	}
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
	}
	return &ContentModerationUnbanUserResult{
		UserID: userID,
		Status: StatusActive,
	}, nil
}

func (s *ContentModerationService) DeleteFlaggedInputHash(ctx context.Context, inputHash string) (*ContentModerationDeleteHashResult, error) {
	inputHash = normalizeContentModerationHash(inputHash)
	if inputHash == "" {
		return nil, infraerrors.BadRequest("INVALID_CONTENT_MODERATION_HASH", "风险输入哈希无效")
	}
	if s == nil || s.hashCache == nil {
		return nil, infraerrors.InternalServer("CONTENT_MODERATION_HASH_CACHE_UNAVAILABLE", "内容审计哈希缓存不可用")
	}
	deleted, err := s.hashCache.DeleteFlaggedInputHash(ctx, inputHash)
	if err != nil {
		return nil, fmt.Errorf("delete content moderation flagged hash: %w", err)
	}
	return &ContentModerationDeleteHashResult{
		InputHash: inputHash,
		Deleted:   deleted,
	}, nil
}

func (s *ContentModerationService) ClearFlaggedInputHashes(ctx context.Context) (*ContentModerationClearHashesResult, error) {
	if s == nil || s.hashCache == nil {
		return nil, infraerrors.InternalServer("CONTENT_MODERATION_HASH_CACHE_UNAVAILABLE", "内容审计哈希缓存不可用")
	}
	deleted, err := s.hashCache.ClearFlaggedInputHashes(ctx)
	if err != nil {
		return nil, fmt.Errorf("clear content moderation flagged hashes: %w", err)
	}
	return &ContentModerationClearHashesResult{Deleted: deleted}, nil
}

func (s *ContentModerationService) GetStatus(ctx context.Context) (*ContentModerationRuntimeStatus, error) {
	if s == nil {
		return &ContentModerationRuntimeStatus{}, nil
	}
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	riskEnabled := s.isRiskControlEnabled(ctx)
	active := int(s.asyncActive.Load())
	if active < 0 {
		active = 0
	}
	if active > cfg.WorkerCount {
		active = cfg.WorkerCount
	}
	preBlockActive := int(s.preBlockActive.Load())
	if preBlockActive < 0 {
		preBlockActive = 0
	}
	preBlockChecked := s.preBlockChecked.Load()
	preBlockAvgLatency := int64(0)
	if preBlockChecked > 0 {
		preBlockAvgLatency = s.preBlockLatencyTotalMS.Load() / preBlockChecked
	}
	queueLength := 0
	if s.asyncQueue != nil {
		queueLength = len(s.asyncQueue)
	}
	queueUsage := 0.0
	if cfg.QueueSize > 0 {
		queueUsage = float64(queueLength) * 100 / float64(cfg.QueueSize)
	}
	var flaggedHashCount int64
	if s.hashCache != nil {
		if n, err := s.hashCache.CountFlaggedInputHashes(ctx); err == nil {
			flaggedHashCount = n
		} else {
			slog.Warn("content_moderation.hash_count_failed", "error", err)
		}
	}
	var lastCleanupAt *time.Time
	if unix := s.lastCleanupUnix.Load(); unix > 0 {
		t := time.Unix(unix, 0)
		lastCleanupAt = &t
	}
	return &ContentModerationRuntimeStatus{
		Enabled:                      cfg.Enabled,
		RiskControlEnabled:           riskEnabled,
		Mode:                         cfg.Mode,
		WorkerCount:                  cfg.WorkerCount,
		MaxWorkers:                   maxContentModerationWorkerCount,
		ActiveWorkers:                active,
		IdleWorkers:                  cfg.WorkerCount - active,
		QueueSize:                    cfg.QueueSize,
		QueueLength:                  queueLength,
		QueueUsagePercent:            queueUsage,
		Enqueued:                     s.asyncEnqueued.Load(),
		Dropped:                      s.asyncDropped.Load(),
		Processed:                    s.asyncProcessed.Load(),
		Errors:                       s.asyncErrors.Load(),
		PreBlockActive:               preBlockActive,
		PreBlockChecked:              preBlockChecked,
		PreBlockAllowed:              s.preBlockAllowed.Load(),
		PreBlockBlocked:              s.preBlockBlocked.Load(),
		PreBlockErrors:               s.preBlockErrors.Load(),
		PreBlockAvgLatencyMS:         preBlockAvgLatency,
		PreBlockAPIKeyActive:         s.preBlockAPIKeyActive(cfg.apiKeys()),
		PreBlockAPIKeyAvailableCount: s.preBlockAPIKeyAvailableCount(cfg.apiKeys()),
		PreBlockAPIKeyTotalCalls:     s.preBlockAPIKeyTotalCalls(cfg.apiKeys()),
		PreBlockAPIKeyLoads:          s.preBlockAPIKeyLoads(cfg.apiKeys()),
		APIKeyStatuses:               s.apiKeyStatuses(cfg.apiKeys()),
		FlaggedHashCount:             flaggedHashCount,
		LastCleanupAt:                lastCleanupAt,
		LastCleanupDeletedHit:        s.lastCleanupDeletedHit.Load(),
		LastCleanupDeletedNonHit:     s.lastCleanupDeletedNonHit.Load(),
	}, nil
}

func (s *ContentModerationService) cleanupWorker(ctx context.Context) {
	defer s.lifecycleWG.Done()
	timer := time.NewTimer(contentModerationCleanupDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		s.runCleanupOnce()
		timer.Reset(contentModerationCleanupInterval)
	}
}

func (s *ContentModerationService) runCleanupOnce() {
	if s == nil || s.repo == nil || s.settingRepo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), contentModerationCleanupTimeout)
	defer cancel()
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		slog.Warn("content_moderation.cleanup_load_config_failed", "error", err)
		return
	}
	now := time.Now()
	hitBefore := now.AddDate(0, 0, -cfg.HitRetentionDays)
	nonHitBefore := now.AddDate(0, 0, -cfg.NonHitRetentionDays)
	result, err := s.repo.CleanupExpiredLogs(ctx, hitBefore, nonHitBefore)
	if err != nil {
		slog.Warn("content_moderation.cleanup_failed", "error", err)
		return
	}
	if result == nil {
		return
	}
	s.lastCleanupUnix.Store(result.FinishedAt.Unix())
	s.lastCleanupDeletedHit.Store(result.DeletedHit)
	s.lastCleanupDeletedNonHit.Store(result.DeletedNonHit)
}

func (s *ContentModerationService) loadConfig(ctx context.Context) (*ContentModerationConfig, error) {
	cfg := defaultContentModerationConfig()
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyContentModerationConfig)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			cfg.normalize()
			return cfg, nil
		}
		return nil, fmt.Errorf("get content moderation config: %w", err)
	}
	if strings.TrimSpace(raw) == "" {
		cfg.normalize()
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(raw), cfg); err != nil {
		return nil, infraerrors.BadRequest("INVALID_CONTENT_MODERATION_CONFIG", "内容审计配置不是有效 JSON")
	}
	cfg.normalize()
	return cfg, nil
}

func (s *ContentModerationService) isRiskControlEnabled(ctx context.Context) bool {
	if s != nil && s.featureStateReady.Load() {
		return s.featureEnabled.Load()
	}
	return s.readRiskControlEnabled(ctx)
}

func (s *ContentModerationService) readRiskControlEnabled(ctx context.Context) bool {
	if s == nil || s.settingRepo == nil {
		return false
	}
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyRiskControlEnabled)
	if err != nil {
		return false
	}
	return raw == "true"
}

func (s *ContentModerationService) validateConfig(ctx context.Context, cfg *ContentModerationConfig) error {
	if cfg == nil {
		return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_CONFIG", "内容审计配置不能为空")
	}
	cfg.normalize()
	switch cfg.Mode {
	case ContentModerationModeOff, ContentModerationModeObserve, ContentModerationModePreBlock:
	default:
		return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_MODE", "内容审计模式无效")
	}
	if _, err := url.ParseRequestURI(cfg.BaseURL); err != nil {
		return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_BASE_URL", "OpenAI Base URL 无效")
	}
	if cfg.BlockStatus < 400 || cfg.BlockStatus > 599 {
		return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_BLOCK_STATUS", "拦截 HTTP 状态码必须在 400-599 之间")
	}
	if cfg.ModelFilter.Type != ContentModerationModelFilterAll && len(cfg.ModelFilter.Models) == 0 {
		return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_MODEL_FILTER", "指定或排除模型时至少需要配置 1 个模型")
	}
	if !cfg.AllGroups && len(cfg.GroupIDs) > 0 && s.groupRepo != nil {
		for _, groupID := range cfg.GroupIDs {
			if _, err := s.groupRepo.GetByIDLite(ctx, groupID); err != nil {
				return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_GROUP", fmt.Sprintf("审计分组不存在: %d", groupID))
			}
		}
	}
	return nil
}
