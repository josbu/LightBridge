import { isDistributionModeNow } from '@/composables/useDeploymentMode'
import {
  FeatureFlags,
  isFeatureFlagEnabled,
  type FeatureFlagDefinition,
  type RegisteredFeatureFlag,
} from '@/utils/featureFlags'

export type ProgressiveFeatureId =
  | 'channelMonitor'
  | 'availableChannels'
  | 'payment'
  | 'riskControl'
  | 'privacyFilter'
  | 'affiliate'
  | 'announcements'
  | 'redeem'
  | 'promo'
  | 'proxies'
  | 'channelPricing'
  | 'subscriptions'

export interface ProgressiveFeatureDefinition {
  readonly id: ProgressiveFeatureId
  readonly label: string
  readonly flag?: RegisteredFeatureFlag
  readonly distributionOnly?: boolean
  readonly routeNames: readonly string[]
  readonly exactPaths?: readonly string[]
  readonly pathPrefixes?: readonly string[]
}

function defineProgressiveFeature(def: ProgressiveFeatureDefinition): ProgressiveFeatureDefinition {
  return def
}

/**
 * Progressive feature registry.
 *
 * This is the frontend single source of truth for feature-driven UI structure:
 * routes, direct-URL fallback checks, sidebar visibility, and small runtime
 * components all resolve through these definitions.
 */
export const ProgressiveFeatures = {
  channelMonitor: defineProgressiveFeature({
    id: 'channelMonitor',
    label: 'Channel Monitor',
    flag: 'channelMonitor',
    routeNames: ['AdminChannelMonitor', 'ChannelStatus'],
    pathPrefixes: ['/admin/channels/monitor', '/monitor'],
  }),
  availableChannels: defineProgressiveFeature({
    id: 'availableChannels',
    label: 'Available Channels',
    flag: 'availableChannels',
    routeNames: ['UserAvailableChannels'],
    pathPrefixes: ['/available-channels'],
  }),
  payment: defineProgressiveFeature({
    id: 'payment',
    label: 'Payment',
    flag: 'payment',
    routeNames: [
      'PurchaseSubscription',
      'OrderList',
      'PaymentQRCode',
      'PaymentResult',
      'StripePayment',
      'AirwallexPayment',
      'StripePopup',
      'AdminPaymentDashboard',
      'AdminOrders',
      'AdminPaymentPlans',
    ],
    pathPrefixes: ['/purchase', '/orders', '/payment', '/admin/orders'],
  }),
  riskControl: defineProgressiveFeature({
    id: 'riskControl',
    label: 'Risk Control',
    flag: 'riskControl',
    distributionOnly: true,
    routeNames: ['AdminRiskControl'],
    pathPrefixes: ['/admin/risk-control'],
  }),
  privacyFilter: defineProgressiveFeature({
    id: 'privacyFilter',
    label: 'Privacy Filter',
    flag: 'privacyFilter',
    routeNames: ['AdminPrivacyFilter'],
    pathPrefixes: ['/admin/privacy-filter'],
  }),
  affiliate: defineProgressiveFeature({
    id: 'affiliate',
    label: 'Affiliate',
    flag: 'affiliate',
    routeNames: [
      'Affiliate',
      'AdminAffiliatesRoot',
      'AdminAffiliateInvites',
      'AdminAffiliateRebates',
      'AdminAffiliateTransfers',
    ],
    pathPrefixes: ['/affiliate', '/admin/affiliates'],
  }),
  announcements: defineProgressiveFeature({
    id: 'announcements',
    label: 'Announcements',
    flag: 'announcements',
    distributionOnly: true,
    routeNames: ['AdminAnnouncements'],
    pathPrefixes: ['/admin/announcements'],
  }),
  redeem: defineProgressiveFeature({
    id: 'redeem',
    label: 'Redeem Codes',
    flag: 'redeem',
    distributionOnly: true,
    routeNames: ['AdminRedeem', 'Redeem'],
    pathPrefixes: ['/admin/redeem', '/redeem'],
  }),
  promo: defineProgressiveFeature({
    id: 'promo',
    label: 'Promo Codes',
    flag: 'promo',
    distributionOnly: true,
    routeNames: ['AdminPromoCodes'],
    pathPrefixes: ['/admin/promo-codes'],
  }),
  proxies: defineProgressiveFeature({
    id: 'proxies',
    label: 'IP Management',
    flag: 'proxies',
    routeNames: ['AdminProxies'],
    pathPrefixes: ['/admin/proxies'],
  }),
  channelPricing: defineProgressiveFeature({
    id: 'channelPricing',
    label: 'Channel Pricing',
    flag: 'channelPricing',
    routeNames: ['AdminChannelsRoot', 'AdminChannels'],
    exactPaths: ['/admin/channels'],
    pathPrefixes: ['/admin/channels/pricing'],
  }),
  subscriptions: defineProgressiveFeature({
    id: 'subscriptions',
    label: 'Subscriptions',
    distributionOnly: true,
    routeNames: ['AdminSubscriptions', 'Subscriptions'],
    pathPrefixes: ['/admin/subscriptions', '/subscriptions'],
  }),
} as const

export const progressiveFeatureList = Object.values(ProgressiveFeatures) as ProgressiveFeatureDefinition[]

export function resolveProgressiveFeatureFlag(
  feature: ProgressiveFeatureDefinition,
): FeatureFlagDefinition | undefined {
  return feature.flag ? FeatureFlags[feature.flag] : undefined
}

export function isProgressiveFeatureEnabled(feature: ProgressiveFeatureDefinition): boolean {
  if (feature.distributionOnly && !isDistributionModeNow()) {
    return false
  }
  const flag = resolveProgressiveFeatureFlag(feature)
  return flag ? isFeatureFlagEnabled(flag) : true
}

export function makeProgressiveSidebarFlag(
  feature: ProgressiveFeatureDefinition,
): () => boolean {
  return () => isProgressiveFeatureEnabled(feature)
}

function pathMatchesFeature(path: string, feature: ProgressiveFeatureDefinition): boolean {
  if (feature.exactPaths?.some((exact) => path === exact)) {
    return true
  }
  return feature.pathPrefixes?.some(
    (prefix) => path === prefix || path.startsWith(prefix + '/'),
  ) ?? false
}

export function findProgressiveFeatureByPath(path: string): ProgressiveFeatureDefinition | undefined {
  return progressiveFeatureList.find((feature) => pathMatchesFeature(path, feature))
}

export function findDisabledProgressiveFeatureByPath(
  path: string,
): ProgressiveFeatureDefinition | undefined {
  return progressiveFeatureList.find(
    (feature) => pathMatchesFeature(path, feature) && !isProgressiveFeatureEnabled(feature),
  )
}

export function isProgressivePathDisabled(path: string): boolean {
  return Boolean(findDisabledProgressiveFeatureByPath(path))
}
