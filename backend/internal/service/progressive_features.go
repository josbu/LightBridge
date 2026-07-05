package service

import "context"

// ProgressiveFeature identifies a feature that should be structurally inactive
// when disabled: routes reject before handlers, frontend routes are absent, and
// background workers do not run.
type ProgressiveFeature string

const (
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
)

type ProgressiveFeatureDefinition struct {
	ID               ProgressiveFeature
	SettingKey       string
	DefaultEnabled   bool
	DistributionOnly bool
	Label            string
}

var progressiveFeatureDefinitions = map[ProgressiveFeature]ProgressiveFeatureDefinition{
	ProgressiveFeatureChannelMonitor: {
		ID:             ProgressiveFeatureChannelMonitor,
		SettingKey:     SettingKeyChannelMonitorEnabled,
		DefaultEnabled: true,
		Label:          "channel monitor",
	},
	ProgressiveFeatureAvailableChannels: {
		ID:             ProgressiveFeatureAvailableChannels,
		SettingKey:     SettingKeyAvailableChannelsEnabled,
		DefaultEnabled: false,
		Label:          "available channels",
	},
	ProgressiveFeaturePayment: {
		ID:             ProgressiveFeaturePayment,
		SettingKey:     SettingPaymentEnabled,
		DefaultEnabled: false,
		Label:          "payment",
	},
	ProgressiveFeatureRiskControl: {
		ID:               ProgressiveFeatureRiskControl,
		SettingKey:       SettingKeyRiskControlEnabled,
		DefaultEnabled:   false,
		DistributionOnly: true,
		Label:            "risk control",
	},
	ProgressiveFeaturePrivacyFilter: {
		ID:             ProgressiveFeaturePrivacyFilter,
		SettingKey:     SettingKeyPrivacyFilterEnabled,
		DefaultEnabled: false,
		Label:          "privacy filter",
	},
	ProgressiveFeatureAffiliate: {
		ID:             ProgressiveFeatureAffiliate,
		SettingKey:     SettingKeyAffiliateEnabled,
		DefaultEnabled: false,
		Label:          "affiliate",
	},
	ProgressiveFeatureAnnouncements: {
		ID:               ProgressiveFeatureAnnouncements,
		SettingKey:       SettingKeyAnnouncementsEnabled,
		DefaultEnabled:   true,
		DistributionOnly: true,
		Label:            "announcements",
	},
	ProgressiveFeatureRedeem: {
		ID:               ProgressiveFeatureRedeem,
		SettingKey:       SettingKeyRedeemEnabled,
		DefaultEnabled:   true,
		DistributionOnly: true,
		Label:            "redeem",
	},
	ProgressiveFeaturePromo: {
		ID:               ProgressiveFeaturePromo,
		SettingKey:       SettingKeyPromoCodeEnabled,
		DefaultEnabled:   true,
		DistributionOnly: true,
		Label:            "promo",
	},
	ProgressiveFeatureProxies: {
		ID:             ProgressiveFeatureProxies,
		SettingKey:     SettingKeyProxiesEnabled,
		DefaultEnabled: true,
		Label:          "proxies",
	},
	ProgressiveFeatureChannelPricing: {
		ID:             ProgressiveFeatureChannelPricing,
		SettingKey:     SettingKeyChannelPricingEnabled,
		DefaultEnabled: true,
		Label:          "channel pricing",
	},
	ProgressiveFeatureSubscriptions: {
		ID:               ProgressiveFeatureSubscriptions,
		DefaultEnabled:   true,
		DistributionOnly: true,
		Label:            "subscriptions",
	},
}

func ProgressiveFeatureDefinitionFor(feature ProgressiveFeature) (ProgressiveFeatureDefinition, bool) {
	def, ok := progressiveFeatureDefinitions[feature]
	return def, ok
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
	def, ok := ProgressiveFeatureDefinitionFor(feature)
	if !ok {
		return false
	}
	if def.DistributionOnly && s.DeploymentMode(ctx) == DeploymentModePersonal {
		return false
	}
	if def.SettingKey == "" {
		return def.DefaultEnabled
	}
	return s.IsBooleanSettingEnabled(ctx, def.SettingKey, def.DefaultEnabled)
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
