export default {
    title: 'Model List',
    description: 'View usable models, pricing, and usage modes from the current group and account catalog',
    searchPlaceholder: 'Search models, groups, or sources...',
    empty: 'No model catalog yet. Pull upstream models or maintain an account model list first.',
    modelCount: '{count} model(s)',
    sourceCount: '{count} source(s)',
    sourceDetails: 'Source details',
    unknownAccount: 'Unknown account',
    noGroups: 'No groups bound',
    noPrice: 'No price configured',
    priceTokenRange: 'Input {input} / Output {output}',
    priceRequestRange: '{price} per request',
    usageUnknown: 'Unspecified',
    views: {
      merged: 'Merged',
      by_group: 'By Group',
      by_channel: 'By Channel',
      by_account: 'By Account'
    },
    usageModes: {
      chat: 'Chat',
      responses: 'Responses',
      embeddings: 'Embeddings',
      image: 'Image',
      audio: 'Audio'
    },
    // Monitor status
    setupMonitor: 'Setup Monitor',
    monitorStatus: {
      operational: 'Operational',
      degraded: 'Degraded',
      failed: 'Failed',
      error: 'Error'
    },
    quickMonitorTitle: 'Set up monitoring for {model}',
    quickMonitorSelectSource: 'Select monitoring source',
    quickMonitorSourceHint: 'Will use the selected source\'s endpoint and API key',
    quickMonitorInterval: 'Check Interval',
    quickMonitorSubmit: 'Create & Enable',
    allGroups: 'All Groups',
    selectAccountMonitor: 'Set up monitoring for {model}',
    selectAccountHint: 'Select an account source to create monitoring using its configuration'
  }
