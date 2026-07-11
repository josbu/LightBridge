# LightBridge 0.2.80 分阶段稳定化与结构整理记录

日期：2026-07-11

本目录由用户提供的 `LightBridge-0.2.80` 原始项目直接修改得到。开发过程先建立完整文件清单和声明索引，再把纯结构重排与业务行为修复分成独立提交，避免“为了整齐而重写”造成隐藏回归。

## 审阅方法与范围

- 仓库中的文本文件全部按路径、行数、归属和 SHA-256 纳入 `docs/architecture/CODEBASE_INVENTORY.tsv`。
- 手写 Go 代码按 AST 顶层声明、方法接收者、导入和调用层次审阅；生成的 Ent 文件不做无依据手改。
- TypeScript、JavaScript 和 Vue script 单元全部进入解析清单；巨型页面按模板、脚本、样式及业务工作流分别审阅。
- SQL migration、CI、安全扫描、GoReleaser、Docker、Shell、YAML/JSON 和开发文档均纳入工程检查。
- 锁文件和生成代码计入清单，但主要通过生成来源、语法、构建和一致性检查验证。

当前清单约包含：

- 2,812 个文本文件；
- 约 698,289 行手写维护内容；
- 约 222,616 行生成代码；
- 1,782 个 Go 文件；
- 687 个 TypeScript、JavaScript 或 Vue script 单元。

清单可通过以下命令重新生成和核对：

```bash
make audit-codebase
python3 tools/codebase_inventory.py --check
```

## 第一轮：高风险稳定化

### 1. 多节点并发槽位正确性

- 修复启动时把其他健康进程的前缀误判为过期槽位的问题。
- 仅依据 Redis 服务器时间和 slot TTL 删除真正过期成员。
- 保留其他实例的账号槽位、用户槽位和等待计数。
- 每个 Redis key 单独运行 Lua，避免 Redis Cluster 跨 hash slot `EVAL`。
- 增加多实例保留、过期清理和等待计数测试。

### 2. 使用量与计费任务不再可丢弃

- 使用量 worker 实际承载余额、订阅、API Key 配额和账号成本，不能按 telemetry 丢弃。
- 队列满、pool 停止或旧配置为 `drop/sample` 时统一同步执行兜底。
- 默认溢出策略改为 `sync`，任务超时提高到 30 秒。
- 增加队列满、停止后提交和旧配置兼容测试。

### 3. 外部 iframe 登录凭据隔离

- 不再把完整登录 JWT 放入 iframe URL。
- 当前页面 query/hash 不再复制到 `src_url`。
- 新增 5 分钟、固定 audience 和 scope 的 `payment_embed` JWT。
- 外部页面通过精确 `postMessage(targetOrigin)` 获取短期凭据。
- scoped token 只能访问用户支付路由，不能刷新、绑定 OAuth 或访问管理员 API。
- iframe 增加 `sandbox` 与 `referrerpolicy="no-referrer"`。
- 同步更新中英文集成文档和测试。

### 4. 生命周期和后台任务排空

- 主服务器错误不再通过 `log.Fatalf` 绕过 defer cleanup。
- 使用 signal context、30 秒 graceful shutdown 和强制关闭兜底。
- 清理顺序改为：停止生产者 → 排空计费 → 刷新配额/缓存/邮件/日志 → 关闭 Redis 与数据库。
- 每阶段使用真实 deadline；不再无限等待后才记录超时。
- 增加清理顺序、并行停止和超时测试。

### 5. SSRF 与 DNS Rebinding

- 直连路径使用“解析、校验并连接同一个 IP”的 dialer，消除 DNS 校验与实际连接之间的 TOCTOU。
- 普通 HTTPS 和 uTLS 指纹路径保留原 hostname 作为 Host、SNI 和证书校验目标。
- 每次请求和重定向重新校验，不再依赖 hostname 安全缓存。
- 扩充特殊地址阻断范围。
- 远端 DNS 代理仍需代理节点自身实施目标 IP 策略，这是明确的剩余边界。

### 6. 全局请求体限制

- 修复 `MaxBytesHandler` 只修改局部变量而未成为 `http.Server.Handler` 的错误。
- 增加真实 HTTP 超限请求测试。

### 7. CI 与工程清理

- 删除 5 个只服务于旧 PR、拥有写权限并向固定分支推送修改的临时 workflow。
- 统一开发文档、`go.mod`、CI 和 Dockerfile 的 Go 版本为 1.26.4。

## 第二轮：框架边界整理

### 8. 大型后端聚合文件按职责拆分

采用“同包、接口不变、方法体不变”的方式拆分核心聚合文件，包括：

- Gateway、OpenAI Gateway、Gateway Handler、Ops 错误中间件；
- SettingService、SettingHandler、AdminService、Usage Repository；
- Antigravity、Gemini compatibility、WebSocket forwarder、Account Repository；
- Config、Account、Content Moderation、Gateway Scheduler；
- 管理员 Account Handler 与 RateLimitService。

文件现在按会话、调度、转发、流式处理、计费、错误、模型映射、重试、配额、Provider 调用和诊断等职责命名。

所有拆分均通过 AST 声明内容比较。累计核对 2,561 个顶层声明，结果为：

```text
missing = 0
extra   = 0
changed = 0
```

这轮重排没有通过重新实现方法来追求“漂亮”，而是先降低物理认知复杂度。

### 9. 前端巨型文件建立 feature-local 边界

- 多个巨型 Vue 页面先把 template/style 与 script 分离，保持原内容等价。
- 中英文单体词典按业务域拆分，同时保持公开 i18n key 不变。
- `SettingsView` 进一步抽出：
  - 默认表单模型；
  - 页签导航、登录协议和表格规则；
  - 支付 Provider 冲突规则；
  - 支付 Provider 工作流 composable；
  - 推广专属用户工作流 composable。
- `SettingsView.vue` 脚本由约 3,000 行降低到约 2,000 行。
- 推广搜索 timer 在组件卸载时清理。
- 表单初始化对象通过 AST/结构比较保持等价。

### 10. 可重复的代码清单与架构地图

新增：

- `tools/codebase_inventory.py`
- `docs/architecture/CODEBASE_INVENTORY.tsv`
- `docs/architecture/CODEBASE_MAP.md`
- `make audit-codebase`

架构地图明确 handler、service、repository、outbound、配置文件、feature-local model/composable 和后台任务的归属规则，防止后续重新堆回 `misc` 或巨型文件。

## 第二轮：P1/P2 行为修复

### 11. 定价失败进入持久化待处理账本

- 新增 `usage_pricing_failures` migration、领域模型和 repository。
- 正式模式遇到未知模型或定价异常时，不再把请求写成正常的零元消费。
- 保存请求 ID、协议、模型候选、token、倍率、订阅/用户/账号上下文和原始错误。
- `(request_id, api_key_id, protocol)` 幂等合并重复记录。
- 如果生产 repository 无法保存待处理记录，返回明确的 fail-closed 错误。
- simple 模式仍保留原有零费用语义。

尚未在本轮实现自动重算 worker 和管理员处理页面；账本已保证恢复数据不再静默消失。

### 12. 计费后通知不再无限创建 goroutine

- 余额与账号配额策略判断在 usage worker 内同步完成。
- 真正的邮件发送使用固定 8 个异步槽位。
- 槽位满时调用方同步执行形成背压，不丢任务、不再额外创建 goroutine。
- panic recovery 收拢到统一执行器。
- 增加同步 fallback、满载背压、槽位释放和 panic 恢复测试。

### 13. 平台配额持久化去重并增加超时

- 主计费与 legacy 路径原本分别实现一套 Redis 累加/flusher/DB 写入逻辑，现统一为 `persistUserPlatformQuotaUsage`。
- Redis enforcement 仍同步更新。
- flusher 开启时只标记 dirty，由批量 flusher 刷库。
- flusher 关闭时，数据库镜像写入在 usage worker 中同步执行，并带 15 秒 detached deadline。
- 删除每请求一个、且没有 deadline 的数据库 goroutine，避免退出时丢失和 goroutine 悬挂。
- 主路径与 legacy 路径继续分别统计失败指标。

## 已执行的验证

当前离线环境内已完成：

- Go AST：1,782 个 Go 文件，0 个解析错误；
- 前端解析：687 个 TypeScript、JavaScript、Vue script 单元，0 个解析错误；
- Go 文件拆分声明等价检查：2,561 个声明，0 差异；
- `SettingsForm` 初始化结构等价检查通过；
- 新增前端 model/composable 独立 strict TypeScript 检查通过；
- JSON 解析、普通 YAML 解析、Shell `bash -n`、冲突标记和补丁 whitespace 检查通过；
- 代码清单 SHA-256 生成与 `--check` 回环验证通过；
- ZIP 将在最终生成后执行完整性和解压回环检查。

## 当前环境无法执行的验证

当前沙盒只有 Go 1.23.2，而项目要求 Go 1.26.4；环境没有外网、Go module cache、pnpm 或 `node_modules`。因此不能诚实宣称在本地完成：

- `go test -tags=unit ./...`
- PostgreSQL/Redis 集成测试
- `go test -race`
- `golangci-lint`
- `pnpm type-check`
- `pnpm test`
- `pnpm build`

对应测试代码已经加入。完整门禁必须在原 GitHub CI 或具备 Go 1.26.4、PostgreSQL、Redis 和 pnpm 依赖的环境运行。

## 后续仍建议分阶段处理

1. 将内存计费任务升级为 PostgreSQL outbox 或 Redis Stream，覆盖进程突然崩溃而来不及同步 fallback 的场景。
2. 为 `usage_pricing_failures` 增加自动重算、重试、人工忽略和管理员对账界面。
3. 把 Refresh Token 从 localStorage 迁移到 HttpOnly/Secure/SameSite Cookie，并完整实现 rotation 和 reuse detection；这需要前后端认证集成测试保护。
4. 把仍使用完整响应缓冲的跨协议流式转换改为内部事件流，降低首 token 延迟和内存峰值。
5. 继续整理 OAuth pending flow、Account Test Service 和剩余大型 Vue 页面，但应继续遵守“先声明等价，再改变行为”。
6. 远端 DNS 代理场景需要代理服务自身执行目标 IP allow/deny，或使用能够返回并固定解析结果的受控协议。

## 第三轮：渐进式功能注册与生产封板

### 14. 统一 Feature Catalog

- 后端以稳定 feature ID 统一描述核心、可选和扩展能力。
- 每项功能声明 `eager/dynamic/boot/on_demand` 激活方式、最低设备档位、依赖和贡献面。
- 认证、网关、计费和 OAuth Token 刷新保持核心常驻，不为了节省空闲资源拆断请求不变量。
- 新增 `minimal/standard/full` 设备档位和进程级 override 校验；核心功能不可关闭。

### 15. 可选后台组件生命周期

- 模块运行时、运维统计、聚合、清理、备份、计划测试、支付过期、渠道监控和 LightBridge Connect 等统一由 `FeatureRuntimeManager` 管理。
- 构造函数不再隐式启动可选 goroutine；运行时管理器成为唯一生命周期所有者。
- restart-safe 业务 worker 可运行时暂停/恢复；使用 `sync.Once`、一次性 channel 或永久 shutdown 状态的高开销子系统只在启动时注册。
- 启动失败会使用独立 30 秒 deadline 回滚；回滚失败的部分启动组件禁止重试，并在最终退出时再次清理。
- 同一 feature 下的多个后台组件作为原子生命周期组启动；任一成员失败时停止后续成员并按逆序回滚已启动成员，避免 Ops 等子系统进入半启动状态。
- Ops 指标采集器停止时会取消正在进行的采集并等待 goroutine 退出，避免 Redis/数据库连接池先关闭。

### 16. 配置状态与本进程实际状态一致

- `boot` 功能同时返回 `configuredEnabled` 与本进程实际 `enabled`。
- 配置与实际状态不一致时返回 `requiresRestart`，并区分启用/停用重启原因。
- 后端路由守卫、前端路由和菜单全部使用本进程实际状态，避免页面存在但 worker 不存在，或 worker 仍运行而页面提前消失。
- 后台组件错误只通过管理员接口提供；公开 Feature Manifest 不泄露文件路径、数据库错误和模块启动细节。

### 17. 前端与模块市场完整渐进式注册

- 内置可选页面只在功能有效时注册路由，页面 chunk 保持动态导入。
- 菜单、路由和组件缓存随动态功能或模块启停统一增删。
- 模块市场完整消费 admin route、account form 和 entity panel 三类 UI contribution。
- 远程入口仅允许同源 `/modules/` 资源；只暴露当前启用版本，并阻止路径穿越和 symlink 跨包访问。
- 动态加载加入并发去重、失败缓存清理、异步 generation 保护和核心路由冲突校验。
- 模块菜单、账号表单和实体面板支持中英文贡献字段。

### 18. 渐进式判定性能

- Feature Manifest 和路由守卫不再逐功能查询数据库。
- 设置批量读取后生成不可变快照；常规请求只进行进程内只读判断。
- 快照失效使用 singleflight 刷新，数据库异常使用声明默认值做一次未缓存评估，不产生逐项查询风暴。
- 独立 Ops 探针并发执行，降低统计采集总等待时间。
- 本阶段没有引入 Rust：已定位的热点主要是 PostgreSQL、Redis、网络和文件系统 I/O，引入 FFI/sidecar 只会增加发布和跨平台复杂度。Rust 留待 CPU profile 证明存在稳定纯计算热点后再评估。

详细边界与模块 UI 契约见 `docs/architecture/PROGRESSIVE_FEATURES.md`。

## 第四轮：Router 严格客户端协议兼容

### 19. Claude Code / new-api / Grok 4.5 响应契约修复

- 增加集中式 Router Client Profile，识别 Claude Code、Codex CLI、Codex App 和 OpenCode 的严格协议需求。
- Anthropic Messages 流在上游缺少 `response.created` 时仍保证首先发送 `message_start`。
- `message_start.message.usage.input_tokens` 与终止 `message_delta.usage` 始终存在；缺失上游 usage 时使用明确零值维持结构安全，不伪装成真实计费结果。
- new-api 或兼容网关只发送最终 `response.output` 时，可从终止事件恢复文本、thinking 和 tool-use 内容，不再返回空 Anthropic 消息。
- 支持 `response.done/incomplete/failed/cancelled/canceled` 等终止别名。

### 20. Codex / OpenCode Responses 终止结构规范化

- HTTP SSE、非流式、API Key passthrough 与 Responses WebSocket 都统一补齐 `response.object/status/output/usage`。
- 接受顶层 usage、`prompt_tokens/completion_tokens` 等兼容别名，并转换为 Responses 字段。
- 原始上游值优先保留；只补缺失字段。

### 21. 客户端请求头和 Grok 4.5 能力策略

- 原生 Codex 路径继续保留受支持的 `originator`、conversation/session 和 turn metadata。
- Anthropic -> 第三方 Responses 桥接不会伪装成原生 Codex 请求，会移除 Codex 专用会话头并使用稳定 Router User-Agent。
- Grok 4.5 保留官方支持的 reasoning effort 与 encrypted reasoning，`xhigh` 降级为 `high`，移除桥接器自动添加但 xAI 未声明支持的 `text.verbosity` 和 summary selector。

详细协议边界见 `docs/architecture/ROUTER_PROTOCOL_COMPATIBILITY.md`。
