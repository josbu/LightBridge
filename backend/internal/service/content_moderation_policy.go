package service

import (
	"encoding/binary"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
)

func defaultContentModerationConfig() *ContentModerationConfig {
	return &ContentModerationConfig{
		Enabled:              false,
		Mode:                 ContentModerationModePreBlock,
		BaseURL:              defaultContentModerationBaseURL,
		Model:                defaultContentModerationModel,
		TimeoutMS:            defaultContentModerationTimeoutMS,
		SampleRate:           100,
		AllGroups:            true,
		GroupIDs:             []int64{},
		RecordNonHits:        false,
		Thresholds:           ContentModerationDefaultThresholds(),
		WorkerCount:          defaultContentModerationWorkerCount,
		QueueSize:            defaultContentModerationQueueSize,
		BlockStatus:          defaultContentModerationBlockHTTPStatus,
		BlockMessage:         defaultContentModerationBlockMessage,
		EmailOnHit:           true,
		AutoBanEnabled:       true,
		BanThreshold:         defaultContentModerationBanThreshold,
		ViolationWindowHours: defaultContentModerationViolationWindowHours,
		RetryCount:           defaultContentModerationRetryCount,
		HitRetentionDays:     defaultContentModerationHitRetentionDays,
		NonHitRetentionDays:  defaultContentModerationNonHitRetentionDays,
		PreHashCheckEnabled:  false,
		BlockedKeywords:      []string{},
		KeywordBlockingMode:  ContentModerationKeywordModeKeywordAndAPI,
		ModelFilter: ContentModerationModelFilter{
			Type:   ContentModerationModelFilterAll,
			Models: []string{},
		},
	}
}

func cloneContentModerationConfig(cfg *ContentModerationConfig) *ContentModerationConfig {
	if cfg == nil {
		return nil
	}
	clone := *cfg
	clone.APIKeys = append([]string(nil), cfg.APIKeys...)
	clone.GroupIDs = append([]int64(nil), cfg.GroupIDs...)
	clone.BlockedKeywords = append([]string(nil), cfg.BlockedKeywords...)
	clone.Thresholds = cloneFloatMap(cfg.Thresholds)
	clone.ModelFilter = ContentModerationModelFilter{
		Type:   cfg.ModelFilter.Type,
		Models: append([]string(nil), cfg.ModelFilter.Models...),
	}
	return &clone
}

func (cfg *ContentModerationConfig) normalize() {
	if cfg.APIKey != "" {
		cfg.APIKeys = normalizeModerationAPIKeys(append(cfg.APIKeys, cfg.APIKey))
		cfg.APIKey = ""
	} else {
		cfg.APIKeys = normalizeModerationAPIKeys(cfg.APIKeys)
	}
	if cfg.Mode == "" {
		cfg.Mode = ContentModerationModePreBlock
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultContentModerationBaseURL
	}
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if cfg.Model == "" {
		cfg.Model = defaultContentModerationModel
	}
	cfg.Model = strings.TrimSpace(cfg.Model)
	if cfg.TimeoutMS <= 0 {
		cfg.TimeoutMS = defaultContentModerationTimeoutMS
	}
	if cfg.TimeoutMS > maxContentModerationTimeoutMS {
		cfg.TimeoutMS = maxContentModerationTimeoutMS
	}
	if cfg.SampleRate < 0 {
		cfg.SampleRate = 0
	}
	if cfg.SampleRate > 100 {
		cfg.SampleRate = 100
	}
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = defaultContentModerationWorkerCount
	}
	if cfg.WorkerCount > maxContentModerationWorkerCount {
		cfg.WorkerCount = maxContentModerationWorkerCount
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = defaultContentModerationQueueSize
	}
	if cfg.QueueSize > maxContentModerationQueueSize {
		cfg.QueueSize = maxContentModerationQueueSize
	}
	if strings.TrimSpace(cfg.BlockMessage) == "" {
		cfg.BlockMessage = defaultContentModerationBlockMessage
	}
	cfg.BlockMessage = strings.TrimSpace(cfg.BlockMessage)
	if cfg.BlockStatus <= 0 {
		cfg.BlockStatus = defaultContentModerationBlockHTTPStatus
	}
	if cfg.BanThreshold <= 0 {
		cfg.BanThreshold = defaultContentModerationBanThreshold
	}
	if cfg.ViolationWindowHours <= 0 {
		cfg.ViolationWindowHours = defaultContentModerationViolationWindowHours
	}
	if cfg.RetryCount < 0 {
		cfg.RetryCount = 0
	}
	if cfg.RetryCount > maxContentModerationRetryCount {
		cfg.RetryCount = maxContentModerationRetryCount
	}
	if cfg.HitRetentionDays <= 0 {
		cfg.HitRetentionDays = defaultContentModerationHitRetentionDays
	}
	if cfg.HitRetentionDays > maxContentModerationRetentionDays {
		cfg.HitRetentionDays = maxContentModerationRetentionDays
	}
	if cfg.NonHitRetentionDays <= 0 {
		cfg.NonHitRetentionDays = defaultContentModerationNonHitRetentionDays
	}
	if cfg.NonHitRetentionDays > maxContentModerationNonHitRetentionDays {
		cfg.NonHitRetentionDays = maxContentModerationNonHitRetentionDays
	}
	cfg.GroupIDs = normalizeInt64IDs(cfg.GroupIDs)
	cfg.Thresholds = mergeContentModerationThresholds(ContentModerationDefaultThresholds(), cfg.Thresholds)
	cfg.BlockedKeywords = normalizeBlockedKeywords(cfg.BlockedKeywords)
	cfg.KeywordBlockingMode = normalizeKeywordBlockingMode(cfg.KeywordBlockingMode)
	cfg.ModelFilter = normalizeContentModerationModelFilter(cfg.ModelFilter)
}

func (cfg *ContentModerationConfig) includesGroup(groupID *int64) bool {
	if cfg.AllGroups {
		return true
	}
	if groupID == nil {
		return false
	}
	for _, id := range cfg.GroupIDs {
		if id == *groupID {
			return true
		}
	}
	return false
}

func (cfg *ContentModerationConfig) includesModel(model string) bool {
	if cfg == nil {
		return true
	}
	filter := normalizeContentModerationModelFilter(cfg.ModelFilter)
	switch filter.Type {
	case ContentModerationModelFilterInclude:
		return contentModerationModelListContains(filter.Models, model)
	case ContentModerationModelFilterExclude:
		return !contentModerationModelListContains(filter.Models, model)
	default:
		return true
	}
}

func contentModerationLogGroupID(groupID *int64) int64 {
	if groupID == nil {
		return 0
	}
	return *groupID
}

func (cfg *ContentModerationConfig) shouldSample(hashText string) bool {
	if cfg.SampleRate >= 100 {
		return true
	}
	if cfg.SampleRate <= 0 {
		return false
	}
	raw, err := hex.DecodeString(hashText)
	if err != nil || len(raw) < 2 {
		return true
	}
	return int(binary.BigEndian.Uint16(raw[:2])%100) < cfg.SampleRate
}

func (cfg *ContentModerationConfig) apiKeys() []string {
	if cfg == nil {
		return nil
	}
	return normalizeModerationAPIKeys(cfg.APIKeys)
}

func (s *ContentModerationService) nextUsableAPIKey(cfg *ContentModerationConfig) (string, bool) {
	keys := cfg.apiKeys()
	if len(keys) == 0 {
		return "", false
	}
	now := time.Now()
	for i := 0; i < len(keys); i++ {
		idx := int(s.apiKeyCursor.Add(1)-1) % len(keys)
		key := keys[idx]
		if !s.isAPIKeyFrozen(key, now) {
			return key, true
		}
	}
	return "", false
}

func (s *ContentModerationService) isAPIKeyFrozen(key string, now time.Time) bool {
	hash := moderationAPIKeyHash(key)
	if hash == "" || s == nil {
		return false
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.keyHealth[hash]
	return state != nil && state.FrozenUntil.After(now)
}

func (s *ContentModerationService) beginModerationAPIKeyCall(key string) {
	hash := moderationAPIKeyHash(key)
	if hash == "" || s == nil {
		return
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.ensureAPIKeyHealthLocked(hash, maskSecretTail(key))
	state.SyncActive++
}

func (s *ContentModerationService) finishModerationAPIKeyCall(key string, latencyMS int, success bool) {
	hash := moderationAPIKeyHash(key)
	if hash == "" || s == nil {
		return
	}
	if latencyMS < 0 {
		latencyMS = 0
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.ensureAPIKeyHealthLocked(hash, maskSecretTail(key))
	if state.SyncActive > 0 {
		state.SyncActive--
	}
	state.SyncTotal++
	state.SyncLatencyMS += int64(latencyMS)
	if success {
		state.SyncSuccess++
		return
	}
	state.SyncErrors++
}

func (s *ContentModerationService) markAPIKeySuccess(key string, latencyMS int, httpStatus int) {
	hash := moderationAPIKeyHash(key)
	if hash == "" || s == nil {
		return
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.ensureAPIKeyHealthLocked(hash, maskSecretTail(key))
	state.FailureCount = 0
	state.SuccessCount++
	state.LastError = ""
	state.LastCheckedAt = time.Now()
	state.FrozenUntil = time.Time{}
	state.LastLatencyMS = latencyMS
	state.LastHTTPStatus = httpStatus
	state.LastTested = true
}

func (s *ContentModerationService) markAPIKeyError(key string, errText string, latencyMS int, httpStatus int) {
	hash := moderationAPIKeyHash(key)
	if hash == "" || s == nil {
		return
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.ensureAPIKeyHealthLocked(hash, maskSecretTail(key))
	if contentModerationFreezeDurationForHTTPStatus(httpStatus) > 0 {
		state.FailureCount++
	}
	state.LastError = trimRunes(errText, 180)
	state.LastCheckedAt = time.Now()
	state.LastLatencyMS = latencyMS
	state.LastHTTPStatus = httpStatus
	state.LastTested = true
	if freezeDuration := contentModerationFreezeDurationForHTTPStatus(httpStatus); freezeDuration > 0 {
		state.FrozenUntil = time.Now().Add(freezeDuration)
	}
}

func contentModerationFreezeDurationForHTTPStatus(httpStatus int) time.Duration {
	switch httpStatus {
	case 0, http.StatusBadRequest:
		return 0
	case http.StatusUnauthorized, http.StatusForbidden:
		return contentModerationKeyAuthFreezeDuration
	case http.StatusTooManyRequests, 529:
		return contentModerationKeyRateLimitFreezeDuration
	default:
		return contentModerationKeyHTTPErrorFreezeDuration
	}
}
