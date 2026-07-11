    // Available Channels (aggregated read-only view)
export default {
      title: 'Available Channels',
      description: 'Aggregated view: each channel with its linked groups and supported models (wildcards expanded)',
      searchPlaceholder: 'Search channels or models...',
      columns: {
        name: 'Channel',
        status: 'Status',
        billingSource: 'Billing Model Source',
        groups: 'Linked Groups',
        supportedModels: 'Supported Models'
      },
      empty: 'No data',
      noGroups: 'No linked groups',
      noModels: 'No model mapping configured',
      noPricing: 'Pricing not configured',
      statusActive: 'Active',
      statusDisabled: 'Disabled',
      billingSource: {
        requested: 'Requested model',
        upstream: 'Upstream model',
        channel_mapped: 'Channel-mapped model'
      },
      pricing: {
        billingMode: 'Billing Mode',
        billingModeToken: 'Per Token',
        billingModePerRequest: 'Per Request',
        billingModeImage: 'Per Image',
        inputPrice: 'Input',
        outputPrice: 'Output',
        cacheWritePrice: 'Cache Write',
        cacheReadPrice: 'Cache Read',
        imageOutputPrice: 'Image Output',
        perRequestPrice: 'Per Request',
        intervals: 'Tiered Pricing',
        unitPerMillion: '/ 1M tokens',
        unitPerRequest: '/ request'
      }
    }
