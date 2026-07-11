    // Announcements
export default {
      title: 'Announcements',
      description: 'Create announcements and target by conditions',
      createAnnouncement: 'Create Announcement',
      editAnnouncement: 'Edit Announcement',
      deleteAnnouncement: 'Delete Announcement',
      searchAnnouncements: 'Search announcements...',
      status: 'Status',
      allStatus: 'All Status',
      columns: {
        title: 'Title',
        status: 'Status',
        notifyMode: 'Notify Mode',
        targeting: 'Targeting',
        timeRange: 'Schedule',
        createdAt: 'Created At',
        actions: 'Actions'
      },
      statusLabels: {
        draft: 'Draft',
        active: 'Active',
        archived: 'Archived'
      },
      notifyModeLabels: {
        silent: 'Silent',
        popup: 'Popup'
      },
      form: {
        title: 'Title',
        content: 'Content (Markdown supported)',
        status: 'Status',
        notifyMode: 'Notify Mode',
        notifyModeHint: 'Popup mode will show a popup notification to users',
        startsAt: 'Starts At',
        endsAt: 'Ends At',
        startsAtHint: 'Leave empty to start immediately',
        endsAtHint: 'Leave empty to never expire',
        targetingMode: 'Targeting',
        targetingAll: 'All users',
        targetingCustom: 'Custom rules',
        addOrGroup: 'Add OR group',
        addAndCondition: 'Add AND condition',
        conditionType: 'Condition type',
        conditionSubscription: 'Subscription',
        conditionBalance: 'Balance',
        operator: 'Operator',
        balanceValue: 'Balance threshold',
        selectPackages: 'Select packages'
      },
      operators: {
        gt: '>',
        gte: '≥',
        lt: '<',
        lte: '≤',
        eq: '='
      },
      targetingSummaryAll: 'All users',
      targetingSummaryCustom: 'Custom ({groups} groups)',
      timeImmediate: 'Immediate',
      timeNever: 'Never',
      readStatus: 'Read Status',
      eligible: 'Eligible',
      readAt: 'Read at',
      unread: 'Unread',
      searchUsers: 'Search users...',
      failedToLoad: 'Failed to load announcements',
      failedToCreate: 'Failed to create announcement',
      failedToUpdate: 'Failed to update announcement',
      failedToDelete: 'Failed to delete announcement',
      failedToLoadReadStatus: 'Failed to load read status',
      deleteConfirm: 'Are you sure you want to delete this announcement? This action cannot be undone.'
    }
