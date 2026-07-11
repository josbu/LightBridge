package service

import (
	"strings"
	"time"
)

type Account struct {
	ID       int64
	Name     string
	Notes    *string
	Platform string
	// SubPlatform 是同一 platform 下的子平台/账号变体判别符。
	// 目前用于在 "gemini" 平台下标识 Antigravity 账号（SubPlatform=="antigravity"）。
	// 空字符串表示原生平台账号。与 Type 正交。使用 IsAntigravity() 读取。
	SubPlatform string
	Type        string
	Credentials map[string]any
	Extra       map[string]any
	ProxyID     *int64
	Concurrency int
	Priority    int
	// RateMultiplier 账号计费倍率（>=0，允许 0 表示该账号计费为 0）。
	// 使用指针用于兼容旧版本调度缓存（Redis）中缺字段的情况：nil 表示按 1.0 处理。
	RateMultiplier     *float64
	LoadFactor         *int // 调度负载因子；nil 表示使用 Concurrency
	Status             string
	ErrorMessage       string
	LastUsedAt         *time.Time
	ExpiresAt          *time.Time
	AutoPauseOnExpired bool
	CreatedAt          time.Time
	UpdatedAt          time.Time

	Schedulable bool

	RateLimitedAt    *time.Time
	RateLimitResetAt *time.Time
	OverloadUntil    *time.Time

	TempUnschedulableUntil  *time.Time
	TempUnschedulableReason string

	SessionWindowStart  *time.Time
	SessionWindowEnd    *time.Time
	SessionWindowStatus string

	Proxy         *Proxy
	AccountGroups []AccountGroup
	GroupIDs      []int64
	Groups        []*Group

	// model_mapping 热路径缓存（非持久化字段）
	modelMappingCache               map[string]string
	modelMappingCacheReady          bool
	modelMappingCacheCredentialsPtr uintptr
	modelMappingCacheRawPtr         uintptr
	modelMappingCacheRawLen         int
	modelMappingCacheRawSig         uint64
}

type OpenAIEndpointCapability string

const (
	OpenAIEndpointCapabilityResponses       OpenAIEndpointCapability = "responses"
	OpenAIEndpointCapabilityChatCompletions OpenAIEndpointCapability = "chat_completions"
	OpenAIEndpointCapabilityEmbeddings      OpenAIEndpointCapability = "embeddings"
)

const openAIEndpointCapabilitiesCredentialKey = "openai_capabilities"

// Account.Extra 键：Claude 模型真伪检测。
// 真 Anthropic 服务端会校验 thinking block 的 signature；伪造/中转套壳通常不会。
// 主动探针(密码学级)与被动检测(SSE 旁路)都会把结论写入这些键。
const (
	AccountExtraKeyAuthenticityVerdict    = "claude_authenticity_verdict"          // genuine / counterfeit / unknown
	AccountExtraKeyAuthenticityCheckedAt  = "claude_authenticity_checked_at"       // RFC3339
	AccountExtraKeyAuthenticityMethod     = "claude_authenticity_method"           // probe / passive
	AccountExtraKeyAuthenticityDetail     = "claude_authenticity_detail"           // 人类可读说明
	AccountExtraKeyAuthenticitySuspicious = "claude_authenticity_suspicious_count" // 被动累计可疑次数(number)
)

// 真伪检测结论枚举。
const (
	AuthenticityVerdictGenuine     = "genuine"
	AuthenticityVerdictCounterfeit = "counterfeit"
	AuthenticityVerdictUnknown     = "unknown"
	AuthenticityMethodProbe        = "probe"
	AuthenticityMethodPassive      = "passive"
)

type TempUnschedulableRule struct {
	ErrorCode       int      `json:"error_code"`
	Keywords        []string `json:"keywords"`
	DurationMinutes int      `json:"duration_minutes"`
	Description     string   `json:"description"`
}

func (a *Account) IsActive() bool {
	return a.Status == StatusActive
}

// BillingRateMultiplier 返回账号计费倍率。
// - nil 表示未配置/旧缓存缺字段，按 1.0 处理
// - 允许 0，表示该账号计费为 0
// - 负数属于非法数据，出于安全考虑按 1.0 处理
func (a *Account) BillingRateMultiplier() float64 {
	if a == nil || a.RateMultiplier == nil {
		return 1.0
	}
	if *a.RateMultiplier < 0 {
		return 1.0
	}
	return *a.RateMultiplier
}

func (a *Account) EffectiveLoadFactor() int {
	if a == nil {
		return 1
	}
	if a.LoadFactor != nil && *a.LoadFactor > 0 {
		return *a.LoadFactor
	}
	if a.Concurrency > 0 {
		return a.Concurrency
	}
	return 1
}

func (a *Account) IsSchedulable() bool {
	if !a.IsActive() || !a.Schedulable {
		return false
	}
	now := time.Now()
	if a.AutoPauseOnExpired && a.ExpiresAt != nil && !now.Before(*a.ExpiresAt) {
		return false
	}
	if a.OverloadUntil != nil && now.Before(*a.OverloadUntil) {
		return false
	}
	if a.RateLimitResetAt != nil && now.Before(*a.RateLimitResetAt) {
		return false
	}
	if a.TempUnschedulableUntil != nil && now.Before(*a.TempUnschedulableUntil) {
		return false
	}
	if a.IsAPIKeyOrBedrock() && a.IsQuotaExceeded() {
		return false
	}
	return true
}

// RestrictToModelList returns whether this account should reject routing for
// models outside its persisted model list. Missing/invalid values default to
// false so unknown models continue to pass through upstream.
func (a *Account) RestrictToModelList() bool {
	if a == nil {
		return false
	}
	if a.Extra != nil {
		if v, ok := a.Extra[AccountExtraKeyRestrictToModelList].(bool); ok {
			return v
		}
	}
	if a.Credentials != nil {
		if v, ok := a.Credentials[AccountExtraKeyRestrictToModelList].(bool); ok {
			return v
		}
	}
	return false
}

// SupportedModelIDs returns the account's cached model catalog list. It accepts
// both the new extra.supported_models cache and legacy credential-side model
// lists, then falls back to exact self-mapping entries for migration safety.
func (a *Account) SupportedModelIDs() []string {
	if a == nil {
		return nil
	}
	models := stringSliceFromRaw(a.extraValue(AccountExtraKeySupportedModels))
	models = append(models, stringSliceFromRaw(a.credentialValue(AccountExtraKeySupportedModels))...)
	models = append(models, stringSliceFromRaw(a.credentialValue("models"))...)
	models = append(models, stringSliceFromRaw(a.credentialValue("model_list"))...)
	if raw, ok := a.credentialValue("model_whitelist").([]any); ok {
		models = append(models, stringSliceFromRaw(raw)...)
	}
	for from, to := range a.GetModelMapping() {
		if from == to && !strings.Contains(from, "*") {
			models = append(models, from)
		}
	}
	return normalizeModelIDs(models)
}

func (a *Account) ModelCatalogSyncState() *AccountModelSyncState {
	if a == nil || a.Extra == nil {
		return nil
	}
	raw, ok := a.Extra[AccountExtraKeyModelCatalogSync].(map[string]any)
	if !ok {
		return nil
	}
	state := &AccountModelSyncState{AccountID: a.ID}
	state.Source, _ = raw["source"].(string)
	state.Status, _ = raw["status"].(string)
	if count, ok := raw["model_count"].(float64); ok {
		state.ModelCount = int(count)
	}
	state.SyncBatchID, _ = raw["sync_batch_id"].(string)
	state.ErrorMessage, _ = raw["error_message"].(string)
	if s, _ := raw["last_synced_at"].(string); s != "" {
		if parsed, err := time.Parse(time.RFC3339, s); err == nil {
			state.LastSyncedAt = &parsed
		}
	}
	if s, _ := raw["updated_at"].(string); s != "" {
		if parsed, err := time.Parse(time.RFC3339, s); err == nil {
			state.UpdatedAt = parsed
		}
	}
	return state
}

func (a *Account) SupportsListedModel(requestedModel string) bool {
	if a == nil {
		return false
	}
	model := strings.TrimSpace(requestedModel)
	if model == "" {
		return true
	}
	models := a.SupportedModelIDs()
	if len(models) == 0 {
		return true
	}
	normalized := normalizeRequestedModelForLookup(a.Platform, model)
	for _, candidate := range models {
		if strings.EqualFold(candidate, model) || strings.EqualFold(candidate, normalized) {
			return true
		}
		if matchWildcard(candidate, model) || (normalized != model && matchWildcard(candidate, normalized)) {
			return true
		}
	}
	return false
}

func (a *Account) extraValue(key string) any {
	if a == nil || a.Extra == nil {
		return nil
	}
	return a.Extra[key]
}

func (a *Account) credentialValue(key string) any {
	if a == nil || a.Credentials == nil {
		return nil
	}
	return a.Credentials[key]
}
