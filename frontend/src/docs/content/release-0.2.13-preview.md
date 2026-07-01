# 0.2.13 版本更新

## 新增功能

1. 路由错误诊断增强
   - 上游错误响应现在会附加技术细节（upstream_status、upstream_error）到客户端错误消息中，便于排查问题。
   - 协议路由失败时返回详细的失败原因（account 信息、支持的协议列表、转换是否实现等）。
   - 覆盖 Anthropic Messages、OpenAI Chat Completions、OpenAI Responses、Gemini 全部协议路径。

2. 应用内文档中心
   - 新增文档中心页面，集中展示使用指南与版本更新说明。

## 修复问题

1. 修复管理端分组处理器与设置处理器同包函数重名导致 Go 编译失败的问题。
2. 修复前端 pnpm lockfile 中 js-cookie overrides 元数据缺失，导致 CI frozen-lockfile 安装失败。
3. 修复 Preview 版本更新检测逻辑。

## 优化调整

1. 重构 LightBridge 前端 UI 与数据流。
2. 完善项目上游致谢说明，README 增加 Sub2API 与 New API 致谢。

## 升级说明

1. 本次预览版不需要额外配置变更，可沿用现有环境变量与部署配置。
