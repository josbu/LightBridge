# 管理员 - 系统设置

本章介绍系统设置、模块管理、隐私过滤和版本控制功能。

---

## 系统设置 `/admin/settings`

系统全局配置中心，包含 9 个配置标签页。

### Tab 1：通用 (General)

| 设置项 | 操作方式 | 说明 |
|--------|---------|------|
| 后端模式 | 开关切换 | 启用后限制非管理员访问 |
| 站点名称 | 文本输入 | 显示在页面标题和邮件中 |
| 站点副标题 | 文本输入 | 显示在首页 |
| API Base URL | 文本输入 | API 基础地址 |
| 默认分页大小 | 数字输入 | 表格默认每页条数 |
| 自定义端点 | 配置项 | 自定义 API 端点 |

### Tab 2：安全 (Security)

| 设置项 | 操作方式 | 说明 |
|--------|---------|------|
| Admin API Key | 创建/重生成/删除/复制 | 管理员 API 密钥 |
| 启用注册 | 开关 | 是否允许新用户注册 |
| 邮箱验证 | 开关 | 注册时是否需要邮箱验证 |
| 邮箱后缀白名单 | 标签输入 | 限制可注册的邮箱后缀 |
| 优惠码 | 开关 | 是否启用优惠码功能 |
| 邀请码 | 开关 | 是否启用邀请码功能 |
| 密码重置 | 开关 | 是否允许密码重置 |
| 前端 URL | 文本输入 | 用于 OAuth 回调等 |
| TOTP 2FA | 配置 | 双因素认证设置 |
| Cloudflare Turnstile | 配置 | 人机验证配置 |
| LinuxDo OAuth | 配置 | Client ID / Secret / Redirect URL |
| GitHub OAuth | 配置 | Client ID / Secret / 回调地址 |
| Google OAuth | 配置 | Client ID / Secret / 回调地址 |
| 微信 OAuth | 配置 | 三套 AppID + AppSecret（PC / 公众号 / 移动应用） |
| 钉钉 OAuth | 配置 | Client ID / Secret / 回调 / 企业限制 / 身份同步 |
| OIDC | 配置 | Provider Name / Client ID / Secret / Issuer URL 等完整 OIDC 配置 |

### Tab 3：用户 (Users)

| 设置项 | 操作方式 | 说明 |
|--------|---------|------|
| 默认余额 | 数字输入 | 新用户默认余额 |
| 默认并发数 | 数字输入 | 新用户默认并发请求数 |
| 默认 RPM 限制 | 数字输入 | 新用户默认 RPM |
| 默认订阅列表 | 可增删列表 | 新用户默认获得的订阅 |
| 全局平台限额 | 矩阵配置 | anthropic / openai / gemini / antigravity 的 daily / weekly / monthly |
| 认证来源默认值 | 按来源配置 | 各 OAuth 来源的注册授予、余额、并发、订阅、平台限额 |

### Tab 4：网关 (Gateway)

| 设置项 | 操作方式 | 说明 |
|--------|---------|------|
| Claude Code 版本限制 | 最低/最高版本 | 限制 Claude Code 客户端版本 |
| 调度设置 | 开关 | 允许未分组 Key 调度、OpenAI 高级调度器 |
| 转发设置 | 多项开关 | 指纹统一化、元数据透传、CCH 签名等 |
| 过载冷却 (529) | 开关+分钟数 | 529 错误冷却时间 |
| 速率限制冷却 (429) | 开关+秒数 | 429 错误冷却时间 |
| 流超时 | 开关+动作+阈值 | 流式响应超时处理 |
| 请求修正器 | 主开关+子开关 | Thinking Signature / Budget / API Key Signature |
| 真伪检测 | 开关+阈值 | 被动检测 Claude 模型真伪 |
| Beta 策略 | 按 beta_token 配置 | 动作/作用域/错误消息/预设/模型白名单/回退 |
| OpenAI Fast/Flex 策略 | 按 Service Tier 配置 | 动作/作用域/模型白名单/回退 |
| Web 搜索模拟 | 开关+多 Provider | Brave / Tavily 配置（API Key / 配额 / 代理 / 测试） |

### Tab 5：支付 (Payment)

管理支付提供商实例、支付方式、限额等配置。

### Tab 6：邮件 (Email)

| 功能 | 操作方式 | 说明 |
|------|---------|------|
| 选择邮件事件 | 点击事件标签 | 支持 12 种邮件事件模板 |
| 选择语言 | 点击语言标签 | 多语言模板编辑 |
| 编辑 Subject | 文本输入 | 邮件主题 |
| 编辑 HTML 正文 | 等宽文本区域 | 支持约 40 个变量占位符 |
| 实时预览 | iframe 渲染 | 实时预览邮件效果 |
| 恢复官方模板 | 点击恢复按钮 | 恢复默认模板 |
| 保存自定义模板 | 点击保存按钮 | 保存修改后的模板 |

### Tab 7：备份 (Backup)

创建系统备份和从备份恢复。

---

## 模块管理 `/admin/modules`

管理系统内置功能和可安装模块。

### 内置功能

以卡片网格展示 5 个内置功能开关，每个卡片含图标、标题、描述和 Toggle 开关：

| 功能 | 配置链接 |
|------|---------|
| Channel Monitor（渠道监控） | `/admin/channels/monitor` |
| Available Channels（可用渠道） | `/admin/channels/pricing` |
| Risk Control（风控） | `/admin/risk-control` |
| Privacy Filter（隐私过滤） | `/admin/privacy-filter` |
| Affiliate（分销） | `/admin/affiliates/invites` |

### 已安装模块

| 操作 | 说明 |
|------|------|
| Approve Permissions | 审批模块权限 |
| Enable / Disable | 启用或禁用模块 |
| Uninstall | 卸载模块 |
| Purge | 清除模块数据（危险操作） |

### 市场

展示可安装的市场模块，点击 Install 按钮安装。

---

## 隐私过滤 `/admin/privacy-filter`

配置隐私过滤规则，保护敏感数据。

| 功能区 | 功能 | 操作方式 |
|--------|------|---------|
| 基础开关 | 启用/禁用全局过滤 | 主开关 + 请求/响应过滤开关 |
| 内置规则 | 选择内置规则 | 复选框网格，逐项勾选/取消 |
| 自定义规则 | 添加/编辑/删除规则 | 名称 + 正则模式 + 替换文本 + 启用开关 |
| 应用对象 | 选择目标用户范围 | All Users / Partial Users / Admin Only |
| 渠道维度 | 选择应用渠道范围 | All / Group / Channel / Account |
| 作用域 | 选择应用分组范围 | All Groups / 指定组 + 模型过滤 |
| 保存配置 | 点击保存按钮 | 保存所有配置变更 |

---

## 版本控制 `/admin/version-control`

查看和管理系统版本。

| 功能 | 操作方式 | 说明 |
|------|---------|------|
| 查看当前版本 | 自动显示 | 当前安装的版本号 |
| 查看最新版本 | 自动显示 | 最新可用版本 |
| 查看构建类型 | 自动显示 | Release Build 或 Source Mode |
| 切换版本类型 | 点击 Production / Preview 标签 | 过滤正式版或预览版 |
| 刷新发布列表 | 点击刷新按钮 | 重新拉取 GitHub Releases |
| 查看变更日志 | 点击「View Upgrade Changes」 | 打开变更日志弹窗 |
| 安装版本 | 点击「Install Version」 | 弹出确认框后执行更新 |
| 重启服务 | 点击「Restart Now」 | 更新成功后重启服务（8 秒倒计时） |
