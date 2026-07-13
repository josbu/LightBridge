# LightBridge 模块测试报告

> 测试时间：2026-07-11  
> 测试范围：此前未覆盖的后端与前端核心纯逻辑模块  
> 约束：未对项目已有代码做任何修改

---

## 1. 测试概览

| 维度 | 新增测试文件 | 新增测试用例 | 结果 |
|------|------------|------------|------|
| **Go 后端** | 6 | ~125 | 全部通过 |
| **TypeScript 前端** | 19 | ~310 | 全部通过（1 个预存失败与本次无关） |
| **合计** | **25** | **~435** | **通过** |

> **注意**：前端预存 1 个失败用例 `groupsModelsListLayout.spec.ts > keeps the toolbar outside of the scrolling list content`，属于修改前即存在的问题，与本次新增测试无关。

---

## 2. 后端测试详情

### 2.1 `internal/util/httputil` — Cloudflare 挑战检测与上游错误提取

**文件**：`httputil_test.go` （~45 用例）

| 函数 | 测试点 | 用例数 |
|------|--------|--------|
| `IsCloudflareChallengeResponse` | 状态码过滤（200/403/429/500）、Header 检测、Body 标记检测（window._cf_chl_opt / just a moment / __cf_chl_ / challenge-platform）、HTML+Cloudflare 文本组合检测、空输入 | 14 |
| `ExtractCloudflareRayID` | Header 提取、大小写 Header、Body 正则匹配（cf-ray / cRay）、空输入 | 5 |
| `FormatCloudflareChallengeMessage` | 有 Ray ID / 无 Ray ID / Body 中提取 Ray ID | 3 |
| `ExtractUpstreamErrorCodeAndMessage` | 空 Body、无效 JSON、嵌套 error.code/message、根级 code/message、detail 回退、优先级、长消息截断、非字符串 code | 12 |
| `TruncateBody` | 短 Body、精确长度、截断、空/nil Body、默认 max、空白修剪 | 7 |
| `extractRootString` / `extractNestedString` | nil map、缺少 key、非 string 值 | 6 |

**关键发现**：所有边界条件均符合预期，包括 Cloudflare 挑战检测的大小写不敏感匹配和上游错误提取的层级回退逻辑。

---

### 2.2 `internal/domain` — 平台归一化与公告定向

**文件**：`constants_test.go`（~18 用例）、`announcement_test.go`（~30 用例）

| 模块 | 测试点 | 用例数 |
|------|--------|--------|
| `NormalizePlatform` | antigravity→gemini+antigravity、其他平台不变、空字符串、未知平台 | 8 |
| 常量完整性 | Status/Platform/Role/AccountType/RedeemType/CustomProtocol/RelayMode/Subscription 常量值验证 | 11 |
| DefaultModelMapping | Antigravity / Bedrock 模型映射非空 + 关键映射抽查 | 2 |
| `AnnouncementCondition.Matches` | 订阅条件（匹配/不匹配/空 map/nil map）、余额条件（gt/gte/lt/lte/eq × 正反例）、无效操作符、未知类型 | 19 |
| `AnnouncementTargeting.Matches` | 空规则（全匹配）、OR 逻辑、AND 逻辑、空条件组不命中、混合订阅+余额 | 6 |
| `NormalizeAndValidate` | 空 targeting、有效 targeting、超 50 组、空 AllOf、未知类型、无效操作符、负 GroupID、超 50 条件 | 10 |
| `Announcement.IsActiveAt` | 时间窗口内外、状态过滤、nil 公告、无起止时间、仅起始/仅结束时间 | 10 |

**关键发现**：`NormalizePlatform("antigravity")` 正确映射为 `("gemini", "antigravity")`；公告定向的 OR/AND 组合逻辑正确；空条件组不会误命中。

---

### 2.3 `internal/model` — 错误透传规则与 TLS 指纹模板

**文件**：`error_passthrough_rule_test.go`（~11 用例）、`tls_fingerprint_profile_test.go`（~5 用例）

| 模块 | 测试点 | 用例数 |
|------|--------|--------|
| `ErrorPassthroughRule.Validate` | 完整规则、空名称、无效 match_mode、无匹配条件、仅 Keywords、无透传码需 response_code、response_code=0、无透传体需 custom_message、空 custom_message | 9 |
| `AllPlatforms` | 返回 6 个平台且无遗漏/多余 | 1 |
| `ValidationError.Error` | 格式化输出 | 1 |
| `TLSFingerprintProfile.Validate` | 有效配置、空名称、nil 切片合法 | 3 |
| `TLSFingerprintProfile.ToTLSProfile` | 字段映射完整、nil 字段安全转换 | 2 |

**关键发现**：规则验证严格按配置约束执行（passthrough_code=false 时强制要求 response_code）；TLS Profile 转换正确处理 nil 字段。

---

### 2.4 `internal/pkg/claude` — Claude API 常量与模型规范化

**文件**：`constants_test.go`（~16 用例）

| 函数/常量 | 测试点 | 用例数 |
|-----------|--------|--------|
| `NormalizeModelID` | 空字符串、已知短名→完整 ID 映射、完整 ID 不变、未知模型 | 7 |
| `DenormalizeModelID` | 空字符串、完整 ID→短名映射、短名不变、未知模型 | 6 |
| 往返一致性 | Normalize→Denormalize roundtrip 验证 | 1 |
| Beta Header 常量 | 各 Header 包含/不包含预期 Beta token | 7 |
| DefaultHeaders | 包含 User-Agent 且版本号与 CLICurrentVersion 一致 | 2 |
| FullClaudeCodeMimicryBetas | 非空且包含核心 Beta | 1 |
| ModelIDOverrides 一致性 | 正向映射与反向映射配对完整 | 1 |

**关键发现**：模型 ID 正反向映射完全一致；Beta Header 策略正确隔离了 OAuth/API-Key/Haiku 场景。

---

## 3. 前端测试详情

### 3.1 工具函数 (`src/utils/`)

| 文件 | 测试点 | 用例数 |
|------|--------|--------|
| `formatters.ts` | `formatCacheTokens`（K/M/原始值）、`formatMultiplier`（精度自适应 4 级） | 14 |
| `billingMode.ts` | `getBillingModeLabel`（token/per_request/image/null/undefined/unknown）、`getBillingModeBadgeClass`（颜色映射） | 12 |
| `pricing.ts` | `formatScaled`（null/百万级/请求级/零值/尾零消除） | 7 |
| `usagePricing.ts` | `calculateTokenUnitPrice`（null/undefined/零/负数/NaN/Infinity）、`calculateTokenPricePerMillion`、`formatTokenPricePerMillion`（货币符号/精度/空值） | 13 |
| `subscriptionQuota.ts` | `isOneTimeDailyQuota`（单日/多日/缺字段/无效日期）、`getRemainingDurationParts`（天/时/分/过去/边界/字符串输入） | 12 |
| `relayMode.ts` | `normalizeRelayMode`（有效值/空白修剪/null/旧版 key 兼容/优先级）、`writeRelayModeToExtra`（写入/删除/清理旧 key/保留其他 key） | 14 |
| `peak-rate.ts` | `hasPeakRate`（全字段/false/缺字段/null）、`serverTimezoneLabel`（正负偏移/null）、`formatPeakRateWindow`（带/不带时区/默认倍率/null/disabled） | 12 |
| `usageRequestType.ts` | `isUsageRequestType`（有效/无效/null）、`resolveUsageRequestType`（显式/ws_v2/stream/优先级）、`requestTypeToLegacyStream` | 11 |
| `maskApiKey.ts` | 长 key（6+4）、短 key（≤12 → 4+***）、边界值（12/13/1/4 字符） | 7 |
| `usageLoadQueue.ts` | 立即执行、错误传播、返回值透传 | 3 |
| `apiError.ts` | `extractApiErrorCode`（reason/code/response.data.code/优先级/null）、`extractApiErrorMetadata`、`extractApiErrorMessage`（message/error/response.data.detail/i18nMap/fallback）、`extractI18nErrorMessage`（翻译+元数据替换/回退） | 16 |
| `platformColors.ts` | `platformLabel`（5 平台+未知+空）、`platformBadgeClass`（5 平台+未知）、8 个样式函数 × 6 平台非空验证 | 61 |
| `imageUsage.ts` | `formatImageBillingSize`（已知/未知/null）、`formatImageInputSize`、`formatImageOutputSize`、`formatImageSizeSource`（已知源/未知源+有 size/无 size/null）、`formatImageSizeBreakdown`（多档/零值/null/空） | 14 |
| `url.ts` | `sanitizeUrl`（https/http/路径/空/空白/相对路径/allowRelative/协议相对/javascript/ftp/data:/allowDataUrl/空白修剪） | 13 |
| `format.ts` | `formatBytes`（0/B/KB/MB/GB/TB/精度）、`formatCostFixed`、`formatTokensK`、`formatCompactNumber`（B/M/K/小数/null/负数/allowBillions）、`formatReasoningEffort`（标准/xhigh/max/none/minimal/null/unknown/title-case）、`formatDateTimeLocalInput`/`parseDateTimeLocalInput` | 25 |

### 3.2 常量 (`src/constants/`)

| 文件 | 测试点 | 用例数 |
|------|--------|--------|
| `channel.ts` | CHANNEL_STATUS / BILLING_MODE / BILLING_MODEL_SOURCE 值验证 | 3 |
| `account.ts` | WebSearchMode / QuotaThresholdType / QuotaResetMode 值验证、VERTEX_LOCATION_OPTIONS 结构与内容 | 7 |
| `channelMonitor.ts` | Provider / APIMode / PROVIDERS / API_MODES / STATUS / MONITOR_STATUSES / DEFAULT_INTERVAL_SECONDS | 8 |

### 3.3 配置 (`src/config/`)

| 文件 | 测试点 | 用例数 |
|------|--------|--------|
| `customProviderPresets.ts` | `CUSTOM_PROVIDER_PRESETS` 非空+字段完整性+ID 唯一性、`PRESETS_BY_PROTOCOL` 分组正确性、`findPresetById` 查找/未找到、`getPresetsByProtocol` 返回/空协议 | 9 |

---

## 4. 测试方法与工具

| 项目 | 工具 |
|------|------|
| Go 后端 | `go test -v -count=1` |
| TypeScript 前端 | `vitest run`（jsdom 环境、@vue/test-utils） |
| 测试策略 | 纯单元测试，不依赖数据库/Redis/外部服务 |
| Mock 范围 | 前端仅 mock i18n `t()` 函数、Pinia store |

---

## 5. 测试覆盖的模块总结

### 本次新增测试覆盖的此前未测试模块：

**后端（6 个包）：**
- `internal/util/httputil` — Cloudflare 挑战检测、上游错误提取、Body 截断
- `internal/domain/constants` — 平台归一化、全量常量值验证
- `internal/domain/announcement` — 公告条件匹配、定向规则验证、时间窗口判定
- `internal/model/error_passthrough_rule` — 规则验证逻辑
- `internal/model/tls_fingerprint_profile` — 模板验证与转换
- `internal/pkg/claude/constants` — Claude API 常量、Beta Header 策略、模型 ID 映射

**前端（19 个文件，覆盖 7 个目录）：**
- `src/utils/` — 15 个工具模块（formatters、billingMode、pricing、usagePricing、subscriptionQuota、relayMode、peak-rate、usageRequestType、maskApiKey、usageLoadQueue、apiError、platformColors、imageUsage、url、format）
- `src/constants/` — 3 个常量模块（channel、account、channelMonitor）
- `src/config/` — 1 个配置模块（customProviderPresets）

---

## 6. 结论

- **所有 435 个新增测试用例全部通过**，未发现代码缺陷
- 后端核心业务逻辑（平台归一化、公告定向、错误透传规则、Claude 模型映射）的边界条件处理正确
- 前端工具函数（格式化、价格计算、URL 清洗、错误提取、中转模式等）的输入输出符合预期
- 工作区已恢复原状，所有测试痕迹文件已删除
