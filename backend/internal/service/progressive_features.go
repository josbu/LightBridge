package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/WilliamWang1721/LightBridge/internal/config"
	infraerrors "github.com/WilliamWang1721/LightBridge/internal/pkg/errors"
)

// ProgressiveFeature is a stable identifier shared by backend route guards,
// background-runtime registration and the frontend feature manifest.
type ProgressiveFeature string

const (
	// Core features are deliberately present in the catalog even though they are
	// not switchable. This makes the core/optional boundary explicit and prevents
	// future resource-profile work from accidentally disabling request-critical
	// services.
	ProgressiveFeatureCoreAuth         ProgressiveFeature = "core_auth"
	ProgressiveFeatureCoreGateway      ProgressiveFeature = "core_gateway"
	ProgressiveFeatureCoreBilling      ProgressiveFeature = "core_billing"
	ProgressiveFeatureCoreTokenRefresh ProgressiveFeature = "core_token_refresh"

	ProgressiveFeatureChannelMonitor    ProgressiveFeature = "channel_monitor"
	ProgressiveFeatureAvailableChannels ProgressiveFeature = "available_channels"
	ProgressiveFeaturePayment           ProgressiveFeature = "payment"
	ProgressiveFeatureRiskControl       ProgressiveFeature = "risk_control"
	ProgressiveFeaturePrivacyFilter     ProgressiveFeature = "privacy_filter"
	ProgressiveFeatureAffiliate         ProgressiveFeature = "affiliate"
	ProgressiveFeatureAnnouncements     ProgressiveFeature = "announcements"
	ProgressiveFeatureRedeem            ProgressiveFeature = "redeem"
	ProgressiveFeaturePromo             ProgressiveFeature = "promo"
	ProgressiveFeatureProxies           ProgressiveFeature = "proxies"
	ProgressiveFeatureChannelPricing    ProgressiveFeature = "channel_pricing"
	ProgressiveFeatureSubscriptions     ProgressiveFeature = "subscriptions"

	ProgressiveFeatureOpsMonitoring        ProgressiveFeature = "ops_monitoring"
	ProgressiveFeatureDashboardAggregation ProgressiveFeature = "dashboard_aggregation"
	ProgressiveFeatureUsageCleanup         ProgressiveFeature = "usage_cleanup"
	ProgressiveFeatureScheduledTests       ProgressiveFeature = "scheduled_tests"
	ProgressiveFeatureBackup               ProgressiveFeature = "backup"
	ProgressiveFeatureModuleRuntime        ProgressiveFeature = "module_runtime"
	ProgressiveFeatureLightBridgeConnect   ProgressiveFeature = "lightbridge_connect"
)

type ProgressiveFeatureTier string

const (
	ProgressiveFeatureTierCore      ProgressiveFeatureTier = "core"
	ProgressiveFeatureTierOptional  ProgressiveFeatureTier = "optional"
	ProgressiveFeatureTierExtension ProgressiveFeatureTier = "extension"
)

type ProgressiveFeatureActivation string

const (
	// Eager components are part of the core request path and are always active.
	ProgressiveFeatureActivationEager ProgressiveFeatureActivation = "eager"
	// Dynamic components may be started and paused after a settings update.
	ProgressiveFeatureActivationDynamic ProgressiveFeatureActivation = "dynamic"
	// Boot components are selected once at process startup and require a restart
	// when their eligibility changes.
	ProgressiveFeatureActivationBoot ProgressiveFeatureActivation = "boot"
	// On-demand components allocate resources only while an explicit operation is
	// running; the feature catalog controls their routes and UI contribution.
	ProgressiveFeatureActivationOnDemand ProgressiveFeatureActivation = "on_demand"
)

type ProgressiveFeatureSurface string

const (
	ProgressiveFeatureSurfaceBackendRoute  ProgressiveFeatureSurface = "backend_route"
	ProgressiveFeatureSurfaceFrontendRoute ProgressiveFeatureSurface = "frontend_route"
	ProgressiveFeatureSurfaceMenu          ProgressiveFeatureSurface = "menu"
	ProgressiveFeatureSurfaceWorker        ProgressiveFeatureSurface = "worker"
	ProgressiveFeatureSurfaceModuleRuntime ProgressiveFeatureSurface = "module_runtime"
)

// ProgressiveFeatureDefinition is the single backend source of truth for
// progressive registration. hardEnabled is intentionally not serialized; it
// expresses process-level prerequisites such as ops.enabled.
type ProgressiveFeatureDefinition struct {
	ID               ProgressiveFeature
	SettingKey       string
	DefaultEnabled   bool
	DistributionOnly bool
	Label            string
	Tier             ProgressiveFeatureTier
	Activation       ProgressiveFeatureActivation
	MinimumProfile   config.FeatureProfile
	Dependencies     []ProgressiveFeature
	Surfaces         []ProgressiveFeatureSurface
	hardEnabled      func(*config.Config) bool
}

// ProgressiveFeatureState is safe for public bootstrap responses. It contains
// no component errors, file paths or other runtime diagnostics.
type ProgressiveFeatureState struct {
	ID                ProgressiveFeature           `json:"id"`
	Enabled           bool                         `json:"enabled"`
	ConfiguredEnabled bool                         `json:"configuredEnabled"`
	RequiresRestart   bool                         `json:"requiresRestart,omitempty"`
	Reason            string                       `json:"reason"`
	Tier              ProgressiveFeatureTier       `json:"tier"`
	Activation        ProgressiveFeatureActivation `json:"activation"`
	MinimumProfile    config.FeatureProfile        `json:"minimumProfile"`
	Surfaces          []ProgressiveFeatureSurface  `json:"surfaces,omitempty"`
}

const progressiveFeatureOverridePrefix = "progressive_feature_override."

// ProgressiveFeatureControlState is the administrator control-plane view of a
// registered feature. Override is nil when the feature follows its profile,
// process configuration and legacy setting.
type ProgressiveFeatureControlState struct {
	ID                ProgressiveFeature              `json:"id"`
	Label             string                          `json:"label"`
	Tier              ProgressiveFeatureTier          `json:"tier"`
	Activation        ProgressiveFeatureActivation    `json:"activation"`
	Enabled           bool                            `json:"enabled"`
	ConfiguredEnabled bool                            `json:"configuredEnabled"`
	Available         bool                            `json:"available"`
	Controllable      bool                            `json:"controllable"`
	Override          *bool                           `json:"override"`
	RequiresRestart   bool                            `json:"requiresRestart"`
	Reason            string                          `json:"reason"`
	MinimumProfile    config.FeatureProfile           `json:"minimumProfile"`
	Dependencies      []ProgressiveFeature            `json:"dependencies"`
	Surfaces          []ProgressiveFeatureSurface     `json:"surfaces"`
	RuntimeComponents []FeatureRuntimeComponentStatus `json:"runtimeComponents"`
}

type ProgressiveFeatureControlOverview struct {
	Profile  config.FeatureProfile            `json:"profile"`
	Features []ProgressiveFeatureControlState `json:"features"`
}

func progressiveFeatureOverrideKey(id ProgressiveFeature) string {
	return progressiveFeatureOverridePrefix + string(id)
}

type progressiveFeatureSnapshot struct {
	states    map[ProgressiveFeature]ProgressiveFeatureState
	ordered   []ProgressiveFeatureState
	expiresAt int64
}

const progressiveFeatureSnapshotTTL = 5 * time.Second
const progressiveFeatureSnapshotKey = "progressive_feature_snapshot"

func coreFeature(id ProgressiveFeature, label string) ProgressiveFeatureDefinition {
	return ProgressiveFeatureDefinition{
		ID:             id,
		DefaultEnabled: true,
		Label:          label,
		Tier:           ProgressiveFeatureTierCore,
		Activation:     ProgressiveFeatureActivationEager,
		MinimumProfile: config.FeatureProfileMinimal,
	}
}

func optionalFeature(
	id ProgressiveFeature,
	label string,
	settingKey string,
	defaultEnabled bool,
	distributionOnly bool,
	surfaces ...ProgressiveFeatureSurface,
) ProgressiveFeatureDefinition {
	return ProgressiveFeatureDefinition{
		ID:               id,
		SettingKey:       settingKey,
		DefaultEnabled:   defaultEnabled,
		DistributionOnly: distributionOnly,
		Label:            label,
		Tier:             ProgressiveFeatureTierOptional,
		Activation:       ProgressiveFeatureActivationDynamic,
		MinimumProfile:   config.FeatureProfileMinimal,
		Surfaces:         surfaces,
	}
}

var progressiveFeatureDefinitions = map[ProgressiveFeature]ProgressiveFeatureDefinition{
	ProgressiveFeatureCoreAuth:         coreFeature(ProgressiveFeatureCoreAuth, "authentication"),
	ProgressiveFeatureCoreGateway:      coreFeature(ProgressiveFeatureCoreGateway, "gateway"),
	ProgressiveFeatureCoreBilling:      coreFeature(ProgressiveFeatureCoreBilling, "billing"),
	ProgressiveFeatureCoreTokenRefresh: coreFeature(ProgressiveFeatureCoreTokenRefresh, "oauth token refresh"),

	ProgressiveFeatureChannelMonitor: optionalFeature(
		ProgressiveFeatureChannelMonitor, "channel monitor", SettingKeyChannelMonitorEnabled, true, false,
		ProgressiveFeatureSurfaceBackendRoute, ProgressiveFeatureSurfaceFrontendRoute, ProgressiveFeatureSurfaceMenu, ProgressiveFeatureSurfaceWorker,
	),
	ProgressiveFeatureAvailableChannels: optionalFeature(
		ProgressiveFeatureAvailableChannels, "available channels", SettingKeyAvailableChannelsEnabled, false, false,
		ProgressiveFeatureSurfaceBackendRoute, ProgressiveFeatureSurfaceFrontendRoute, ProgressiveFeatureSurfaceMenu,
	),
	ProgressiveFeaturePayment: optionalFeature(
		ProgressiveFeaturePayment, "payment", SettingPaymentEnabled, false, false,
		ProgressiveFeatureSurfaceBackendRoute, ProgressiveFeatureSurfaceFrontendRoute, ProgressiveFeatureSurfaceMenu, ProgressiveFeatureSurfaceWorker,
	),
	ProgressiveFeatureRiskControl: optionalFeature(
		ProgressiveFeatureRiskControl, "risk control", SettingKeyRiskControlEnabled, false, true,
		ProgressiveFeatureSurfaceBackendRoute, ProgressiveFeatureSurfaceFrontendRoute, ProgressiveFeatureSurfaceMenu, ProgressiveFeatureSurfaceWorker,
	),
	ProgressiveFeaturePrivacyFilter: optionalFeature(
		ProgressiveFeaturePrivacyFilter, "privacy filter", SettingKeyPrivacyFilterEnabled, false, false,
		ProgressiveFeatureSurfaceBackendRoute, ProgressiveFeatureSurfaceFrontendRoute, ProgressiveFeatureSurfaceMenu,
	),
	ProgressiveFeatureAffiliate: optionalFeature(
		ProgressiveFeatureAffiliate, "affiliate", SettingKeyAffiliateEnabled, false, false,
		ProgressiveFeatureSurfaceBackendRoute, ProgressiveFeatureSurfaceFrontendRoute, ProgressiveFeatureSurfaceMenu,
	),
	ProgressiveFeatureAnnouncements: optionalFeature(
		ProgressiveFeatureAnnouncements, "announcements", SettingKeyAnnouncementsEnabled, true, true,
		ProgressiveFeatureSurfaceBackendRoute, ProgressiveFeatureSurfaceFrontendRoute, ProgressiveFeatureSurfaceMenu,
	),
	ProgressiveFeatureRedeem: optionalFeature(
		ProgressiveFeatureRedeem, "redeem", SettingKeyRedeemEnabled, true, true,
		ProgressiveFeatureSurfaceBackendRoute, ProgressiveFeatureSurfaceFrontendRoute, ProgressiveFeatureSurfaceMenu,
	),
	ProgressiveFeaturePromo: optionalFeature(
		ProgressiveFeaturePromo, "promo", SettingKeyPromoCodeEnabled, true, true,
		ProgressiveFeatureSurfaceBackendRoute, ProgressiveFeatureSurfaceFrontendRoute, ProgressiveFeatureSurfaceMenu,
	),
	ProgressiveFeatureProxies: optionalFeature(
		ProgressiveFeatureProxies, "proxies", SettingKeyProxiesEnabled, true, false,
		ProgressiveFeatureSurfaceBackendRoute, ProgressiveFeatureSurfaceFrontendRoute, ProgressiveFeatureSurfaceMenu,
	),
	ProgressiveFeatureChannelPricing: optionalFeature(
		ProgressiveFeatureChannelPricing, "channel pricing", SettingKeyChannelPricingEnabled, true, false,
		ProgressiveFeatureSurfaceBackendRoute, ProgressiveFeatureSurfaceFrontendRoute, ProgressiveFeatureSurfaceMenu,
	),
	ProgressiveFeatureSubscriptions: optionalFeature(
		ProgressiveFeatureSubscriptions, "subscriptions", "", true, true,
		ProgressiveFeatureSurfaceBackendRoute, ProgressiveFeatureSurfaceFrontendRoute, ProgressiveFeatureSurfaceMenu,
	),

	ProgressiveFeatureOpsMonitoring: {
		ID:             ProgressiveFeatureOpsMonitoring,
		SettingKey:     SettingKeyOpsMonitoringEnabled,
		DefaultEnabled: true,
		Label:          "ops monitoring",
		Tier:           ProgressiveFeatureTierOptional,
		Activation:     ProgressiveFeatureActivationBoot,
		MinimumProfile: config.FeatureProfileStandard,
		Surfaces: []ProgressiveFeatureSurface{
			ProgressiveFeatureSurfaceBackendRoute,
			ProgressiveFeatureSurfaceFrontendRoute,
			ProgressiveFeatureSurfaceMenu,
			ProgressiveFeatureSurfaceWorker,
		},
		hardEnabled: func(cfg *config.Config) bool { return cfg == nil || cfg.Ops.Enabled },
	},
	ProgressiveFeatureDashboardAggregation: {
		ID:             ProgressiveFeatureDashboardAggregation,
		DefaultEnabled: true,
		Label:          "dashboard aggregation",
		Tier:           ProgressiveFeatureTierOptional,
		Activation:     ProgressiveFeatureActivationBoot,
		MinimumProfile: config.FeatureProfileStandard,
		Surfaces:       []ProgressiveFeatureSurface{ProgressiveFeatureSurfaceWorker},
		hardEnabled:    func(cfg *config.Config) bool { return cfg == nil || cfg.DashboardAgg.Enabled },
	},
	ProgressiveFeatureUsageCleanup: {
		ID:             ProgressiveFeatureUsageCleanup,
		DefaultEnabled: true,
		Label:          "usage cleanup",
		Tier:           ProgressiveFeatureTierOptional,
		Activation:     ProgressiveFeatureActivationBoot,
		MinimumProfile: config.FeatureProfileStandard,
		Surfaces:       []ProgressiveFeatureSurface{ProgressiveFeatureSurfaceWorker},
		hardEnabled:    func(cfg *config.Config) bool { return cfg == nil || cfg.UsageCleanup.Enabled },
	},
	ProgressiveFeatureScheduledTests: {
		ID:             ProgressiveFeatureScheduledTests,
		DefaultEnabled: true,
		Label:          "scheduled tests",
		Tier:           ProgressiveFeatureTierOptional,
		Activation:     ProgressiveFeatureActivationBoot,
		MinimumProfile: config.FeatureProfileStandard,
		Surfaces: []ProgressiveFeatureSurface{
			ProgressiveFeatureSurfaceBackendRoute,
			ProgressiveFeatureSurfaceFrontendRoute,
			ProgressiveFeatureSurfaceMenu,
			ProgressiveFeatureSurfaceWorker,
		},
	},
	ProgressiveFeatureBackup: {
		ID:             ProgressiveFeatureBackup,
		DefaultEnabled: true,
		Label:          "backup",
		Tier:           ProgressiveFeatureTierOptional,
		Activation:     ProgressiveFeatureActivationBoot,
		MinimumProfile: config.FeatureProfileStandard,
		Surfaces: []ProgressiveFeatureSurface{
			ProgressiveFeatureSurfaceBackendRoute,
			ProgressiveFeatureSurfaceFrontendRoute,
			ProgressiveFeatureSurfaceMenu,
			ProgressiveFeatureSurfaceWorker,
		},
	},
	ProgressiveFeatureModuleRuntime: {
		ID:             ProgressiveFeatureModuleRuntime,
		DefaultEnabled: true,
		Label:          "module runtime",
		Tier:           ProgressiveFeatureTierExtension,
		Activation:     ProgressiveFeatureActivationBoot,
		MinimumProfile: config.FeatureProfileFull,
		Surfaces: []ProgressiveFeatureSurface{
			ProgressiveFeatureSurfaceBackendRoute,
			ProgressiveFeatureSurfaceFrontendRoute,
			ProgressiveFeatureSurfaceMenu,
			ProgressiveFeatureSurfaceModuleRuntime,
		},
	},
	ProgressiveFeatureLightBridgeConnect: {
		ID:             ProgressiveFeatureLightBridgeConnect,
		DefaultEnabled: true,
		Label:          "lightbridge connect",
		Tier:           ProgressiveFeatureTierExtension,
		Activation:     ProgressiveFeatureActivationBoot,
		MinimumProfile: config.FeatureProfileFull,
		Surfaces:       []ProgressiveFeatureSurface{ProgressiveFeatureSurfaceWorker},
	},
}

func ProgressiveFeatureDefinitionFor(feature ProgressiveFeature) (ProgressiveFeatureDefinition, bool) {
	def, ok := progressiveFeatureDefinitions[feature]
	return def, ok
}

func ProgressiveFeatureDefinitions() []ProgressiveFeatureDefinition {
	defs := make([]ProgressiveFeatureDefinition, 0, len(progressiveFeatureDefinitions))
	for _, def := range progressiveFeatureDefinitions {
		defs = append(defs, cloneProgressiveFeatureDefinition(def))
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	return defs
}

func cloneProgressiveFeatureDefinition(def ProgressiveFeatureDefinition) ProgressiveFeatureDefinition {
	def.Dependencies = append([]ProgressiveFeature(nil), def.Dependencies...)
	def.Surfaces = append([]ProgressiveFeatureSurface(nil), def.Surfaces...)
	return def
}

// ValidateProgressiveFeatureConfig rejects unknown overrides and attempts to
// disable core features. It is called during runtime-manager construction so
// configuration mistakes fail application initialization instead of being
// silently ignored.
func ValidateProgressiveFeatureConfig(cfg *config.Config) error {
	if cfg == nil {
		return nil
	}
	for rawID, enabled := range cfg.Features.Overrides {
		id := ProgressiveFeature(strings.TrimSpace(rawID))
		def, ok := progressiveFeatureDefinitions[id]
		if !ok {
			return fmt.Errorf("features.overrides contains unknown feature %q", rawID)
		}
		if def.Tier == ProgressiveFeatureTierCore && !enabled {
			return fmt.Errorf("core feature %q cannot be disabled", rawID)
		}
	}
	return nil
}

func normalizedFeatureProfile(cfg *config.Config) config.FeatureProfile {
	if cfg == nil || cfg.Features.Profile == "" {
		return config.FeatureProfileFull
	}
	return cfg.Features.Profile
}

func featureProfileRank(profile config.FeatureProfile) int {
	switch profile {
	case config.FeatureProfileMinimal:
		return 0
	case config.FeatureProfileStandard:
		return 1
	case config.FeatureProfileFull:
		return 2
	default:
		return 2
	}
}

func (s *SettingService) IsBooleanSettingEnabled(ctx context.Context, key string, defaultEnabled bool) bool {
	if s == nil || s.settingRepo == nil || key == "" {
		return defaultEnabled
	}
	value, err := s.settingRepo.GetValue(ctx, key)
	if err != nil {
		return defaultEnabled
	}
	if defaultEnabled {
		return !isFalseSettingValue(value)
	}
	return value == "true"
}

func (s *SettingService) IsProgressiveFeatureEnabled(ctx context.Context, feature ProgressiveFeature) bool {
	state, ok := s.ProgressiveFeatureState(ctx, feature)
	return ok && state.Enabled
}

// SetProgressiveFeatureAvailabilityResolver installs the process-lifetime
// availability overlay owned by FeatureRuntimeManager. It is intentionally
// separate from the cached configured snapshot so setting changes can be shown
// as restart-required without exposing routes whose boot component is absent.
func (s *SettingService) SetProgressiveFeatureAvailabilityResolver(
	resolver func(ProgressiveFeature) (bool, bool),
) {
	if s == nil {
		return
	}
	s.progressiveFeatureAvailabilityMu.Lock()
	s.progressiveFeatureAvailabilityResolver = resolver
	s.progressiveFeatureAvailabilityMu.Unlock()
}

func (s *SettingService) applyProgressiveFeatureAvailability(state ProgressiveFeatureState) ProgressiveFeatureState {
	if s == nil || state.Activation != ProgressiveFeatureActivationBoot {
		return state
	}
	s.progressiveFeatureAvailabilityMu.RLock()
	resolver := s.progressiveFeatureAvailabilityResolver
	s.progressiveFeatureAvailabilityMu.RUnlock()
	if resolver == nil {
		return state
	}
	active, known := resolver(state.ID)
	if !known {
		return state
	}
	configured := state.ConfiguredEnabled
	state.Enabled = active
	state.RequiresRestart = active != configured
	if state.RequiresRestart {
		if configured {
			state.Reason = "restart_required_enable"
		} else {
			state.Reason = "restart_required_disable"
		}
	} else if active {
		state.Reason = "enabled"
	}
	return state
}

func (s *SettingService) ProgressiveFeatureState(ctx context.Context, feature ProgressiveFeature) (ProgressiveFeatureState, bool) {
	snapshot := s.progressiveFeatureSnapshot(ctx)
	if snapshot == nil {
		return ProgressiveFeatureState{}, false
	}
	state, ok := snapshot.states[feature]
	if !ok {
		return ProgressiveFeatureState{}, false
	}
	return s.applyProgressiveFeatureAvailability(state), true
}

func (s *SettingService) ProgressiveFeatureManifest(ctx context.Context) []ProgressiveFeatureState {
	snapshot := s.progressiveFeatureSnapshot(ctx)
	if snapshot == nil {
		return nil
	}
	result := make([]ProgressiveFeatureState, len(snapshot.ordered))
	for i, state := range snapshot.ordered {
		state.Surfaces = append([]ProgressiveFeatureSurface(nil), state.Surfaces...)
		result[i] = s.applyProgressiveFeatureAvailability(state)
	}
	return result
}

func (s *SettingService) InvalidateProgressiveFeatureSnapshot() {
	if s == nil {
		return
	}
	s.progressiveFeatureSnapshotCache.Store((*progressiveFeatureSnapshot)(nil))
	s.progressiveFeatureSnapshotSF.Forget(progressiveFeatureSnapshotKey)
}

func (s *SettingService) progressiveFeatureSnapshot(ctx context.Context) *progressiveFeatureSnapshot {
	if s == nil {
		return buildProgressiveFeatureSnapshot(nil, nil, nil)
	}
	if cached, _ := s.progressiveFeatureSnapshotCache.Load().(*progressiveFeatureSnapshot); cached != nil && time.Now().UnixNano() < cached.expiresAt {
		return cached
	}

	value, _, _ := s.progressiveFeatureSnapshotSF.Do(progressiveFeatureSnapshotKey, func() (any, error) {
		if cached, _ := s.progressiveFeatureSnapshotCache.Load().(*progressiveFeatureSnapshot); cached != nil && time.Now().UnixNano() < cached.expiresAt {
			return cached, nil
		}

		keys := progressiveFeatureSettingKeys()
		values := map[string]string{}
		cacheable := true
		if s.settingRepo != nil && len(keys) > 0 {
			loaded, err := s.settingRepo.GetMultiple(ctx, keys)
			if err != nil {
				cacheable = false
			} else {
				values = loaded
			}
		}
		snapshot := buildProgressiveFeatureSnapshot(s.cfg, values, progressiveFeatureDefinitions)
		if cacheable {
			s.progressiveFeatureSnapshotCache.Store(snapshot)
		}
		return snapshot, nil
	})
	snapshot, _ := value.(*progressiveFeatureSnapshot)
	return snapshot
}

func progressiveFeatureSettingKeys() []string {
	set := map[string]struct{}{SettingKeyDeploymentMode: {}}
	for _, def := range progressiveFeatureDefinitions {
		if def.SettingKey != "" {
			set[def.SettingKey] = struct{}{}
		}
		if def.Tier != ProgressiveFeatureTierCore {
			set[progressiveFeatureOverrideKey(def.ID)] = struct{}{}
		}
	}
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func buildProgressiveFeatureSnapshot(
	cfg *config.Config,
	values map[string]string,
	definitions map[ProgressiveFeature]ProgressiveFeatureDefinition,
) *progressiveFeatureSnapshot {
	if definitions == nil {
		definitions = progressiveFeatureDefinitions
	}
	profile := normalizedFeatureProfile(cfg)
	deploymentMode := DeploymentModeDistribution
	if raw := strings.TrimSpace(values[SettingKeyDeploymentMode]); raw != "" {
		deploymentMode = NormalizeDeploymentMode(raw)
	}

	states := make(map[ProgressiveFeature]ProgressiveFeatureState, len(definitions))
	visiting := make(map[ProgressiveFeature]bool, len(definitions))
	var evaluate func(ProgressiveFeature) ProgressiveFeatureState
	evaluate = func(id ProgressiveFeature) ProgressiveFeatureState {
		if state, ok := states[id]; ok {
			return state
		}
		def, ok := definitions[id]
		if !ok {
			return ProgressiveFeatureState{ID: id, Enabled: false, Reason: "unknown_feature"}
		}
		state := ProgressiveFeatureState{
			ID:             id,
			Tier:           def.Tier,
			Activation:     def.Activation,
			MinimumProfile: def.MinimumProfile,
			Surfaces:       append([]ProgressiveFeatureSurface(nil), def.Surfaces...),
		}
		if def.Tier == ProgressiveFeatureTierCore {
			state.Enabled = true
			state.Reason = "core"
			states[id] = state
			return state
		}
		if visiting[id] {
			state.Reason = "dependency_cycle"
			states[id] = state
			return state
		}
		visiting[id] = true
		defer delete(visiting, id)

		if def.hardEnabled != nil && !def.hardEnabled(cfg) {
			state.Reason = "hard_disabled"
			states[id] = state
			return state
		}
		dbOverride, hasDBOverride := parseProgressiveFeatureOverride(values[progressiveFeatureOverrideKey(id)])
		if hasDBOverride && !dbOverride {
			state.Reason = "control_override_disabled"
			states[id] = state
			return state
		}
		if hasDBOverride {
			state.Enabled = true
			state.Reason = "control_override_enabled"
		} else if cfg != nil {
			if override, exists := cfg.Features.Overrides[string(id)]; exists {
				if !override {
					state.Reason = "override_disabled"
					states[id] = state
					return state
				}
				// Explicit enable overrides the selected resource profile, but not a
				// hard process-level prerequisite.
				state.Enabled = true
				state.Reason = "override_enabled"
			}
		}
		if !hasDBOverride && !state.Enabled && featureProfileRank(profile) < featureProfileRank(def.MinimumProfile) {
			state.Reason = "profile"
			states[id] = state
			return state
		}
		if def.DistributionOnly && deploymentMode == DeploymentModePersonal {
			state.Enabled = false
			state.Reason = "personal_mode"
			states[id] = state
			return state
		}
		if !hasDBOverride && def.SettingKey != "" {
			raw, exists := values[def.SettingKey]
			enabled := def.DefaultEnabled
			if exists {
				if def.DefaultEnabled {
					enabled = !isFalseSettingValue(raw)
				} else {
					enabled = raw == "true"
				}
			}
			if !enabled {
				state.Enabled = false
				state.Reason = "setting_disabled"
				states[id] = state
				return state
			}
		}
		for _, dependency := range def.Dependencies {
			if !evaluate(dependency).Enabled {
				state.Enabled = false
				state.Reason = "dependency_disabled"
				states[id] = state
				return state
			}
		}
		state.Enabled = true
		if state.Reason == "" {
			state.Reason = "enabled"
		}
		states[id] = state
		return state
	}

	orderedIDs := make([]ProgressiveFeature, 0, len(definitions))
	for id := range definitions {
		orderedIDs = append(orderedIDs, id)
	}
	sort.Slice(orderedIDs, func(i, j int) bool { return orderedIDs[i] < orderedIDs[j] })
	ordered := make([]ProgressiveFeatureState, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		state := evaluate(id)
		state.ConfiguredEnabled = state.Enabled
		states[id] = state
		ordered = append(ordered, state)
	}
	return &progressiveFeatureSnapshot{
		states:    states,
		ordered:   ordered,
		expiresAt: time.Now().Add(progressiveFeatureSnapshotTTL).UnixNano(),
	}
}

func parseProgressiveFeatureOverride(raw string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true":
		return true, true
	case "false":
		return false, true
	default:
		return false, false
	}
}

// progressiveFeatureRepositoryOverride lets services that own request-path
// behavior consume the same explicit control-plane decision as route guards
// and lifecycle components. When no override exists, callers retain their
// legacy setting/default behavior.
func progressiveFeatureRepositoryOverride(
	ctx context.Context,
	repo SettingRepository,
	id ProgressiveFeature,
) (bool, bool) {
	if repo == nil {
		return false, false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	raw, err := repo.GetValue(ctx, progressiveFeatureOverrideKey(id))
	if err != nil {
		return false, false
	}
	return parseProgressiveFeatureOverride(raw)
}

// ProgressiveFeatureControlOverview returns the complete administrator view.
// Runtime diagnostics are supplied by the lifecycle manager so the registry
// remains usable in tests and lightweight deployments without that manager.
func (s *SettingService) ProgressiveFeatureControlOverview(
	ctx context.Context,
	runtime []FeatureRuntimeComponentStatus,
) ProgressiveFeatureControlOverview {
	manifest := s.ProgressiveFeatureManifest(ctx)
	values := map[string]string{}
	if s != nil && s.settingRepo != nil {
		keys := make([]string, 0, len(progressiveFeatureDefinitions))
		for _, def := range progressiveFeatureDefinitions {
			if def.Tier != ProgressiveFeatureTierCore {
				keys = append(keys, progressiveFeatureOverrideKey(def.ID))
			}
		}
		if loaded, err := s.settingRepo.GetMultiple(ctx, keys); err == nil {
			values = loaded
		}
	}
	runtimeByFeature := make(map[ProgressiveFeature][]FeatureRuntimeComponentStatus)
	for _, component := range runtime {
		runtimeByFeature[component.Feature] = append(runtimeByFeature[component.Feature], component)
	}

	features := make([]ProgressiveFeatureControlState, 0, len(manifest))
	for _, state := range manifest {
		def, ok := progressiveFeatureDefinitions[state.ID]
		if !ok {
			continue
		}
		var override *bool
		if value, exists := parseProgressiveFeatureOverride(values[progressiveFeatureOverrideKey(state.ID)]); exists {
			copyValue := value
			override = &copyValue
		}
		features = append(features, ProgressiveFeatureControlState{
			ID:                state.ID,
			Label:             def.Label,
			Tier:              state.Tier,
			Activation:        state.Activation,
			Enabled:           state.Enabled,
			ConfiguredEnabled: state.ConfiguredEnabled,
			Available:         state.Enabled,
			Controllable:      def.Tier != ProgressiveFeatureTierCore,
			Override:          override,
			RequiresRestart:   state.RequiresRestart,
			Reason:            state.Reason,
			MinimumProfile:    state.MinimumProfile,
			Dependencies:      append([]ProgressiveFeature{}, def.Dependencies...),
			Surfaces:          append([]ProgressiveFeatureSurface{}, state.Surfaces...),
			RuntimeComponents: append([]FeatureRuntimeComponentStatus{}, runtimeByFeature[state.ID]...),
		})
	}
	var cfg *config.Config
	if s != nil {
		cfg = s.cfg
	}
	return ProgressiveFeatureControlOverview{
		Profile:  normalizedFeatureProfile(cfg),
		Features: features,
	}
}

// SetProgressiveFeatureOverride persists an explicit control-plane decision.
// Passing nil restores inherited configuration. Lifecycle callbacks reconcile
// dynamic components asynchronously; boot components remain restart-bound.
func (s *SettingService) SetProgressiveFeatureOverride(ctx context.Context, id ProgressiveFeature, enabled *bool) error {
	def, ok := progressiveFeatureDefinitions[id]
	if !ok {
		return infraerrors.NotFound("PROGRESSIVE_FEATURE_NOT_FOUND", "progressive feature not found")
	}
	if def.Tier == ProgressiveFeatureTierCore {
		return infraerrors.BadRequest("PROGRESSIVE_FEATURE_NOT_CONTROLLABLE", "core features cannot be controlled")
	}
	if s == nil || s.settingRepo == nil {
		return infraerrors.InternalServer("SETTING_REPOSITORY_UNAVAILABLE", "setting repository is unavailable")
	}
	key := progressiveFeatureOverrideKey(id)
	var err error
	if enabled == nil {
		err = s.settingRepo.Delete(ctx, key)
	} else {
		err = s.settingRepo.Set(ctx, key, fmt.Sprintf("%t", *enabled))
	}
	if err != nil {
		return fmt.Errorf("persist progressive feature override: %w", err)
	}
	s.runOnUpdateCallbacks()
	return nil
}

func (s *SettingService) DeploymentMode(ctx context.Context) string {
	if s == nil || s.settingRepo == nil {
		return DeploymentModeDistribution
	}
	value, err := s.settingRepo.GetValue(ctx, SettingKeyDeploymentMode)
	if err != nil {
		return DeploymentModeDistribution
	}
	return NormalizeDeploymentMode(value)
}
