# 0.2.24-preview 版本更新

本次 Preview 是 `v0.2.23-preview` 的发布修复版本，主要解决 Preview 工作流未能生成 Release 产物的问题，并继续包含模型列表侧边栏入口修复。

## 修复问题

1. 修复 Preview 二进制构建失败
    - 影响范围：`v0.2.22-preview` 与 `v0.2.23-preview` 的 GitHub Release workflow 在构建预览二进制时失败，导致用户侧收不到 Preview Release。
    - 修复方式：模型目录仓储中的时间 helper 改为独立命名，避免与 scheduler cache 在同一 `repository` 包内重复定义。

2. 保留模型列表侧边栏入口修复
    - 影响范围：用户侧侧边栏开启简洁模式时，`/model-catalog` 入口此前会被隐藏。
    - 修复方式：模型列表入口不再参与简洁模式隐藏，普通用户和管理员的个人区都可以从侧边栏进入。

## 优化调整

1. 发布说明同步
    - 新增本次 Preview 的根目录发布说明。
    - 同步新增 docs 版本更新说明，便于后续版本控制页面和维护记录追溯。

## 验证与发布备注

1. 已验证
    - `go test ./internal/repository`
    - `go test ./internal/service ./internal/handler -run '^$'`
    - `go build -tags=embed -trimpath -o /tmp/LightBridge-preview-build-check ./cmd/server`

2. 环境说明
    - 本地完整 `go test ./... -run '^$'` 仍受既有测试文件编译问题与沙箱网络下载超时影响，不作为本次 Preview 发布阻塞项。
