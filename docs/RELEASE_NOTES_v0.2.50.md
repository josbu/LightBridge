# 0.2.50 版本更新

## 新增功能

1. 订阅分组高峰时段倍率
    - 分组新增高峰时段开关、开始时间、结束时间和高峰倍率配置。
    - 后端计费在高峰窗口内按分组倍率叠加计算，支持 API Key 鉴权缓存与运行时快照同步。
    - 管理端分组页面、类型定义和中英文文案已同步支持高峰倍率配置与展示。

2. OpenAI Responses WebSocket HTTP Bridge
    - OpenAI WS mode 新增 `http_bridge` 模式，可将 WebSocket 入站桥接到 HTTP Responses 上游。
    - Grok 账号的 Responses WebSocket 入站自动走 HTTP Bridge，避免强依赖上游 WS v2 传输。
    - 账号创建、编辑、批量编辑、WS mode 工具函数和测试已同步支持新模式。

3. IP 地理信息与运维展示增强
    - 新增 IP 地理信息查询工具、批量获取工具栏和通用 IP 地理信息展示单元。
    - 账号使用量、管理用量表、用户用量页与运维错误列表补充客户端 IP 和地区信息展示。
    - 运维错误详情保留更多客户端来源信息，便于快速定位异常请求来源。

## 修复问题

1. 修复 Router 模式下协议过滤导致账号被错误清空的 P0 问题
    - router 模式的消息协议请求不再按入站协议与账号上游 `protocol` 做调度前强匹配。
    - 修复 OpenAI Chat Completions / Anthropic Messages 入站请求调度到 Custom OpenAI Responses 上游时被提前拒绝的问题。
    - 已增加 `mimo-v2.5-pro` 回归测试，确保 Custom OpenAI Responses 上游在 router 模式下可被 Chat Completions 请求选中。

2. 修复 Custom 账号编辑时协议字段写错位置的问题
    - 后端以 `accounts.extra["protocol"]` 作为 Custom 账号协议来源，编辑弹窗现在会正确读取并写回 `extra.protocol`。
    - 保存时会清理旧的 `credentials.protocol`，避免前端显示与后端调度来源不一致。

3. 修复分组上游协议展示与 Router 语义不一致的问题
    - router 模式账号在分组上游协议派生中暴露全部可路由消息协议。
    - passthrough 与 full_passthrough 账号仍按真实目标协议参与分组协议过滤。

4. 修复 OpenAI WS 与协议转发的若干兼容问题
    - WebSocket 入站补充 HTTP Bridge 转发路径、上下文恢复和 replay 相关处理。
    - OpenAI WS mode 传输兼容性检测覆盖新的 ingress 模式。

## 优化调整

1. 透传模式语义收紧
    - `passthrough` 与 `full_passthrough` 均要求入站协议与目标账号协议一致。
    - router 模式继续承担跨协议转换，避免透传账号被误选中后原样转发到不兼容上游。

2. 计费与用量统计优化
    - 用量记录支持高峰倍率参与后的文本倍率与图片倍率分离计算。
    - 账号用量、平台用量与模型限流缓存同步补充新字段，减少高峰计费与缓存快照不一致。

3. 前端配置体验优化
    - 分组管理补充高峰倍率输入、校验与展示。
    - 账号管理补充 HTTP Bridge WS mode 选项和 Custom 协议保存校验。
    - 中英文文案、类型定义与单元测试同步覆盖新增能力。

## 兼容性 / 破坏性变更

1. 本次正式版包含数据库迁移
    - 影响范围：分组表新增高峰时段倍率相关字段。
    - 处理方式：升级时请确保后端迁移正常执行；手动迁移场景请应用 `backend/migrations/155_add_group_peak_rate_multiplier.sql`。

2. `full_passthrough` 不再允许跨协议原样转发
    - 影响范围：此前将 full passthrough 账号用于跨协议请求的配置会在调度阶段被拒绝。
    - 处理方式：需要跨协议时请使用 router 模式；只有确认入站协议与目标上游协议一致时才使用 passthrough/full_passthrough。

3. Custom 账号协议字段以 `extra.protocol` 为准
    - 影响范围：历史编辑误写入 `credentials.protocol` 的账号可能无法被后端识别为目标协议。
    - 处理方式：进入账号编辑页保存一次，或通过 API 将协议写入 `extra.protocol`。

## 升级说明

1. 升级到 `0.2.50` 后建议检查分组高峰倍率配置
    - 默认未启用高峰倍率，不会改变现有分组计费。
    - 如启用高峰倍率，请确认时段格式为 `HH:MM` 且不跨天。

2. 升级后建议验证 Router 自定义上游
    - 对 Custom OpenAI Responses 上游，请确认账号处于 router 模式且 `extra.protocol=openai_responses`。
    - 使用 OpenAI Chat Completions 请求 `mimo-v2.5-pro` 时，不应再因为协议不一致返回 503。

3. 升级后建议验证 OpenAI/Grok WS mode
    - 如使用 Responses WebSocket 入站，请检查账号 WS mode 配置。
    - Grok 账号会通过 HTTP Bridge 处理 Responses WebSocket 入站。

## 验证与发布备注

1. 已验证后端关键服务包
    - `go test ./internal/service`

2. 已验证 Router P0 回归路径
    - `go test ./internal/service -run 'TestProtocolRouteDecision|TestFilterAccountsByRequestProtocol|TestAccountMatchesRequestProtocol|TestAccountUpstreamProtocols|TestOpenAIGatewayService_SelectAccountWithScheduler_RouterCustomResponsesServesChatMIMO'`

3. 已验证前端类型检查与账号编辑回归
    - `yarn typecheck`
    - `yarn test:run src/components/account/__tests__/EditAccountModal.spec.ts`

4. 已验证代码空白
    - `git diff --check`
