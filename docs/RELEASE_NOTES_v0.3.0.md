# LightBridge 0.3.0 正式版

## 渐进式功能注册与控制面

- 新增始终可访问的管理员“功能注册中心” `/admin/features`，统一展示核心、可选和扩展功能。
- 所有非核心功能支持“显式启用、显式停用、继承默认”三态控制；核心鉴权、网关、计费和 Token 刷新保持锁定。
- 控制面同时展示资源配置档位、依赖、注册面、配置目标、实际运行状态与运行时组件。
- 动态功能在修改后自动协调启停；启动期功能明确提示需要重启，并保留当前实际状态。
- 运行时启动失败不再隐藏管理入口，控制面会直接显示组件状态与错误原因。
- 控制面决策已统一接入公开功能清单及支付、风控、隐私过滤、运维监控等实际请求路径。

## 移除模块市场

- 移除旧 `/admin/modules` 页面、侧边栏入口及全部管理员模块安装、权限、启停、卸载 API。
- 删除用户可见的市场列表和安装流程，内置功能不再伪装成可安装模块。
- 保留 OpenAI、Anthropic 等 LightBridge 托管 Provider 的底层运行时、签名校验、自动安装和远程 UI contribution。
- 旧 `marketplace_*` 配置键仅作为内部托管 Provider registry 的兼容别名保留，不再构成产品市场能力。

## 0.3 系列主要能力

- 增加 Grok Build 与官方 xAI API 双 OAuth 模式，以及 Responses、Messages、Chat Completions 和 WebSocket bridge。
- 完善 Grok 多轮 reasoning/tool call 回放、Token context、真实可用性探测、配额冷却和账户迁移。
- 恢复渠道模型定价限制、分组平台约束、模型映射与显式白名单的统一判定。
- 修复协议桥空响应状态码、Gemini SSE 边界和 Grok WebSocket 终端事件观察顺序。
- 强化 Release 供应链：不可变 Action SHA、固定工具链、清单校验、secret scan、前后端回归和 GoReleaser 多平台产物。

## 升级说明

- 本版本没有新增数据库 schema migration；功能控制覆盖通过现有 settings 存储持久化。
- 升级后从“系统与配置 → 功能注册中心”管理渐进式功能。
- `boot` 类型功能修改后按页面提示重启 LightBridge；`dynamic` 类型会自动协调运行时。
- 现有 Grok Build OAuth 账号如缺少 `referrer=grok-build` 或真实探测结果，建议重新授权。

## 发布验证范围

- 后端 unit/integration、控制面 service/handler、Provider runtime 与配置兼容测试。
- 前端 lint、typecheck、功能注册 API/页面/路由测试与生产构建。
- Release 配置校验、代码清单、secret scan、Go module verify 与 GoReleaser 正式发布流程。
