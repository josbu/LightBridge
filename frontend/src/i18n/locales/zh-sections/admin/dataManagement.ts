export default {
      title: '数据管理',
      description: '统一管理数据管理代理状态、对象存储配置和备份任务',
      agent: {
        title: '数据管理代理状态',
        description: '系统会自动探测固定 Unix Socket，仅在可连通时启用数据管理功能。',
        enabled: '数据管理代理已就绪，可继续进行数据管理操作。',
        disabled: '数据管理代理不可用，当前仅可查看诊断信息。',
        socketPath: 'Socket 路径',
        version: '版本',
        status: '状态',
        uptime: '运行时长',
        reasonLabel: '不可用原因',
        reason: {
          DATA_MANAGEMENT_AGENT_SOCKET_MISSING: '未检测到数据管理 Socket 文件',
          DATA_MANAGEMENT_AGENT_UNAVAILABLE: '数据管理代理不可连通',
          BACKUP_AGENT_SOCKET_MISSING: '未检测到备份 Socket 文件',
          BACKUP_AGENT_UNAVAILABLE: '备份代理不可连通',
          UNKNOWN: '未知原因'
        }
      },
      sections: {
        config: {
          title: '备份配置',
          description: '配置备份源、保留策略与 S3 存储参数。'
        },
        s3: {
          title: 'S3 对象存储',
          description: '配置并测试备份产物上传到标准 S3 对象存储。'
        },
        backup: {
          title: '备份操作',
          description: '触发 PostgreSQL、Redis 与全量备份任务。'
        },
        history: {
          title: '备份历史',
          description: '查看备份任务执行状态、错误与产物信息。'
        }
      },
      form: {
        sourceMode: '源模式',
        backupRoot: '备份根目录',
        activePostgresProfile: '当前激活 PostgreSQL 配置',
        activeRedisProfile: '当前激活 Redis 配置',
        activeS3Profile: '当前激活 S3 账号',
        retentionDays: '保留天数',
        keepLast: '至少保留最近任务数',
        uploadToS3: '上传到 S3',
        useActivePostgresProfile: '使用当前激活 PostgreSQL 配置',
        useActiveRedisProfile: '使用当前激活 Redis 配置',
        useActiveS3Profile: '使用当前激活账号',
        idempotencyKey: '幂等键（可选）',
        secretConfigured: '已配置，留空不变',
        source: {
          profileID: '配置 ID（唯一）',
          profileName: '配置名称',
          setActive: '创建后立即设为激活配置'
        },
        postgres: {
          title: 'PostgreSQL',
          host: '主机',
          port: '端口',
          user: '用户名',
          password: '密码',
          database: '数据库',
          sslMode: 'SSL 模式',
          containerName: '容器名（docker_exec 模式）'
        },
        redis: {
          title: 'Redis',
          addr: '地址（host:port）',
          username: '用户名',
          password: '密码',
          db: '数据库编号',
          containerName: '容器名（docker_exec 模式）'
        },
        s3: {
          enabled: '启用 S3 上传',
          profileID: '账号 ID（唯一）',
          profileName: '账号名称',
          endpoint: 'Endpoint（可选）',
          region: 'Region',
          bucket: 'Bucket',
          accessKeyID: 'Access Key ID',
          secretAccessKey: 'Secret Access Key',
          prefix: '对象前缀',
          forcePathStyle: '强制 path-style',
          useSSL: '使用 SSL',
          setActive: '创建后立即设为激活账号'
        }
      },
      sourceProfiles: {
        createTitle: '创建数据源配置',
        editTitle: '编辑数据源配置',
        empty: '暂无配置，请先创建',
        deleteConfirm: '确定删除配置 {profileID} 吗？',
        columns: {
          profile: '配置',
          active: '激活状态',
          connection: '连接信息',
          database: '数据库',
          updatedAt: '更新时间',
          actions: '操作'
        }
      },
      s3Profiles: {
        createTitle: '创建 S3 账号',
        editTitle: '编辑 S3 账号',
        empty: '暂无 S3 账号，请先创建',
        editHint: '点击“编辑”将在右侧抽屉中修改账号信息。',
        deleteConfirm: '确定删除 S3 账号 {profileID} 吗？',
        columns: {
          profile: '账号',
          active: '激活状态',
          storage: '存储配置',
          updatedAt: '更新时间',
          actions: '操作'
        }
      },
      history: {
        total: '共 {count} 条',
        empty: '暂无备份任务',
        columns: {
          jobID: '任务 ID',
          type: '类型',
          status: '状态',
          triggeredBy: '触发人',
          pgProfile: 'PostgreSQL 配置',
          redisProfile: 'Redis 配置',
          s3Profile: 'S3 账号',
          finishedAt: '完成时间',
          artifact: '产物',
          error: '错误'
        },
        status: {
          queued: '排队中',
          running: '执行中',
          succeeded: '成功',
          failed: '失败',
          partial_succeeded: '部分成功'
        }
      },
      actions: {
        refresh: '刷新状态',
        disabledHint: '请先启动 datamanagementd 并确认 Socket 可连通。',
        reloadConfig: '加载配置',
        reloadSourceProfiles: '刷新数据源配置',
        reloadProfiles: '刷新账号列表',
        newSourceProfile: '新建数据源配置',
        saveConfig: '保存配置',
        configSaved: '配置保存成功',
        testS3: '测试 S3 连接',
        s3TestOK: 'S3 连接测试成功',
        s3TestFailed: 'S3 连接测试失败',
        newProfile: '新建账号',
        saveProfile: '保存账号',
        activateProfile: '设为激活',
        profileIDRequired: '请输入账号 ID',
        profileNameRequired: '请输入账号名称',
        profileSelectRequired: '请先选择要编辑的账号',
        profileCreated: 'S3 账号创建成功',
        profileSaved: 'S3 账号保存成功',
        profileActivated: 'S3 账号已切换为激活',
        profileDeleted: 'S3 账号删除成功',
        sourceProfileCreated: '数据源配置创建成功',
        sourceProfileSaved: '数据源配置保存成功',
        sourceProfileActivated: '数据源配置已切换为激活',
        sourceProfileDeleted: '数据源配置删除成功',
        createBackup: '创建备份任务',
        jobCreated: '备份任务已创建：{jobID}（{status}）',
        refreshJobs: '刷新任务',
        loadMore: '加载更多'
      }
    }
