export default {
      title: 'Data Management',
      description: 'Manage data management agent status, object storage settings, and backup jobs in one place',
      agent: {
        title: 'Data Management Agent Status',
        description: 'The system probes a fixed Unix socket and enables data management only when reachable.',
        enabled: 'Data management agent is ready. Data management operations are available.',
        disabled: 'Data management agent is unavailable. Only diagnostic information is available now.',
        socketPath: 'Socket Path',
        version: 'Version',
        status: 'Status',
        uptime: 'Uptime',
        reasonLabel: 'Unavailable Reason',
        reason: {
          DATA_MANAGEMENT_AGENT_SOCKET_MISSING: 'Data management socket file is missing',
          DATA_MANAGEMENT_AGENT_UNAVAILABLE: 'Data management agent is unreachable',
          BACKUP_AGENT_SOCKET_MISSING: 'Backup socket file is missing',
          BACKUP_AGENT_UNAVAILABLE: 'Backup agent is unreachable',
          UNKNOWN: 'Unknown reason'
        }
      },
      sections: {
        config: {
          title: 'Backup Configuration',
          description: 'Configure backup source, retention policy, and S3 settings.'
        },
        s3: {
          title: 'S3 Object Storage',
          description: 'Configure and test uploads of backup artifacts to a standard S3-compatible storage.'
        },
        backup: {
          title: 'Backup Operations',
          description: 'Trigger PostgreSQL, Redis, and full backup jobs.'
        },
        history: {
          title: 'Backup History',
          description: 'Review backup job status, errors, and artifact metadata.'
        }
      },
      form: {
        sourceMode: 'Source Mode',
        backupRoot: 'Backup Root',
        activePostgresProfile: 'Active PostgreSQL Profile',
        activeRedisProfile: 'Active Redis Profile',
        activeS3Profile: 'Active S3 Profile',
        retentionDays: 'Retention Days',
        keepLast: 'Keep Last Jobs',
        uploadToS3: 'Upload to S3',
        useActivePostgresProfile: 'Use Active PostgreSQL Profile',
        useActiveRedisProfile: 'Use Active Redis Profile',
        useActiveS3Profile: 'Use Active Profile',
        idempotencyKey: 'Idempotency Key (Optional)',
        secretConfigured: 'Configured already, leave empty to keep unchanged',
        source: {
          profileID: 'Profile ID (Unique)',
          profileName: 'Profile Name',
          setActive: 'Set as active after creation'
        },
        postgres: {
          title: 'PostgreSQL',
          host: 'Host',
          port: 'Port',
          user: 'User',
          password: 'Password',
          database: 'Database',
          sslMode: 'SSL Mode',
          containerName: 'Container Name (docker_exec mode)'
        },
        redis: {
          title: 'Redis',
          addr: 'Address (host:port)',
          username: 'Username',
          password: 'Password',
          db: 'Database Index',
          containerName: 'Container Name (docker_exec mode)'
        },
        s3: {
          enabled: 'Enable S3 Upload',
          profileID: 'Profile ID (Unique)',
          profileName: 'Profile Name',
          endpoint: 'Endpoint (Optional)',
          region: 'Region',
          bucket: 'Bucket',
          accessKeyID: 'Access Key ID',
          secretAccessKey: 'Secret Access Key',
          prefix: 'Object Prefix',
          forcePathStyle: 'Force Path Style',
          useSSL: 'Use SSL',
          setActive: 'Set as active after creation'
        }
      },
      sourceProfiles: {
        createTitle: 'Create Source Profile',
        editTitle: 'Edit Source Profile',
        empty: 'No source profiles yet, create one first',
        deleteConfirm: 'Delete source profile {profileID}?',
        columns: {
          profile: 'Profile',
          active: 'Active',
          connection: 'Connection',
          database: 'Database',
          updatedAt: 'Updated At',
          actions: 'Actions'
        }
      },
      s3Profiles: {
        createTitle: 'Create S3 Profile',
        editTitle: 'Edit S3 Profile',
        empty: 'No S3 profiles yet, create one first',
        editHint: 'Click "Edit" to modify profile details in the right drawer.',
        deleteConfirm: 'Delete S3 profile {profileID}?',
        columns: {
          profile: 'Profile',
          active: 'Active',
          storage: 'Storage',
          updatedAt: 'Updated At',
          actions: 'Actions'
        }
      },
      history: {
        total: '{count} jobs',
        empty: 'No backup jobs yet',
        columns: {
          jobID: 'Job ID',
          type: 'Type',
          status: 'Status',
          triggeredBy: 'Triggered By',
          pgProfile: 'PostgreSQL Profile',
          redisProfile: 'Redis Profile',
          s3Profile: 'S3 Profile',
          finishedAt: 'Finished At',
          artifact: 'Artifact',
          error: 'Error'
        },
        status: {
          queued: 'Queued',
          running: 'Running',
          succeeded: 'Succeeded',
          failed: 'Failed',
          partial_succeeded: 'Partial Succeeded'
        }
      },
      actions: {
        refresh: 'Refresh Status',
        disabledHint: 'Start datamanagementd first and ensure the socket is reachable.',
        reloadConfig: 'Reload Config',
        reloadSourceProfiles: 'Reload Source Profiles',
        reloadProfiles: 'Reload Profiles',
        newSourceProfile: 'New Source Profile',
        saveConfig: 'Save Config',
        configSaved: 'Configuration saved',
        testS3: 'Test S3 Connection',
        s3TestOK: 'S3 connection test succeeded',
        s3TestFailed: 'S3 connection test failed',
        newProfile: 'New Profile',
        saveProfile: 'Save Profile',
        activateProfile: 'Activate',
        profileIDRequired: 'Profile ID is required',
        profileNameRequired: 'Profile name is required',
        profileSelectRequired: 'Select a profile to edit first',
        profileCreated: 'S3 profile created',
        profileSaved: 'S3 profile saved',
        profileActivated: 'S3 profile activated',
        profileDeleted: 'S3 profile deleted',
        sourceProfileCreated: 'Source profile created',
        sourceProfileSaved: 'Source profile saved',
        sourceProfileActivated: 'Source profile activated',
        sourceProfileDeleted: 'Source profile deleted',
        createBackup: 'Create Backup Job',
        jobCreated: 'Backup job created: {jobID} ({status})',
        refreshJobs: 'Refresh Jobs',
        loadMore: 'Load More'
      }
    }
