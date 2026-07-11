    // TLS 指纹模板
export default {
      title: 'TLS 指纹模板',
      description: '管理 TLS 指纹模板，用于模拟特定客户端的 TLS 握手特征',
      createProfile: '创建模板',
      editProfile: '编辑模板',
      deleteProfile: '删除模板',
      noProfiles: '暂无模板',
      createFirstProfile: '创建你的第一个 TLS 指纹模板',

      columns: {
        name: '名称',
        description: '描述',
        grease: 'GREASE',
        alpn: 'ALPN',
        actions: '操作'
      },

      form: {
        pasteYaml: '粘贴 YAML 配置',
        pasteYamlPlaceholder: '将 TLS 指纹采集器复制的 YAML 粘贴到这里...',
        pasteYamlHint: '粘贴从 TLS 指纹采集器复制的 YAML 配置，自动填充所有字段。',
        openCollector: '打开采集器',
        parseYaml: '解析 YAML',
        yamlParsed: 'YAML 解析成功，字段已自动填充',
        yamlParseFailed: 'YAML 解析失败：未找到 name 字段',
        name: '模板名称',
        namePlaceholder: '例如 macOS Node.js v24',
        description: '描述',
        descriptionPlaceholder: '可选的模板描述',
        enableGrease: '启用 GREASE',
        enableGreaseHint: '在 TLS ClientHello 扩展中插入 GREASE 值',
        cipherSuites: '密码套件',
        cipherSuitesHint: '逗号分隔的十六进制值，例如 0x1301, 0x1302, 0xc02c',
        curves: '椭圆曲线',
        curvesHint: '逗号分隔的曲线 ID',
        pointFormats: '点格式',
        signatureAlgorithms: '签名算法',
        alpnProtocols: 'ALPN 协议',
        alpnProtocolsHint: '逗号分隔，例如 h2, http/1.1',
        supportedVersions: '支持的 TLS 版本',
        keyShareGroups: '密钥共享组',
        pskModes: 'PSK 模式',
        extensions: '扩展'
      },

      deleteConfirm: '删除模板',
      deleteConfirmMessage: '确定要删除模板 "{name}" 吗？使用此模板的账号将回退到内置默认值。',
      createSuccess: '模板创建成功',
      updateSuccess: '模板更新成功',
      deleteSuccess: '模板删除成功',
      loadFailed: '加载模板失败',
      saveFailed: '保存模板失败',
      deleteFailed: '删除模板失败'
    }
