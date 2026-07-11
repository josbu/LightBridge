    // Error Passthrough Rules
export default {
      title: 'Error Passthrough Rules',
      description: 'Configure how upstream errors are returned to clients',
      createRule: 'Create Rule',
      editRule: 'Edit Rule',
      deleteRule: 'Delete Rule',
      noRules: 'No rules configured',
      createFirstRule: 'Create your first error passthrough rule',
      allPlatforms: 'All Platforms',
      passthrough: 'Passthrough',
      custom: 'Custom',
      code: 'Code',
      body: 'Body',
      skipMonitoring: 'Skip Monitoring',

      // Columns
      columns: {
        priority: 'Priority',
        name: 'Name',
        conditions: 'Conditions',
        platforms: 'Platforms',
        behavior: 'Behavior',
        status: 'Status',
        actions: 'Actions'
      },

      // Match Mode
      matchMode: {
        any: 'Code OR Keyword',
        all: 'Code AND Keyword',
        anyHint: 'Status code matches any error code, OR message contains any keyword',
        allHint: 'Status code matches any error code, AND message contains any keyword'
      },

      // Form
      form: {
        name: 'Rule Name',
        namePlaceholder: 'e.g., Context Limit Passthrough',
        priority: 'Priority',
        priorityHint: 'Lower values have higher priority',
        description: 'Description',
        descriptionPlaceholder: 'Describe the purpose of this rule...',
        matchConditions: 'Match Conditions',
        errorCodes: 'Error Codes',
        errorCodesPlaceholder: '422, 400, 429',
        errorCodesHint: 'Separate multiple codes with commas',
        keywords: 'Keywords',
        keywordsPlaceholder: 'One keyword per line\ncontext limit\nmodel not supported',
        keywordsHint: 'One keyword per line, case-insensitive',
        matchMode: 'Match Mode',
        platforms: 'Platforms',
        platformsHint: 'Leave empty to apply to all platforms',
        responseBehavior: 'Response Behavior',
        passthroughCode: 'Passthrough upstream status code',
        responseCode: 'Custom status code',
        passthroughBody: 'Passthrough upstream error message',
        customMessage: 'Custom error message',
        customMessagePlaceholder: 'Error message to return to client...',
        skipMonitoring: 'Skip monitoring',
        skipMonitoringHint: 'When enabled, errors matching this rule will not be recorded in ops monitoring',
        enabled: 'Enable this rule'
      },

      // Messages
      nameRequired: 'Please enter rule name',
      conditionsRequired: 'Please configure at least one error code or keyword',
      ruleCreated: 'Rule created successfully',
      ruleUpdated: 'Rule updated successfully',
      ruleDeleted: 'Rule deleted successfully',
      deleteConfirm: 'Are you sure you want to delete rule "{name}"?',
      failedToLoad: 'Failed to load rules',
      failedToSave: 'Failed to save rule',
      failedToDelete: 'Failed to delete rule',
      failedToToggle: 'Failed to toggle status'
    }
