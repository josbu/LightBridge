    // Auth Settings
export default {
      title: 'Auth & Login',
      description: 'Manage OAuth login provider configurations including OIDC, GitHub, Google, WeChat, LinuxDO, and DingTalk',
      backToSettings: 'Back to Settings',
      oidc: {
        title: 'OIDC Login',
        description: 'Generic OpenID Connect protocol login',
        providerName: 'Provider Name',
        providerNamePlaceholder: 'e.g., Keycloak',
        clientId: 'Client ID',
        clientIdPlaceholder: 'Enter Client ID',
        clientSecret: 'Client Secret',
        clientSecretPlaceholder: 'Enter Client Secret',
        issuerUrl: 'Issuer URL',
        issuerUrlPlaceholder: 'https://your-oidc-provider.com',
        redirectUrl: 'Redirect URL',
        redirectUrlPlaceholder: 'https://your-domain.com/api/v1/oauth/oidc/callback'
      },
      github: {
        title: 'GitHub Login',
        description: 'GitHub OAuth login',
        clientId: 'Client ID',
        clientIdPlaceholder: 'Enter GitHub Client ID',
        clientSecret: 'Client Secret',
        clientSecretPlaceholder: 'Enter GitHub Client Secret',
        redirectUrl: 'Redirect URL',
        redirectUrlPlaceholder: 'https://your-domain.com/api/v1/oauth/github/callback'
      },
      google: {
        title: 'Google Login',
        description: 'Google OAuth login',
        clientId: 'Client ID',
        clientIdPlaceholder: 'Enter Google Client ID',
        clientSecret: 'Client Secret',
        clientSecretPlaceholder: 'Enter Google Client Secret',
        redirectUrl: 'Redirect URL',
        redirectUrlPlaceholder: 'https://your-domain.com/api/v1/oauth/google/callback'
      },
      wechat: {
        title: 'WeChat Login',
        description: 'WeChat OAuth login',
        appId: 'App ID',
        appIdPlaceholder: 'Enter WeChat App ID',
        appSecret: 'App Secret',
        appSecretPlaceholder: 'Enter WeChat App Secret',
        redirectUrl: 'Redirect URL',
        redirectUrlPlaceholder: 'https://your-domain.com/api/v1/oauth/wechat/callback'
      },
      linuxdo: {
        title: 'LinuxDO Login',
        description: 'LinuxDO OAuth login',
        clientId: 'Client ID',
        clientIdPlaceholder: 'Enter LinuxDO Client ID',
        clientSecret: 'Client Secret',
        clientSecretPlaceholder: 'Enter LinuxDO Client Secret',
        redirectUrl: 'Redirect URL',
        redirectUrlPlaceholder: 'https://your-domain.com/api/v1/oauth/linuxdo/callback'
      },
      dingtalk: {
        title: 'DingTalk Login',
        description: 'DingTalk OAuth login',
        clientId: 'Client ID',
        clientIdPlaceholder: 'Enter DingTalk Client ID',
        redirectUrl: 'Redirect URL',
        redirectUrlPlaceholder: 'https://your-domain.com/api/v1/oauth/dingtalk/callback'
      },
      registration: {
        title: 'Registration Settings',
        description: 'Control user registration, password reset, and invitation code features',
        emailVerify: 'Email Verification',
        emailVerifyHint: 'Require email verification during registration',
        passwordReset: 'Password Reset',
        passwordResetHint: 'Allow users to reset password via email',
        invitationCode: 'Invitation Code',
        invitationCodeHint: 'Require invitation code during registration'
      }
    }
