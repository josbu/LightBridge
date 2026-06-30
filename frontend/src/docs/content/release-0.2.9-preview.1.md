# 0.2.9-preview.1 版本更新

## 修复问题

1. 修复分组内混合协议账号无法被调度的问题
   - 完全移除分组所属平台对账号轮询的路由限制，分组内账号统一进入候选池。
   - 修复 Gemini / OpenAI Chat / OpenAI Responses 入站在非同平台分组中仍可能返回 `503 no available accounts` 的问题。
   - Scheduler 快照分组 bucket 统一为全协议候选，不再按分组平台拆分缓存。

2. 修复关联模块仍按分组平台提前拦截的问题
   - 渠道模型映射、渠道模型限制和账号统计定价改为按入站协议选择平台配置。
   - OpenAI Chat Completions 与 Responses handler 在解析渠道映射前写入入站协议，避免 fallback 到分组平台。
   - 前端与运维错误分析移除 platform mismatch 诊断展示。

## 优化调整

1. 分组模型可见性改为聚合组内所有账号
   - `/v1/models` 和管理端模型候选列表不再按分组平台过滤账号模型映射。
   - 模型路由、复制分组账号、fallback 分组配置不再要求同平台。

2. 预览版更新包选择更稳定
   - 正式版更新优先选择 `.tar.gz` 归档包，预览版仍优先使用增量二进制包。

## 验证与发布备注

1. 已完成核心回归验证
   - 后端 service、handler、routes、middleware 测试通过。
   - 前端错误分析测试、分组模型候选测试与 `vue-tsc --noEmit` 通过。
