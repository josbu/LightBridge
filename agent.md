# Agent 工作规则

本文件是本仓库 agent 的实际规则来源。`CLAUDE.md` 仅作为入口，会要求 agent 先来阅读本文件。

## 规则一：版本部署必须走线上 GoReleaser

版本发布与打包**只能**通过 GitHub 的在线 GoReleaser workflow 完成，**禁止**在本地执行打包 / 构建发布产物。

### 正确做法

发布动作通过 `.github/workflows/release.yml` 触发，二选一：

1. **推送 tag**（推荐）：打一个 `v*` 形式的 annotated tag 并推送，workflow 自动运行。
   ```bash
   git tag -a v0.2.3 -m "release: v0.2.3"
   git push origin v0.2.3
   ```
2. **手动触发**：在 GitHub Actions 页面对 `Release` workflow 执行 `workflow_dispatch`，填入 `tag`（如 `v0.2.3`），可选勾选 `simple_release`。
   ```bash
   gh workflow run release.yml -f tag=v0.2.3
   ```

该 workflow 会在 GitHub Runner 上完成：更新 `VERSION` 文件、构建前端、运行 GoReleaser（`.goreleaser.yaml` 或 `simple_release` 时的 `.goreleaser.simple.yaml`）、推送镜像到 GHCR / DockerHub、发送 Telegram 通知、回写 `VERSION`。

### 禁止做法

- 禁止在本地运行 `goreleaser release`、`goreleaser build`（即使加 `--snapshot`）来产出正式发布物。
- 禁止本地 `docker build` + `docker push` 手动发布镜像。
- 本地至多允许 `goreleaser check` 校验配置语法，不得产生或上传发布产物。

如果用户要求"部署 / 发布新版本"，应通过上述线上 workflow 完成，而不是在本地打包。

### Tag 格式与发布规则

打 tag、撰写版本升级说明、区分 Preview / Production 发布，统一遵循本地文档 `docs/RELEASE_PROCESS.md`（未纳入版本控制，仅本地留存）：

- **Tag 格式**：`v<MAJOR>.<MINOR>.<PATCH>[-<预发布后缀>]`。
- **Production**：纯 `vX.Y.Z`（无后缀），GoReleaser 发为正式 release。
- **Preview**：带后缀（推荐 `-preview`，如 `v0.2.4-preview`），GoReleaser `prerelease: auto` 自动标记为 pre-release，前端自动归入 Preview，无需手动勾选。
- **Tag message**：第一行标题 + 空行 + Markdown 正文；正文会作为 GitHub Release 内容与版本控制页面的升级说明。

发布前请先阅读 `docs/RELEASE_PROCESS.md` 的完整规则。

