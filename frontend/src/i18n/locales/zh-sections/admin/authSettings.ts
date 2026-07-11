    // Auth Settings
export default {
      title: '登录与认证',
      description: '管理 OAuth 登录提供商配置，包括 OIDC、GitHub、Google、微信、LinuxDO 和钉钉',
      backToSettings: '返回系统设置',
      oidc: {
        title: 'OIDC 登录',
        description: '通用 OpenID Connect 协议登录',
        providerName: '提供商名称',
        providerNamePlaceholder: '例如：Keycloak',
        clientId: 'Client ID',
        clientIdPlaceholder: '输入 Client ID',
        clientSecret: 'Client Secret',
        clientSecretPlaceholder: '输入 Client Secret',
        issuerUrl: 'Issuer URL',
        issuerUrlPlaceholder: 'https://your-oidc-provider.com',
        redirectUrl: 'Redirect URL',
        redirectUrlPlaceholder: 'https://your-domain.com/api/v1/oauth/oidc/callback'
      },
      github: {
        title: 'GitHub 登录',
        description: 'GitHub OAuth 登录',
        clientId: 'Client ID',
        clientIdPlaceholder: '输入 GitHub Client ID',
        clientSecret: 'Client Secret',
        clientSecretPlaceholder: '输入 GitHub Client Secret',
        redirectUrl: 'Redirect URL',
        redirectUrlPlaceholder: 'https://your-domain.com/api/v1/oauth/github/callback'
      },
      google: {
        title: 'Google 登录',
        description: 'Google OAuth 登录',
        clientId: 'Client ID',
        clientIdPlaceholder: '输入 Google Client ID',
        clientSecret: 'Client Secret',
        clientSecretPlaceholder: '输入 Google Client Secret',
        redirectUrl: 'Redirect URL',
        redirectUrlPlaceholder: 'https://your-domain.com/api/v1/oauth/google/callback'
      },
      wechat: {
        title: '微信登录',
        description: '微信 OAuth 登录',
        appId: 'App ID',
        appIdPlaceholder: '输入微信 App ID',
        appSecret: 'App Secret',
        appSecretPlaceholder: '输入微信 App Secret',
        redirectUrl: 'Redirect URL',
        redirectUrlPlaceholder: 'https://your-domain.com/api/v1/oauth/wechat/callback'
      },
      linuxdo: {
        title: 'LinuxDO 登录',
        description: 'LinuxDO OAuth 登录',
        clientId: 'Client ID',
        clientIdPlaceholder: '输入 LinuxDO Client ID',
        clientSecret: 'Client Secret',
        clientSecretPlaceholder: '输入 LinuxDO Client Secret',
        redirectUrl: 'Redirect URL',
        redirectUrlPlaceholder: 'https://your-domain.com/api/v1/oauth/linuxdo/callback'
      },
      dingtalk: {
        title: '钉钉登录',
        description: '钉钉 OAuth 登录',
        clientId: 'Client ID',
        clientIdPlaceholder: '输入钉钉 Client ID',
        redirectUrl: 'Redirect URL',
        redirectUrlPlaceholder: 'https://your-domain.com/api/v1/oauth/dingtalk/callback'
      },
      registration: {
        title: '注册设置',
        description: '控制用户注册、密码重置和邀请码功能',
        emailVerify: '邮箱验证',
        emailVerifyHint: '注册时要求验证邮箱地址',
        passwordReset: '密码重置',
        passwordResetHint: '允许用户通过邮箱重置密码',
        invitationCode: '邀请码注册',
        invitationCodeHint: '注册时要求输入邀请码'
      }
    }
